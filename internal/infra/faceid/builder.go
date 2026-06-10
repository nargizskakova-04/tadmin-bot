// Package faceid builds the deliverables for the /face-scripts command: an
// Excel report (one row per login, with the photo embedded) and a zip archive
// of the full-resolution photos.
package faceid

import (
	"archive/zip"
	"bytes"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"

	"admin-bot/internal/usecase"
)

const sheetName = "Face Id"

// photoRowHeight is tall enough to show an embedded thumbnail; AutoFit scales
// the image down to fit the cell.
const photoRowHeight = 90

var headers = []string{"№", "Lastname", "Firstname", "Photo", "IIN/ID"}

// BuildExcel renders the records into an .xlsx workbook and returns its bytes.
// Photos are embedded directly into the Photo column when available.
func BuildExcel(records []usecase.FaceRecord) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()

	// NewFile creates a default "Sheet1"; rename it instead of adding a second.
	if err := f.SetSheetName("Sheet1", sheetName); err != nil {
		return nil, fmt.Errorf("name sheet: %w", err)
	}

	headerStyle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF", Size: 11, Family: "Arial"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"2F5496"}},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border:    thinBorder(),
	})
	if err != nil {
		return nil, fmt.Errorf("header style: %w", err)
	}

	cellStyle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10, Family: "Arial"},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
		Border:    thinBorder(),
	})
	if err != nil {
		return nil, fmt.Errorf("cell style: %w", err)
	}

	// Column widths roughly match the reference script.
	_ = f.SetColWidth(sheetName, "A", "A", 6)
	_ = f.SetColWidth(sheetName, "B", "C", 22)
	_ = f.SetColWidth(sheetName, "D", "D", 24)
	_ = f.SetColWidth(sheetName, "E", "E", 22)

	// Header row.
	for col, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(col+1, 1)
		_ = f.SetCellStr(sheetName, cell, h)
		_ = f.SetCellStyle(sheetName, cell, cell, headerStyle)
	}
	_ = f.SetRowHeight(sheetName, 1, 22)

	// Data rows.
	for i, rec := range records {
		row := i + 2
		_ = f.SetCellInt(sheetName, cellRef(1, row), int64(rec.Index))
		_ = f.SetCellStr(sheetName, cellRef(2, row), displayName(rec.LastName, rec))
		_ = f.SetCellStr(sheetName, cellRef(3, row), rec.FirstName)
		// IIN as a string preserves leading zeros.
		_ = f.SetCellStr(sheetName, cellRef(5, row), rec.IIN)

		photoCell := cellRef(4, row)
		if len(rec.Photo) > 0 {
			if err := embedPhoto(f, photoCell, rec); err != nil {
				// Fall back to the file name so the row still references the zip.
				_ = f.SetCellStr(sheetName, photoCell, rec.PhotoName)
			} else {
				_ = f.SetRowHeight(sheetName, row, photoRowHeight)
			}
		} else if note := rowNote(rec); note != "" {
			_ = f.SetCellStr(sheetName, photoCell, note)
		}

		_ = f.SetCellStyle(sheetName, cellRef(1, row), cellRef(5, row), cellStyle)
	}

	// Freeze the header row.
	_ = f.SetPanes(sheetName, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("write workbook: %w", err)
	}
	return buf.Bytes(), nil
}

// BuildZip packs every record's photo into a zip archive. Records without a
// photo are skipped. Returns the archive bytes and the number of photos added.
func BuildZip(records []usecase.FaceRecord) ([]byte, int, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	count := 0
	for _, rec := range records {
		if len(rec.Photo) == 0 {
			continue
		}
		w, err := zw.Create(rec.PhotoName)
		if err != nil {
			return nil, 0, fmt.Errorf("zip create %q: %w", rec.PhotoName, err)
		}
		if _, err := w.Write(rec.Photo); err != nil {
			return nil, 0, fmt.Errorf("zip write %q: %w", rec.PhotoName, err)
		}
		count++
	}

	if err := zw.Close(); err != nil {
		return nil, 0, fmt.Errorf("close zip: %w", err)
	}
	return buf.Bytes(), count, nil
}

func embedPhoto(f *excelize.File, cell string, rec usecase.FaceRecord) error {
	ext := strings.ToLower(filepath.Ext(rec.PhotoName))
	if ext == "" {
		ext = ".jpg"
	}
	return f.AddPictureFromBytes(sheetName, cell, &excelize.Picture{
		Extension: ext,
		File:      rec.Photo,
		Format: &excelize.GraphicOptions{
			AutoFit: true,
			OffsetX: 2,
			OffsetY: 2,
			AltText: rec.PhotoName,
		},
	})
}

// displayName shows the last name, or a note when the login could not be
// resolved, so empty rows aren't silently blank.
func displayName(lastName string, rec usecase.FaceRecord) string {
	if lastName != "" {
		return lastName
	}
	if note := rowNote(rec); note != "" {
		return note
	}
	return ""
}

func rowNote(rec usecase.FaceRecord) string {
	switch {
	case rec.NotFound:
		return fmt.Sprintf("не найден: %s", rec.Login)
	case rec.Err != "":
		return rec.Err
	default:
		return ""
	}
}

func cellRef(col, row int) string {
	c, _ := excelize.CoordinatesToCellName(col, row)
	return c
}

func thinBorder() []excelize.Border {
	const color = "BFBFBF"
	return []excelize.Border{
		{Type: "left", Color: color, Style: 1},
		{Type: "right", Color: color, Style: 1},
		{Type: "top", Color: color, Style: 1},
		{Type: "bottom", Color: color, Style: 1},
	}
}
