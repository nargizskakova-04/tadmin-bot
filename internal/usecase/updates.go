package usecase

import (
	"admin-bot/internal/domain"
	"context"
	"errors"
	"fmt"
	"strings"
)

type UpdatesUseCase struct {
	eduClient domain.OneEduClient

	// regionEvents maps a lowercased campus name to its pinned event-ID config.
	// A campus with no entry (or a zero-valued config) uses the default
	// path-based lookup for every metric.
	regionEvents map[string]domain.RegionUpdateEventsConfig
}

type AstanaUpdatesUseCase = UpdatesUseCase

func NewUpdatesUseCase(eduClient domain.OneEduClient, regionEvents map[string]domain.RegionUpdateEventsConfig) *UpdatesUseCase {
	if regionEvents == nil {
		regionEvents = map[string]domain.RegionUpdateEventsConfig{}
	}
	return &UpdatesUseCase{
		eduClient:    eduClient,
		regionEvents: regionEvents,
	}
}

func NewAstanaUpdatesUseCase(eduClient domain.OneEduClient) *AstanaUpdatesUseCase {
	return NewUpdatesUseCase(eduClient, nil)
}

// eventsForRegion returns the pinned event-ID config for a campus, or a
// zero-valued config (all metrics path-based) when nothing is configured.
func (u *UpdatesUseCase) eventsForRegion(campus string) domain.RegionUpdateEventsConfig {
	return u.regionEvents[strings.ToLower(strings.TrimSpace(campus))]
}

func (u *UpdatesUseCase) GetAstanaUpdates(ctx context.Context) (domain.AstanaUpdatesInfo, error) {
	astanaUpdate, err := u.eduClient.GetAstanaUpdates(ctx)
	if err != nil {
		return domain.AstanaUpdatesInfo{}, fmt.Errorf("get astana updates: %w", err)
	}
	return domain.AstanaUpdatesInfo{
		Total:     astanaUpdate.Total,
		Succeeded: astanaUpdate.Succeeded,
		Checkin:   astanaUpdate.Checkin,
		Piscinego: astanaUpdate.Piscinego,
	}, nil
}

func (u *UpdatesUseCase) GetRegionUpdates(ctx context.Context) (domain.RegionUpdatesReport, error) {
	campuses, err := u.eduClient.GetCampuses(ctx)
	if err != nil {
		return domain.RegionUpdatesReport{}, fmt.Errorf("get campuses: %w", err)
	}
	if len(campuses) == 0 {
		return domain.RegionUpdatesReport{}, domain.ErrNoCampuses
	}

	report := domain.RegionUpdatesReport{
		Regions: make([]domain.RegionUpdatesInfo, 0, len(campuses)),
	}

	for _, campus := range campuses {
		campus = strings.TrimSpace(campus)
		if campus == "" {
			report.Errors = append(report.Errors, domain.RegionUpdatesError{
				Err: errors.New("empty campus name"),
			})
			continue
		}

		info, err := u.eduClient.GetRegionUpdates(ctx, campus, u.eventsForRegion(campus))
		if err != nil {
			report.Errors = append(report.Errors, domain.RegionUpdatesError{
				Region: campus,
				Err:    err,
			})
			continue
		}
		if info == nil {
			report.Errors = append(report.Errors, domain.RegionUpdatesError{
				Region: campus,
				Err:    errors.New("empty region stats response"),
			})
			continue
		}
		if strings.TrimSpace(info.Region) == "" {
			info.Region = campus
		}

		report.Regions = append(report.Regions, *info)
	}

	return report, nil
}
