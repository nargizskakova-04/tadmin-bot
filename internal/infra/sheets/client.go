package sheets

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"

	"admin-bot/internal/usecase"
)

// Client wraps Google Sheets and Drive APIs.
type Client struct {
	sheetsSvc *sheets.Service
	driveSvc  *drive.Service
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
)

// NewClient creates a Sheets client using a service account credentials JSON file.
func NewClient(credentialsFile string, logger *slog.Logger) (*Client, error) {
	ctx := context.Background()

	sheetsSvc, err := sheets.NewService(ctx, option.WithCredentialsFile(credentialsFile))
	if err != nil {
		return nil, fmt.Errorf("create sheets service: %w", err)
	}

	driveSvc, err := drive.NewService(ctx, option.WithCredentialsFile(credentialsFile))
	if err != nil {
		return nil, fmt.Errorf("create drive service: %w", err)
	}

	return &Client{
		sheetsSvc: sheetsSvc,
		driveSvc:  driveSvc,
		logger:    logger,
	}, nil
}

// DefenseTableParams holds everything needed to create a defense table.
type DefenseTableParams struct {
	RaidName    string
	DefenseDate time.Time // Monday date for the defense
	Schedule    usecase.DefenseSchedule
}

// CreateDefenseTable creates a new Google Sheet with the defense schedule
// and returns the public URL.
func (c *Client) CreateDefenseTable(ctx context.Context, params DefenseTableParams) (string, error) {
	// 1. Build the spreadsheet data.
	title := fmt.Sprintf("Защита %s — %s", params.RaidName, params.DefenseDate.Format("02.01.2006"))

	rowData := buildRows(params)

	spreadsheet := &sheets.Spreadsheet{
		Properties: &sheets.SpreadsheetProperties{
			Title: title,
		},
		Sheets: []*sheets.Sheet{
			{
				Properties: &sheets.SheetProperties{
					Title: "Защита",
					GridProperties: &sheets.GridProperties{
						ColumnCount: totalColumns,
						RowCount:    int64(len(rowData) + 1),
					},
				},
			},
		},
	}

	// 2. Create the spreadsheet.
	created, err := c.sheetsSvc.Spreadsheets.Create(spreadsheet).Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("create spreadsheet: %w", err)
	}

	spreadsheetID := created.SpreadsheetId
	c.logger.Info("spreadsheet created", "id", spreadsheetID, "title", title)

	// 3. Populate with data.
	if err := c.populateSheet(ctx, spreadsheetID, params, rowData); err != nil {
		return "", fmt.Errorf("populate sheet: %w", err)
	}

	// 4. Format the sheet.
	if err := c.formatSheet(ctx, spreadsheetID, created.Sheets[0].Properties.SheetId, rowData); err != nil {
		c.logger.Warn("formatting failed (non-critical)", "err", err)
	}

	// 5. Make it publicly editable.
	if err := c.makePublicEditable(ctx, spreadsheetID); err != nil {
		return "", fmt.Errorf("set permissions: %w", err)
	}

	url := fmt.Sprintf("https://docs.google.com/spreadsheets/d/%s/edit", spreadsheetID)
	c.logger.Info("defense table ready", "url", url)

	return url, nil
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
func (c *Client) populateSheet(ctx context.Context, spreadsheetID string, params DefenseTableParams, rows []tableRow) error {
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

	rangeStr := fmt.Sprintf("Защита!A1:D%d", len(values))
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

// makePublicEditable sets the spreadsheet so anyone with the link can edit.
func (c *Client) makePublicEditable(ctx context.Context, spreadsheetID string) error {
	_, err := c.driveSvc.Permissions.Create(spreadsheetID, &drive.Permission{
		Type: "anyone",
		Role: "writer",
	}).Context(ctx).Do()
	return err
}
