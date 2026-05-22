package domain

import "testing"

func TestAllPiscines(t *testing.T) {
	got := AllPiscines()
	want := []PiscineType{PiscineGo, PiscineJS, PiscineAI}
	if len(got) != len(want) {
		t.Fatalf("AllPiscines length = %d, want %d", len(got), len(want))
	}
	for i, p := range want {
		if got[i] != p {
			t.Errorf("AllPiscines[%d] = %q, want %q", i, got[i], p)
		}
	}
}

func TestTotalWeeks(t *testing.T) {
	cases := []struct {
		p    PiscineType
		want int
	}{
		{PiscineGo, 4},
		{PiscineJS, 4},
		{PiscineAI, 3},
		{PiscineType("unknown"), 0},
		{"", 0},
	}
	for _, tc := range cases {
		if got := TotalWeeks(tc.p); got != tc.want {
			t.Errorf("TotalWeeks(%q) = %d, want %d", tc.p, got, tc.want)
		}
	}
}

func TestHasHackathon(t *testing.T) {
	cases := []struct {
		p    PiscineType
		want bool
	}{
		{PiscineGo, true},
		{PiscineJS, true},
		{PiscineAI, false},
		{PiscineType("unknown"), false},
	}
	for _, tc := range cases {
		if got := HasHackathon(tc.p); got != tc.want {
			t.Errorf("HasHackathon(%q) = %v, want %v", tc.p, got, tc.want)
		}
	}
}

func TestIsFinalWeek(t *testing.T) {
	cases := []struct {
		p    PiscineType
		week int
		want bool
	}{
		{PiscineGo, 1, false},
		{PiscineGo, 3, false},
		{PiscineGo, 4, true},
		{PiscineJS, 4, true},
		{PiscineAI, 2, false},
		{PiscineAI, 3, true},
		// Unknown piscine has TotalWeeks == 0, so week 0 is "final" — document this.
		{PiscineType("unknown"), 0, true},
		{PiscineType("unknown"), 1, false},
	}
	for _, tc := range cases {
		if got := IsFinalWeek(tc.p, tc.week); got != tc.want {
			t.Errorf("IsFinalWeek(%q, %d) = %v, want %v", tc.p, tc.week, got, tc.want)
		}
	}
}

func TestGetRaidQueryName(t *testing.T) {
	cases := []struct {
		p    PiscineType
		want string
	}{
		{PiscineGo, "GetRaidsByPiscineGoId"},
		{PiscineJS, "GetRaidsByPiscineJsId"},
		{PiscineAI, "GetRaidsByPiscineAiId"},
		{PiscineType("unknown"), ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := GetRaidQueryName(tc.p); got != tc.want {
			t.Errorf("GetRaidQueryName(%q) = %q, want %q", tc.p, got, tc.want)
		}
	}
}

func TestWeekNumberByRaid(t *testing.T) {
	cases := []struct {
		p    PiscineType
		raid string
		want int
	}{
		// Piscine Go
		{PiscineGo, "quad", 1},
		{PiscineGo, "sudoku", 2},
		{PiscineGo, "quadchecker", 3},
		{PiscineGo, "unknown_raid", 0},

		// Piscine JS
		{PiscineJS, "crossword", 1},
		{PiscineJS, "sortable", 2},
		{PiscineJS, "clonernews", 3},
		{PiscineJS, "missing", 0},

		// Piscine AI
		{PiscineAI, "backtesting-sp500", 1},
		{PiscineAI, "forest-prediction", 2},
		// AI has no week-3 raid.
		{PiscineAI, "anything", 0},

		// Unknown piscine.
		{PiscineType("unknown"), "quad", 0},
	}
	for _, tc := range cases {
		if got := WeekNumberByRaid(tc.p, tc.raid); got != tc.want {
			t.Errorf("WeekNumberByRaid(%q, %q) = %d, want %d", tc.p, tc.raid, got, tc.want)
		}
	}
}

// TestRaidWeekMap_NoCollisions makes sure no raid name maps to more than one
// piscine. If someone adds a raid, this catches accidental cross-wiring.
func TestRaidWeekMap_NoCollisions(t *testing.T) {
	seen := map[string]PiscineType{}
	for p, m := range RaidWeekMap {
		for name := range m {
			if other, ok := seen[name]; ok {
				t.Errorf("raid name %q assigned to both %q and %q", name, other, p)
			}
			seen[name] = p
		}
	}
}

// TestRaidWeekMap_WeeksMatchTotalMinusOne ensures every raid week is in
// 1..TotalWeeks(p)-1 (the final week has no raid).
func TestRaidWeekMap_WeeksMatchTotalMinusOne(t *testing.T) {
	for p, m := range RaidWeekMap {
		maxWeek := TotalWeeks(p) - 1
		for name, week := range m {
			if week < 1 || week > maxWeek {
				t.Errorf("%s: raid %q has week=%d, want 1..%d", p, name, week, maxWeek)
			}
		}
	}
}
