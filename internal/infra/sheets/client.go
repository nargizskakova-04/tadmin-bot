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

// rowType distinguishes between slot rows, break rows, and the header.
type rowType int

const (
	rowSlot rowType = iota
	rowBreak
)

type tableRow struct {
	Type     rowType
	TimeSlot string // e.g. "11:00"
}

// buildRows generates the list of rows (time slots + breaks).
func buildRows(params DefenseTableParams) []tableRow {
	schedule := params.Schedule
	breakSet := make(map[int]bool)
	for _, b := range schedule.BreakAfterRows {
		breakSet[b] = true
	}

	var rows []tableRow
	startTime := time.Date(2000, 1, 1, defenseStartHour, 0, 0, 0, time.UTC)
	current := startTime

	for row := 1; row <= schedule.Rows; row++ {
		rows = append(rows, tableRow{
			Type:     rowSlot,
			TimeSlot: fmt.Sprintf("%02d:%02d", current.Hour(), current.Minute()),
		})
		current = current.Add(slotDuration)

		if breakSet[row] {
			rows = append(rows, tableRow{
				Type:     rowBreak,
				TimeSlot: fmt.Sprintf("%02d:%02d", current.Hour(), current.Minute()),
			})
			current = current.Add(breakDuration)
		}
	}

	return rows
}

// populateSheet fills the spreadsheet with headers and time slots.
func (c *Client) populateSheet(ctx context.Context, spreadsheetID, tabName string, params DefenseTableParams, rows []tableRow) error {
	// Build all values: header row + data rows.
	var values [][]interface{}

	// Header row.
	header := []interface{}{
		fmt.Sprintf("%s — %s", params.RaidName, params.DefenseDate.Format("02.01.2006")),
		"Группа 1",
		"Группа 2",
		"Группа 3",
	}
	values = append(values, header)

	// Data rows.
	for _, row := range rows {
		switch row.Type {
		case rowSlot:
			values = append(values, []interface{}{row.TimeSlot, "", "", ""})
		case rowBreak:
			values = append(values, []interface{}{row.TimeSlot, "Перерыв", "Перерыв", "Перерыв"})
		}
	}

	rangeStr := fmt.Sprintf("'%s'!A1:D%d", tabName, len(values))
	_, err := c.sheetsSvc.Spreadsheets.Values.Update(spreadsheetID, rangeStr, &sheets.ValueRange{
		Values: values,
	}).ValueInputOption("RAW").Context(ctx).Do()

	return err
}

// formatSheet applies basic formatting: bold header, colored break rows,
// column widths, and borders. Each formatting concern lives in its own helper;
// this function just orchestrates the BatchUpdate.
func (c *Client) formatSheet(ctx context.Context, spreadsheetID string, sheetID int64, rows []tableRow) error {
	requests := []*sheets.Request{boldHeaderRequest(sheetID)}
	requests = append(requests, breakRowRequests(sheetID, rows)...)
	requests = append(requests, columnWidthRequests(sheetID)...)
	requests = append(requests, bordersRequest(sheetID, int64(len(rows)+1)))

	_, err := c.sheetsSvc.Spreadsheets.BatchUpdate(spreadsheetID, &sheets.BatchUpdateSpreadsheetRequest{
		Requests: requests,
	}).Context(ctx).Do()
	return err
}

// boldHeaderRequest formats row 0 (the header) as bold with a light-blue fill.
func boldHeaderRequest(sheetID int64) *sheets.Request {
	return &sheets.Request{
		RepeatCell: &sheets.RepeatCellRequest{
			Range: &sheets.GridRange{
				SheetId:       sheetID,
				StartRowIndex: 0,
				EndRowIndex:   1,
			},
			Cell: &sheets.CellData{
				UserEnteredFormat: &sheets.CellFormat{
					TextFormat:      &sheets.TextFormat{Bold: true},
					BackgroundColor: &sheets.Color{Red: 0.85, Green: 0.92, Blue: 1.0, Alpha: 1.0},
				},
			},
			Fields: "userEnteredFormat(textFormat,backgroundColor)",
		},
	}
}

// breakRowRequests highlights each "Перерыв" row with a warm fill and centers
// the text. Returns nil if there are no break rows.
func breakRowRequests(sheetID int64, rows []tableRow) []*sheets.Request {
	var reqs []*sheets.Request
	for i, row := range rows {
		if row.Type != rowBreak {
			continue
		}
		rowIndex := int64(i + 1) // +1 because row 0 is the header
		reqs = append(reqs, &sheets.Request{
			RepeatCell: &sheets.RepeatCellRequest{
				Range: &sheets.GridRange{
					SheetId:       sheetID,
					StartRowIndex: rowIndex,
					EndRowIndex:   rowIndex + 1,
				},
				Cell: &sheets.CellData{
					UserEnteredFormat: &sheets.CellFormat{
						BackgroundColor:     &sheets.Color{Red: 1.0, Green: 0.95, Blue: 0.8, Alpha: 1.0},
						HorizontalAlignment: "CENTER",
					},
				},
				Fields: "userEnteredFormat(backgroundColor,horizontalAlignment)",
			},
		})
	}
	return reqs
}

// columnWidthRequests sets column A (time) to 80px and B–D (groups) to 200px.
func columnWidthRequests(sheetID int64) []*sheets.Request {
	return []*sheets.Request{
		columnWidthRequest(sheetID, 0, 1, 80),
		columnWidthRequest(sheetID, 1, totalColumns, 200),
	}
}

func columnWidthRequest(sheetID, start, end, pixelSize int64) *sheets.Request {
	return &sheets.Request{
		UpdateDimensionProperties: &sheets.UpdateDimensionPropertiesRequest{
			Range: &sheets.DimensionRange{
				SheetId:    sheetID,
				Dimension:  "COLUMNS",
				StartIndex: start,
				EndIndex:   end,
			},
			Properties: &sheets.DimensionProperties{PixelSize: pixelSize},
			Fields:     "pixelSize",
		},
	}
}

// bordersRequest adds a light-gray solid border around and between every cell
// in the populated range (header + data rows × 4 columns).
func bordersRequest(sheetID, totalRows int64) *sheets.Request {
	border := &sheets.Border{
		Style: "SOLID",
		Color: &sheets.Color{Red: 0.8, Green: 0.8, Blue: 0.8, Alpha: 1.0},
	}
	return &sheets.Request{
		UpdateBorders: &sheets.UpdateBordersRequest{
			Range: &sheets.GridRange{
				SheetId:          sheetID,
				StartRowIndex:    0,
				EndRowIndex:      totalRows,
				StartColumnIndex: 0,
				EndColumnIndex:   totalColumns,
			},
			Top:             border,
			Bottom:          border,
			Left:            border,
			Right:           border,
			InnerVertical:   border,
			InnerHorizontal: border,
		},
	}
}
