package usecase

import "testing"

// TestCalculateDefenseSchedule_Columns verifies that the number of columns
// drives the row count (rows = ceil(teams/columns)) and is carried through to
// the schedule, while the default 3-column layout reproduces the historical
// behavior.
func TestCalculateDefenseSchedule_Columns(t *testing.T) {
	cases := []struct {
		name       string
		teams      int
		columns    int
		wantRows   int
		wantSlots  int
		wantEnd    string // no breaks expected in these small cases OR verified separately
		wantEndSet bool
	}{
		{"default_3_cols_9_teams", 9, 3, 3, 9, "12:30", true},    // 3 rows < 5 → no break
		{"5_cols_30_teams", 30, 5, 6, 30, "", false},             // 6 rows → 1 break
		{"10_cols_30_teams", 30, 10, 3, 30, "12:30", true},       // 3 rows < 5 → no break
		{"1_col_4_teams", 4, 1, 4, 4, "13:00", true},             // 4 rows < 5 → no break
		{"zero_cols_defaults_to_one", 3, 0, 3, 3, "12:30", true}, // guarded to 1 column
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := CalculateDefenseSchedule(ScheduleParams{
				TeamsCount:    tc.teams,
				Columns:       tc.columns,
				StartHour:     DefaultStartHour,
				StartMinute:   DefaultStartMinute,
				IncludeBreaks: true,
			})
			if got.Rows != tc.wantRows {
				t.Errorf("Rows = %d, want %d", got.Rows, tc.wantRows)
			}
			if got.TotalSlots != tc.wantSlots {
				t.Errorf("TotalSlots = %d, want %d", got.TotalSlots, tc.wantSlots)
			}
			wantCols := tc.columns
			if wantCols < 1 {
				wantCols = 1
			}
			if got.Columns != wantCols {
				t.Errorf("Columns = %d, want %d", got.Columns, wantCols)
			}
			if tc.wantEndSet && got.EndTime != tc.wantEnd {
				t.Errorf("EndTime = %q, want %q", got.EndTime, tc.wantEnd)
			}
		})
	}
}

// TestCalculateDefenseSchedule_NoBreaks verifies that IncludeBreaks=false skips
// break computation entirely: no break rows, and the end time reflects pure
// slot time with no inserted gaps.
func TestCalculateDefenseSchedule_NoBreaks(t *testing.T) {
	withBreaks := CalculateDefenseSchedule(ScheduleParams{
		TeamsCount: 30, Columns: 3, StartHour: 11, IncludeBreaks: true,
	})
	if len(withBreaks.BreakAfterRows) == 0 {
		t.Fatal("precondition: expected breaks with IncludeBreaks=true for 10 rows")
	}

	noBreaks := CalculateDefenseSchedule(ScheduleParams{
		TeamsCount: 30, Columns: 3, StartHour: 11, IncludeBreaks: false,
	})
	if len(noBreaks.BreakAfterRows) != 0 {
		t.Errorf("BreakAfterRows = %v, want empty when IncludeBreaks=false", noBreaks.BreakAfterRows)
	}
	if len(noBreaks.BreakTimes) != 0 {
		t.Errorf("BreakTimes = %v, want empty when IncludeBreaks=false", noBreaks.BreakTimes)
	}
	// 10 rows × 30min from 11:00, no breaks → 16:00 (vs 16:30 with one break).
	if noBreaks.EndTime != "16:00" {
		t.Errorf("EndTime = %q, want %q", noBreaks.EndTime, "16:00")
	}
}

// TestCalculateDefenseSchedule_StartHour verifies the start time flows into the
// computed times (so /edit_tables can shift the whole schedule).
func TestCalculateDefenseSchedule_StartHour(t *testing.T) {
	got := CalculateDefenseSchedule(ScheduleParams{
		TeamsCount: 3, Columns: 3, StartHour: 9, StartMinute: 30, IncludeBreaks: true,
	})
	if got.StartTime != "09:30" {
		t.Errorf("StartTime = %q, want %q", got.StartTime, "09:30")
	}
	// 1 row × 30min from 09:30 → 10:00.
	if got.EndTime != "10:00" {
		t.Errorf("EndTime = %q, want %q", got.EndTime, "10:00")
	}
}

