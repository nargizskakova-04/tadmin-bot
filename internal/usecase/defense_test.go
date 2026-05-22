package usecase

import (
	"strings"
	"testing"
)

// TestCalculateDefenseSchedule_Rows checks the basic ceil(teams/3) row count.
func TestCalculateDefenseSchedule_Rows(t *testing.T) {
	cases := []struct {
		teams    int
		wantRows int
	}{
		{1, 1},
		{2, 1},
		{3, 1},
		{4, 2},
		{6, 2},
		{7, 3},
		{15, 5},
		{16, 6},
		{35, 12}, // matches the README example
		{0, 0},
	}
	for _, tc := range cases {
		s := CalculateDefenseSchedule(tc.teams)
		if s.Rows != tc.wantRows {
			t.Errorf("teams=%d: Rows=%d, want %d", tc.teams, s.Rows, tc.wantRows)
		}
		if s.TotalSlots != s.Rows*3 {
			t.Errorf("teams=%d: TotalSlots=%d, want %d", tc.teams, s.TotalSlots, s.Rows*3)
		}
	}
}

// TestCalculateDefenseSchedule_Breaks verifies the break placement rules.
// <5 rows -> no break; 5..10 -> one; >10 -> two; max consecutive 5 rows.
func TestCalculateDefenseSchedule_Breaks(t *testing.T) {
	cases := []struct {
		name       string
		teams      int
		wantBreaks []int
	}{
		// rows = ceil(teams/3)
		{"3 teams, 1 row", 3, nil},
		{"12 teams, 4 rows", 12, nil},

		// 5..10 rows -> one break (rows+1)/2
		{"13 teams, 5 rows", 13, []int{3}},
		{"16 teams, 6 rows", 16, []int{3}},
		{"19 teams, 7 rows", 19, []int{4}},
		{"22 teams, 8 rows", 22, []int{4}},
		{"25 teams, 9 rows", 25, []int{5}},
		{"28 teams, 10 rows", 28, []int{5}},

		// >10 rows -> two breaks splitting into ~thirds
		{"31 teams, 11 rows", 31, []int{4, 8}},  // 4/4/3
		{"34 teams, 12 rows", 34, []int{4, 8}},  // 4/4/4
		{"35 teams, 12 rows (README)", 35, []int{4, 8}},
		{"37 teams, 13 rows", 37, []int{5, 9}},  // 5/4/4

		// Enforce max 5 consecutive rows: 15 rows would split 5/5/5;
		// 16 rows would split 6/5/5, so the first segment >5 must get an extra break.
		{"46 teams, 16 rows", 46, []int{5, 6, 11}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := CalculateDefenseSchedule(tc.teams)
			if !equalInts(s.BreakAfterRows, tc.wantBreaks) {
				t.Errorf("teams=%d: BreakAfterRows=%v, want %v",
					tc.teams, s.BreakAfterRows, tc.wantBreaks)
			}
			assertMaxConsecutive(t, s.BreakAfterRows, s.Rows, 5)
		})
	}
}

// TestCalculateDefenseSchedule_Schedule confirms the human-readable schedule.
func TestCalculateDefenseSchedule_Schedule(t *testing.T) {
	// 35 teams -> 12 rows, breaks after rows 4 and 8 (README example).
	// Timeline: 11:00 + 4 slots = 13:00 -> break -> 13:30 + 4 slots = 15:30
	// -> break -> 16:00 + 4 slots = 18:00. Last slot starts at 17:30.
	s := CalculateDefenseSchedule(35)

	if got, want := s.StartTime, "11:00"; got != want {
		t.Errorf("StartTime=%q, want %q", got, want)
	}
	if got, want := s.EndTime, "18:00"; got != want {
		t.Errorf("EndTime=%q, want %q (end of last slot)", got, want)
	}
	wantBreaks := []string{"13:00", "15:30"}
	if !equalStrs(s.BreakTimes, wantBreaks) {
		t.Errorf("BreakTimes=%v, want %v", s.BreakTimes, wantBreaks)
	}

	// Range in the rendered schedule shows start..start-of-last-slot.
	if !strings.HasPrefix(s.RecommendedSchedule, "11:00–17:30") {
		t.Errorf("RecommendedSchedule should start with 11:00–17:30, got %q",
			s.RecommendedSchedule)
	}
	if !strings.Contains(s.RecommendedSchedule, "Перерывы: 13:00 и 15:30") {
		t.Errorf("RecommendedSchedule missing breaks line, got %q", s.RecommendedSchedule)
	}
}

// TestCalculateDefenseSchedule_ScheduleLabel covers all three break-count branches
// of the rendered text (no break / one break / two breaks).
func TestCalculateDefenseSchedule_ScheduleLabel(t *testing.T) {
	cases := []struct {
		teams       int
		mustContain []string
		mustExclude []string
	}{
		{3, []string{"11:00"}, []string{"Перерыв"}},     // no breaks
		{15, []string{"Перерыв: "}, []string{"Перерывы:"}}, // one break (5 rows)
		{35, []string{"Перерывы: "}, []string{"Перерыв: "}}, // two breaks
	}
	for _, tc := range cases {
		s := CalculateDefenseSchedule(tc.teams)
		for _, want := range tc.mustContain {
			if !strings.Contains(s.RecommendedSchedule, want) {
				t.Errorf("teams=%d: schedule %q missing %q",
					tc.teams, s.RecommendedSchedule, want)
			}
		}
		for _, bad := range tc.mustExclude {
			if strings.Contains(s.RecommendedSchedule, bad) {
				t.Errorf("teams=%d: schedule %q should not contain %q",
					tc.teams, s.RecommendedSchedule, bad)
			}
		}
	}
}

func TestCalculateDefenseSchedule_TeamsCountIsRecorded(t *testing.T) {
	s := CalculateDefenseSchedule(7)
	if s.TeamsCount != 7 {
		t.Errorf("TeamsCount=%d, want 7", s.TeamsCount)
	}
}

// --- helpers ---

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// assertMaxConsecutive verifies no segment exceeds max consecutive rows
// without a break (and the tail from the last break to totalRows).
func assertMaxConsecutive(t *testing.T, breaks []int, totalRows, max int) {
	t.Helper()
	prev := 0
	for _, b := range breaks {
		if b-prev > max {
			t.Errorf("segment from row %d..%d exceeds max %d", prev+1, b, max)
		}
		prev = b
	}
	if totalRows-prev > max {
		t.Errorf("tail segment from row %d..%d exceeds max %d", prev+1, totalRows, max)
	}
}
