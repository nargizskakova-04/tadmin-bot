package sheets

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"

	"admin-bot/internal/usecase"
)

// Client wraps the Google Sheets API.
//
// The Drive API is no longer needed: tables are pre-created and shared by the
// administrator, so the bot only writes data, never creates files or changes
// permissions.
type Client struct {
	sheetsSvc *sheets.Service
	logger    *slog.Logger
}

// Layout constants for the generated defense table.
//
// Note: these mirror the scheduling assumptions in
// internal/usecase/defense.go (start hour, slot duration, break duration).
// If those change, change these too.
const (
	defenseStartHour = 11
	slotDuration     = 30 * time.Minute
	breakDuration    = 30 * time.Minute
	groupColumns     = 3
	totalColumns     = groupColumns + 1 // +1 for the time column

	// sheetName is the tab name on every defense spreadsheet. The pre-created
	// templates have a single tab — we always write to whatever its current
	// name is by using an unqualified A1 notation (no "SheetName!" prefix),
	// which targets the first sheet.
	defaultClearRange = "A1:Z1000"
)

// NewClient creates a Sheets client using a service account credentials JSON file.
func NewClient(credentialsFile string, logger *slog.Logger) (*Client, error) {
	ctx := context.Background()

	sheetsSvc, err := sheets.NewService(ctx, option.WithCredentialsFile(credentialsFile))
	if err != nil {
		return nil, fmt.Errorf("create sheets service: %w", err)
	}

	return &Client{
		sheetsSvc: sheetsSvc,
		logger:    logger,
	}, nil
}

// DefenseTableParams holds everything needed to (re)populate a defense table.
type DefenseTableParams struct {
	RaidName    string
	DefenseDate time.Time // Monday date for the defense
	Schedule    usecase.DefenseSchedule
}

// UpdateDefenseTable wipes the first sheet of the given spreadsheet and
// rewrites it with the latest defense schedule. The spreadsheet must already
// exist and be shared with the bot's service account.
//
// Returns the canonical URL to the spreadsheet.
func (c *Client) UpdateDefenseTable(ctx context.Context, spreadsheetID string, params DefenseTableParams) (string, error) {
	// 1. Resolve the first sheet (its tab name and sheetId).
	tabName, sheetID, err := c.firstSheetMeta(ctx, spreadsheetID)
	if err != nil {
		return "", fmt.Errorf("inspect spreadsheet: %w", err)
	}

	rowData := buildRows(params)

	// 2. Wipe the first sheet so stale rows don't bleed into the new table.
	clearRange := fmt.Sprintf("'%s'!%s", tabName, defaultClearRange)
	if _, err := c.sheetsSvc.Spreadsheets.Values.Clear(
		spreadsheetID, clearRange, &sheets.ClearValuesRequest{},
	).Context(ctx).Do(); err != nil {
		return "", fmt.Errorf("clear values: %w", err)
	}

	// 3. Write the new content.
	if err := c.populateSheet(ctx, spreadsheetID, tabName, params, rowData); err != nil {
		return "", fmt.Errorf("populate sheet: %w", err)
	}

	// 4. Apply formatting (non-critical — log and continue on failure).
	if err := c.formatSheet(ctx, spreadsheetID, sheetID, rowData); err != nil {
		c.logger.Warn("formatting failed (non-critical)", "spreadsheet_id", spreadsheetID, "err", err)
	}

	url := fmt.Sprintf("https://docs.google.com/spreadsheets/d/%s/edit", spreadsheetID)
	c.logger.Info("defense table updated", "spreadsheet_id", spreadsheetID, "url", url)
	return url, nil
}

// firstSheetMeta returns the title and sheetId of the first sheet of the
// spreadsheet — needed because writes and formatting both have to target a
// specific tab.
func (c *Client) firstSheetMeta(ctx context.Context, spreadsheetID string) (title string, sheetID int64, err error) {
	resp, err := c.sheetsSvc.Spreadsheets.Get(spreadsheetID).
		Fields("sheets.properties.sheetId,sheets.properties.title").
		Context(ctx).Do()
	if err != nil {
		return "", 0, err
	}
	if len(resp.Sheets) == 0 || resp.Sheets[0].Properties == nil {
		return "", 0, fmt.Errorf("spreadsheet %q has no sheets", spreadsheetID)
	}
	return resp.Sheets[0].Properties.Title, resp.Sheets[0].Properties.SheetId, nil
}