// TestCalculateDefenseSchedule_Shortfall documents the data behind the
// /edit_tables shortfall warning: with too few columns the computed end time
// runs past a requested window. Here 30 teams at 3 columns from 11:00 finishes
// at 16:30 — later than a requested 14:00 close.
func TestCalculateDefenseSchedule_Shortfall(t *testing.T) {
	got := CalculateDefenseSchedule(ScheduleParams{
		TeamsCount: 30, Columns: 3, StartHour: 11, IncludeBreaks: true,
	})
	if got.EndTime != "16:30" {
		t.Errorf("EndTime = %q, want %q", got.EndTime, "16:30")
	}
	// The computed end (16:30) exceeds a desired 14:00 window → shortfall.
	if got.EndTime <= "14:00" {
		t.Errorf("expected computed end %q to exceed the 14:00 window", got.EndTime)
	}
}

// TestCalculateDefenseScheduleWindow verifies the /edit_tables layout: rows are
// however many whole slots fit the window, independent of team count, with an
// optional break of one slot's length placed at the requested time.
func TestCalculateDefenseScheduleWindow(t *testing.T) {
	t.Run("fills_window_no_breaks", func(t *testing.T) {
		got := CalculateDefenseScheduleWindow(WindowScheduleParams{
			StartHour: 11, EndHour: 17, SlotMinutes: 30, Columns: 3,
		})
		if got.Rows != 12 { // 360 min / 30
			t.Errorf("Rows = %d, want 12", got.Rows)
		}
		if got.TotalSlots != 36 { // 12 * 3
			t.Errorf("TotalSlots = %d, want 36", got.TotalSlots)
		}
		if got.EndTime != "17:00" {
			t.Errorf("EndTime = %q, want 17:00", got.EndTime)
		}
		if len(got.BreakAfterRows) != 0 {
			t.Errorf("BreakAfterRows = %v, want none", got.BreakAfterRows)
		}
	})

	t.Run("slot_length_changes_row_count", func(t *testing.T) {
		got := CalculateDefenseScheduleWindow(WindowScheduleParams{
			StartHour: 11, EndHour: 17, SlotMinutes: 40, Columns: 2,
		})
		if got.Rows != 9 { // 360 / 40
			t.Errorf("Rows = %d, want 9", got.Rows)
		}
		if got.SlotMinutes != 40 {
			t.Errorf("SlotMinutes = %d, want 40", got.SlotMinutes)
		}
		if got.EndTime != "17:00" {
			t.Errorf("EndTime = %q, want 17:00", got.EndTime)
		}
	})

	t.Run("break_at_requested_time", func(t *testing.T) {
		got := CalculateDefenseScheduleWindow(WindowScheduleParams{
			StartHour: 11, EndHour: 17, SlotMinutes: 30, Columns: 3,
			IncludeBreaks: true, BreakHour: 14,
		})
		// 6 slots (11:00–14:00), a break at 14:00, then 5 slots (14:30–17:00).
		if got.Rows != 11 {
			t.Errorf("Rows = %d, want 11", got.Rows)
		}
		if len(got.BreakAfterRows) != 1 || got.BreakAfterRows[0] != 6 {
			t.Errorf("BreakAfterRows = %v, want [6]", got.BreakAfterRows)
		}
		if len(got.BreakTimes) != 1 || got.BreakTimes[0] != "14:00" {
			t.Errorf("BreakTimes = %v, want [14:00]", got.BreakTimes)
		}
		if got.EndTime != "17:00" {
			t.Errorf("EndTime = %q, want 17:00", got.EndTime)
		}
	})

	t.Run("window_shorter_than_one_slot", func(t *testing.T) {
		got := CalculateDefenseScheduleWindow(WindowScheduleParams{
			StartHour: 11, StartMinute: 0, EndHour: 11, EndMinute: 20, SlotMinutes: 30, Columns: 3,
		})
		if got.Rows != 0 || got.TotalSlots != 0 {
			t.Errorf("Rows=%d TotalSlots=%d, want 0/0", got.Rows, got.TotalSlots)
		}
	})
}

// TestDefaultScheduleParams pins the historical defaults so the automatic paths
// (/create_tables, scheduled reminder) keep producing the same layout.
func TestDefaultScheduleParams(t *testing.T) {
	p := DefaultScheduleParams(12)
	if p.TeamsCount != 12 || p.Columns != 3 || p.StartHour != 11 || p.StartMinute != 0 || !p.IncludeBreaks {
		t.Errorf("DefaultScheduleParams(12) = %+v, want {12, 3, 11, 0, true}", p)
	}
}
