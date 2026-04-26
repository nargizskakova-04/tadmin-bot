package usecase

import (
	"context"
	"fmt"
	"time"

	"admin-bot/internal/domain"
	"admin-bot/internal/usecase/strategy"
)

// RaidUseCase orchestrates fetching data and building announcements.
type RaidUseCase struct {
	eduClient  domain.OneEduClient
	templates  domain.TemplateRenderer
	strategies map[domain.PiscineType]strategy.PiscineStrategy
}

// NewRaidUseCase constructs a RaidUseCase with the provided dependencies.
func NewRaidUseCase(
	eduClient domain.OneEduClient,
	templates domain.TemplateRenderer,
	strategies []strategy.PiscineStrategy,
) *RaidUseCase {
	m := make(map[domain.PiscineType]strategy.PiscineStrategy, len(strategies))
	for _, s := range strategies {
		m[s.Type()] = s
	}
	return &RaidUseCase{
		eduClient:  eduClient,
		templates:  templates,
		strategies: m,
	}
}

// CurrentWeekInfo holds the result of detecting the current week.
type CurrentWeekInfo struct {
	PiscineInfo *domain.PiscineInfo
	WeekNumber  int
	ActiveRaid  *domain.RaidInfo // nil on final week (no raid)
}

// DetectCurrentWeek determines which week it is for a given piscine.
// It fetches the active piscine, then its raids, and finds which raid
// is currently active (startAt <= now <= endAt).
func (uc *RaidUseCase) DetectCurrentWeek(ctx context.Context, piscine domain.PiscineType) (*CurrentWeekInfo, error) {
	// 1. Get active piscine event.
	piscineInfo, err := uc.eduClient.GetCurrentPiscineID(ctx, piscine)
	if err != nil {
		return nil, fmt.Errorf("get piscine ID: %w", err)
	}
	if piscineInfo == nil {
		return nil, fmt.Errorf("no active piscine found for %s", piscine)
	}

	// 2. Get all raids for this piscine.
	raids, err := uc.eduClient.GetRaidsByPiscineID(ctx, piscine, piscineInfo.ID)
	if err != nil {
		return nil, fmt.Errorf("get raids: %w", err)
	}

	// 3. Find the active raid (startAt <= now <= endAt).
	now := time.Now()
	for _, raid := range raids {
		if !raid.StartDate.After(now) && !raid.EndDate.Before(now) {
			return &CurrentWeekInfo{
				PiscineInfo: piscineInfo,
				WeekNumber:  raid.WeekNumber,
				ActiveRaid:  &raid,
			}, nil
		}
	}

	// 4. No active raid — could be final exam week.
	//    Determine week by checking how many raids have already ended.
	endedCount := 0
	for _, raid := range raids {
		if raid.EndDate.Before(now) {
			endedCount++
		}
	}
	// If all raids have ended, it's the final week.
	totalRaidWeeks := domain.TotalWeeks(piscine) - 1 // final week has no raid
	if endedCount >= totalRaidWeeks {
		return &CurrentWeekInfo{
			PiscineInfo: piscineInfo,
			WeekNumber:  domain.TotalWeeks(piscine),
			ActiveRaid:  nil,
		}, nil
	}

	// 5. Between raids — find the next upcoming raid to determine week.
	var nextRaid *domain.RaidInfo
	for i := range raids {
		if raids[i].StartDate.After(now) {
			if nextRaid == nil || raids[i].StartDate.Before(nextRaid.StartDate) {
				nextRaid = &raids[i]
			}
		}
	}
	if nextRaid != nil {
		return &CurrentWeekInfo{
			PiscineInfo: piscineInfo,
			WeekNumber:  nextRaid.WeekNumber,
			ActiveRaid:  nextRaid,
		}, nil
	}

	return nil, fmt.Errorf("could not determine current week for %s", piscine)
}

// BuildMessage builds a message of the given type for the given piscine.
// Returns the rendered text and an error.
func (uc *RaidUseCase) BuildMessage(
	ctx context.Context,
	piscine domain.PiscineType,
	msgType domain.MessageType,
	extra map[string]string,
) (string, error) {
	strat, ok := uc.strategies[piscine]
	if !ok {
		return "", fmt.Errorf("%w: %s", domain.ErrPiscineNotFound, piscine)
	}

	// Detect current week.
	weekInfo, err := uc.DetectCurrentWeek(ctx, piscine)
	if err != nil {
		return "", fmt.Errorf("detect week: %w", err)
	}

	// Check if this message type is applicable for this week.
	if !strat.SupportsMessage(msgType, weekInfo.WeekNumber) {
		return "", fmt.Errorf("message type %s not supported for %s week %d",
			msgType, piscine, weekInfo.WeekNumber)
	}

	// Build template vars.
	raidInfo := weekInfo.ActiveRaid
	if raidInfo == nil {
		// Final week — create a stub RaidInfo for template rendering.
		raidInfo = &domain.RaidInfo{
			Piscine:    piscine,
			WeekNumber: weekInfo.WeekNumber,
		}
	}

	vars := strat.TemplateVars(msgType, raidInfo, extra)
	templateKey := strat.TemplateKey(msgType)

	text, err := uc.templates.Render(templateKey, vars)
	if err != nil {
		return "", fmt.Errorf("render template %q: %w", templateKey, err)
	}

	return text, nil
}

// BuildDefenseReminder builds the admin reminder about creating the defense table.
// Returns the rendered text and the calculated schedule info.
func (uc *RaidUseCase) BuildDefenseReminder(
	ctx context.Context,
	piscine domain.PiscineType,
) (string, *DefenseSchedule, error) {
	weekInfo, err := uc.DetectCurrentWeek(ctx, piscine)
	if err != nil {
		return "", nil, fmt.Errorf("detect week: %w", err)
	}

	if weekInfo.ActiveRaid == nil {
		return "", nil, fmt.Errorf("no active raid for defense reminder")
	}

	raid := weekInfo.ActiveRaid
	schedule := CalculateDefenseSchedule(raid.TeamsCount)

	strat, ok := uc.strategies[piscine]
	if !ok {
		return "", nil, fmt.Errorf("%w: %s", domain.ErrPiscineNotFound, piscine)
	}

	extra := map[string]string{
		"ROWS":                 fmt.Sprintf("%d", schedule.Rows),
		"TOTAL_SLOTS":          fmt.Sprintf("%d", schedule.TotalSlots),
		"RECOMMENDED_SCHEDULE": schedule.RecommendedSchedule,
	}

	vars := strat.TemplateVars(domain.MsgDefenseReminder, raid, extra)
	templateKey := strat.TemplateKey(domain.MsgDefenseReminder)

	text, err := uc.templates.Render(templateKey, vars)
	if err != nil {
		return "", nil, fmt.Errorf("render template: %w", err)
	}

	return text, &schedule, nil
}

// GetStrategy returns the strategy for a piscine type (used by handlers).
func (uc *RaidUseCase) GetStrategy(piscine domain.PiscineType) (strategy.PiscineStrategy, bool) {
	s, ok := uc.strategies[piscine]
	return s, ok
}
