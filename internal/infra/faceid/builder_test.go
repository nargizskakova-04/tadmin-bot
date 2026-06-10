package faceid

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/xuri/excelize/v2"

	"admin-bot/internal/usecase"
)

// tinyPNG returns the bytes of a valid 2x2 PNG so image embedding has something
// real to decode.
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func sampleRecords(t *testing.T) []usecase.FaceRecord {
	t.Helper()
	photo := tinyPNG(t)
	return []usecase.FaceRecord{
		{Index: 1, Login: "ivan", LastName: "Иванов", FirstName: "Иван", IIN: "001234567890", PhotoName: "01_Иванов Иван.png", Photo: photo},
		{Index: 2, Login: "ghost", NotFound: true},
		{Index: 3, Login: "boom", Err: "не удалось получить данные пользователя"},
	}
}

func TestBuildExcel(t *testing.T) {
	data, err := BuildExcel(sampleRecords(t))
	if err != nil {
		t.Fatalf("BuildExcel: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("BuildExcel returned no bytes")
	}

	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("reopen workbook: %v", err)
	}
	defer f.Close()

	// Headers must be on the renamed sheet.
	got, err := f.GetCellValue(sheetName, "A1")
	if err != nil {
		t.Fatalf("get A1: %v", err)
	}
	if got != "№" {
		t.Errorf("A1 = %q, want %q", got, "№")
	}

	// IIN keeps its leading zero (stored as a string).
	iin, _ := f.GetCellValue(sheetName, "E2")
	if iin != "001234567890" {
		t.Errorf("E2 (IIN) = %q, want %q", iin, "001234567890")
	}

	// The embedded photo should be present on the first data row.
	pics, err := f.GetPictures(sheetName, "D2")
	if err != nil {
		t.Fatalf("get pictures: %v", err)
	}
	if len(pics) == 0 {
		t.Error("expected an embedded picture in D2, found none")
	}

	// A not-found login is annotated, not silently blank.
	note, _ := f.GetCellValue(sheetName, "B3")
	if note == "" {
		t.Error("expected a note for the not-found row in B3")
	}
}

func TestBuildZip(t *testing.T) {
	data, count, err := BuildZip(sampleRecords(t))
	if err != nil {
		t.Fatalf("BuildZip: %v", err)
	}
	if count != 1 {
		t.Errorf("photo count = %d, want 1", count)
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	if len(zr.File) != 1 {
		t.Fatalf("zip has %d files, want 1", len(zr.File))
	}
	if zr.File[0].Name != "01_Иванов Иван.png" {
		t.Errorf("zip entry = %q, want %q", zr.File[0].Name, "01_Иванов Иван.png")
	}
}
