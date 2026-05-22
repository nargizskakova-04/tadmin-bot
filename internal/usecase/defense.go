package usecase

import (
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	columnsPerRow      = 3
	slotDuration       = 30 * time.Minute
	breakDuration      = 30 * time.Minute
	defenseStartHour   = 11
	defenseStartMin    = 0
	maxConsecutiveRows = 5
)

// DefenseSchedule holds the calculated defense table parameters.
type DefenseSchedule struct {
	TeamsCount          int
	Rows                int
	TotalSlots          int
	BreakAfterRows      []int  // row numbers after which a break is inserted
	RecommendedSchedule string // e.g. "11:00–17:30\nПерерывы: 13:30 и 15:30"
	StartTime           string
	EndTime             string
	BreakTimes          []string // formatted break times
}

// CalculateDefenseSchedule computes the defense table layout for a given number of teams.
func CalculateDefenseSchedule(teamsCount int) DefenseSchedule {
	rows := int(math.Ceil(float64(teamsCount) / float64(columnsPerRow)))
	totalSlots := rows * columnsPerRow

	breakAfterRows := computeBreaks(rows)
	breakAfterRows = enforceMaxConsecutive(breakAfterRows, rows)

	startTime := time.Date(2000, 1, 1, defenseStartHour, defenseStartMin, 0, 0, time.UTC)
	endTime, breakTimes := computeTimes(startTime, rows, breakAfterRows)

	schedule := formatSchedule(startTime, endTime, breakTimes)

	return DefenseSchedule{
		TeamsCount:          teamsCount,
		Rows:                rows,
		TotalSlots:          totalSlots,
		BreakAfterRows:      breakAfterRows,
		RecommendedSchedule: schedule,
		StartTime:           formatTime(startTime),
		EndTime:             formatTime(endTime),
		BreakTimes:          breakTimes,
	}
}

// computeBreaks determines after which rows breaks should be placed.
//   - fewer than 5 rows: no break
//   - 5–10 rows: one break, slightly biased toward the front when odd
//   - more than 10 rows: two breaks, splitting into three roughly equal segments
func computeBreaks(rows int) []int {
	const (
		oneBreakMin = 5
		oneBreakMax = 10
	)

	switch {
	case rows < oneBreakMin:
		return nil
	case rows <= oneBreakMax:
		// (rows+1)/2 == ceil(rows/2): for 5→3, 6→3, 7→4, 8→4, 9→5, 10→5.
		return []int{(rows + 1) / 2}
	default:
		seg1, seg2, _ := splitIntoThree(rows)
		return []int{seg1, seg1 + seg2}
	}
}

// splitIntoThree splits n into three segments as equal as possible.
// Any remainder is distributed to the earlier segments first.
func splitIntoThree(n int) (a, b, c int) {
	base := n / 3
	rem := n % 3
	a, b, c = base, base, base
	if rem >= 1 {
		a++
	}
	if rem >= 2 {
		b++
	}
	return a, b, c
}

// enforceMaxConsecutive ensures no more than maxConsecutiveRows consecutive rows
// without a break. Inserts additional breaks if needed.
func enforceMaxConsecutive(breaks []int, totalRows int) []int {
	var result []int
	prev := 0

	for _, b := range breaks {
		gap := b - prev
		for gap > maxConsecutiveRows {
			prev += maxConsecutiveRows
			result = append(result, prev)
			gap = b - prev
		}
		result = append(result, b)
		prev = b
	}

	// Check the last segment (from last break to end).
	remaining := totalRows - prev
	for remaining > maxConsecutiveRows {
		prev += maxConsecutiveRows
		result = append(result, prev)
		remaining = totalRows - prev
	}

	return result
}

// computeTimes calculates the end time and break start times.
func computeTimes(start time.Time, rows int, breakAfterRows []int) (endTime time.Time, breakTimes []string) {
	breakSet := make(map[int]bool)
	for _, b := range breakAfterRows {
		breakSet[b] = true
	}

	current := start
	for row := 1; row <= rows; row++ {
		current = current.Add(slotDuration)

		if breakSet[row] {
			breakTimes = append(breakTimes, formatTime(current))
			current = current.Add(breakDuration)
		}
	}

	return current, breakTimes
}

// formatSchedule builds the human-readable schedule string.
func formatSchedule(start, end time.Time, breakTimes []string) string {
	// End time displayed is the start of the last slot, not the end.
	// Actually we display as range: "11:00–17:30"
	// The end time from computeTimes is AFTER the last slot, so subtract one slot.
	lastSlotStart := end.Add(-slotDuration)

	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s–%s", formatTime(start), formatTime(lastSlotStart)))

	switch len(breakTimes) {
	case 0:
		// no breaks
	case 1:
		b.WriteString(fmt.Sprintf("\nПерерыв: %s", breakTimes[0]))
	default:
		b.WriteString(fmt.Sprintf("\nПерерывы: %s", strings.Join(breakTimes, " и ")))
	}

	return b.String()
}

func formatTime(t time.Time) string {
	return fmt.Sprintf("%02d:%02d", t.Hour(), t.Minute())
}
