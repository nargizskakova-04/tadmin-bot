package telegram

import (
	"testing"
	"time"
)

func TestParsePiscineFromCallback(t *testing.T) {
	cases := []struct {
		name   string
		data   string
		prefix string
		want   string
	}{
		{"happy_path", "defense_create:Piscine Go", "defense_create:", "Piscine Go"},
		{"different_prefix", "defense_edit:Piscine JS", "defense_edit:", "Piscine JS"},
		{"missing_prefix", "wrong:Piscine Go", "defense_create:", ""},
		{"empty_data", "", "defense_create:", ""},
		{"prefix_only", "defense_create:", "defense_create:", ""},
		{"value_contains_colon", "defense_create:Piscine: AI", "defense_create:", "Piscine: AI"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := parsePiscineFromCallback(tc.data, tc.prefix)
			if got != tc.want {
				t.Errorf("parsePiscineFromCallback(%q, %q) = %q, want %q",
					tc.data, tc.prefix, got, tc.want)
			}
		})
	}
}

// TestNextMonday verifies the helper returns the *next* Monday (never today)
// at 00:00 in the input's location, for every weekday.
func TestNextMonday(t *testing.T) {
	loc := time.UTC
	cases := []struct {
		name   string
		in     time.Time
		wantDM string // YYYY-MM-DD
	}{
		// 2024-01-01 was a Monday.
		{"monday_morning_returns_next_monday", time.Date(2024, 1, 1, 9, 0, 0, 0, loc), "2024-01-08"},
		{"tuesday", time.Date(2024, 1, 2, 9, 0, 0, 0, loc), "2024-01-08"},
		{"wednesday", time.Date(2024, 1, 3, 9, 0, 0, 0, loc), "2024-01-08"},
		{"thursday", time.Date(2024, 1, 4, 9, 0, 0, 0, loc), "2024-01-08"},
		{"friday", time.Date(2024, 1, 5, 9, 0, 0, 0, loc), "2024-01-08"},
		{"saturday", time.Date(2024, 1, 6, 9, 0, 0, 0, loc), "2024-01-08"},
		{"sunday_returns_tomorrow", time.Date(2024, 1, 7, 9, 0, 0, 0, loc), "2024-01-08"},
		// Crossing a month boundary.
		{"end_of_month_friday", time.Date(2024, 1, 26, 9, 0, 0, 0, loc), "2024-01-29"},
		{"end_of_month_sunday", time.Date(2024, 1, 28, 9, 0, 0, 0, loc), "2024-01-29"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := nextMonday(tc.in)
			gotStr := got.Format("2006-01-02")
			if gotStr != tc.wantDM {
				t.Errorf("nextMonday(%s) = %s, want %s", tc.in.Format("2006-01-02 Mon"), gotStr, tc.wantDM)
			}
			if got.Weekday() != time.Monday {
				t.Errorf("nextMonday(%s) returned weekday %s, want Monday",
					tc.in.Format("2006-01-02 Mon"), got.Weekday())
			}
			if got.Hour() != 0 || got.Minute() != 0 || got.Second() != 0 || got.Nanosecond() != 0 {
				t.Errorf("nextMonday(%s) should be midnight, got %v", tc.in, got)
			}
		})
	}
}

// TestNextMonday_PreservesInputLocation documents that the helper returns a time
// in the same location as its input. (See the "unresolved concerns" note about
// the implication for timezone-aware scheduling.)
func TestNextMonday_PreservesInputLocation(t *testing.T) {
	almaty, err := time.LoadLocation("Asia/Almaty")
	if err != nil {
		t.Skip("Asia/Almaty tzdata not available")
	}
	in := time.Date(2024, 1, 7, 23, 0, 0, 0, almaty)
	got := nextMonday(in)
	if got.Location().String() != almaty.String() {
		t.Errorf("nextMonday returned location %q, want %q", got.Location(), almaty)
	}
}
