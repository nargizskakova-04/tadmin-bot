package domain

import "time"

// PiscineType identifies a piscine programme.
type PiscineType string

const (
	PiscineGo PiscineType = "Piscine Go"
	PiscineJS PiscineType = "Piscine JS"
	PiscineAI PiscineType = "Piscine AI"
)

// AllPiscines returns every known piscine type.
func AllPiscines() []PiscineType {
	return []PiscineType{PiscineGo, PiscineJS, PiscineAI}
}

// TotalWeeks returns how many weeks a piscine lasts (including final exam week).
func TotalWeeks(p PiscineType) int {
	switch p {
	case PiscineGo, PiscineJS:
		return 4
	case PiscineAI:
		return 3
	default:
		return 0
	}
}

// HasHackathon returns true if this piscine type includes a hackathon on week 3.
func HasHackathon(p PiscineType) bool {
	return p == PiscineGo || p == PiscineJS
}

// IsFinalWeek returns true if weekNumber is the last week (no raid, no defense table).
func IsFinalWeek(p PiscineType, weekNumber int) bool {
	return weekNumber == TotalWeeks(p)
}

// RaidWeekMap maps raid names to week numbers for each piscine type.
var RaidWeekMap = map[PiscineType]map[string]int{
	PiscineGo: {
		"quad":        1,
		"sudoku":      2,
		"quadchecker": 3,
	},
	PiscineJS: {
		"crossword":  1,
		"sortable":   2,
		"clonernews": 3,
	},
	PiscineAI: {
		"backtesting-sp500": 1,
		"forest-prediction": 2,
	},
}

// GetRaidQueryName returns the GraphQL operation name for fetching raids
// of a specific piscine type.
func GetRaidQueryName(p PiscineType) string {
	switch p {
	case PiscineGo:
		return "GetRaidsByPiscineGoId"
	case PiscineJS:
		return "GetRaidsByPiscineJsId"
	case PiscineAI:
		return "GetRaidsByPiscineAiId"
	default:
		return ""
	}
}

// WeekNumberByRaid returns the week number for a given raid name within a piscine.
// Returns 0 if the raid name is not recognized.
func WeekNumberByRaid(p PiscineType, raidName string) int {
	if m, ok := RaidWeekMap[p]; ok {
		return m[raidName]
	}
	return 0
}

// Team represents a single raid team (group).
type Team struct {
	Captain string
	Members []string
	Status  string
}

// RaidInfo is the aggregated data about a specific raid event.
type RaidInfo struct {
	Piscine    PiscineType
	EventID    int
	RaidName   string
	WeekNumber int
	TeamsCount int
	Teams      []Team
	StartDate  time.Time
	EndDate    time.Time
}

// PiscineInfo holds the active piscine event data.
type PiscineInfo struct {
	ID      int
	StartAt time.Time
	EndAt   time.Time
}
