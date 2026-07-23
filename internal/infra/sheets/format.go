package sheets

import (
	"context"

	"google.golang.org/api/sheets/v4"
)

// formatSheet applies basic formatting: bold header, colored break rows,
// column widths, and borders. Each formatting concern lives in its own helper;
// this function just orchestrates the BatchUpdate.
func (c *Client) formatSheet(ctx context.Context, spreadsheetID string, sheetID int64, rows []tableRow, groupColumns int) error {
	if groupColumns < 1 {
		groupColumns = 1
	}
	totalColumns := int64(groupColumns + 1) // +1 for the time column

	requests := []*sheets.Request{boldHeaderRequest(sheetID)}
	requests = append(requests, breakRowRequests(sheetID, rows)...)
	requests = append(requests, columnWidthRequests(sheetID, totalColumns)...)
	requests = append(requests, bordersRequest(sheetID, int64(len(rows)+1), totalColumns))

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

// columnWidthRequests sets column A (time) to 80px and the group columns to
// 200px each.
func columnWidthRequests(sheetID, totalColumns int64) []*sheets.Request {
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
// in the populated range (header + data rows × totalColumns).
func bordersRequest(sheetID, totalRows, totalColumns int64) *sheets.Request {
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
