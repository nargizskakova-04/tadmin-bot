package sheets

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/api/sheets/v4"
)

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
