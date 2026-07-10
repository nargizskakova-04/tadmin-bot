package usecase

import (
	"context"
	"errors"
	"testing"

	"admin-bot/internal/domain"
)

type fakeUpdatesClient struct {
	campuses    []string
	campusesErr error
	stats       map[string]*domain.RegionUpdatesInfo
	statsErr    map[string]error
	statsCalls  []string
	eventsSeen  map[string]domain.RegionUpdateEventsConfig
	events      map[int]*domain.EventMeta
}

func (f *fakeUpdatesClient) RefreshToken(ctx context.Context) error {
	return nil
}

func (f *fakeUpdatesClient) GetCurrentPiscineID(ctx context.Context, piscine domain.PiscineType) (*domain.PiscineInfo, error) {
	return nil, nil
}

func (f *fakeUpdatesClient) GetRaidsByPiscineID(ctx context.Context, piscine domain.PiscineType, piscineEventID int) ([]domain.RaidInfo, error) {
	return nil, nil
}

func (f *fakeUpdatesClient) GetRaidByName(ctx context.Context, name string, startAt string) (*domain.RaidInfo, error) {
	return nil, nil
}

func (f *fakeUpdatesClient) GetAstanaUpdates(ctx context.Context) (*domain.AstanaUpdatesInfo, error) {
	return nil, nil
}

func (f *fakeUpdatesClient) GetCampuses(ctx context.Context) ([]string, error) {
	if f.campusesErr != nil {
		return nil, f.campusesErr
	}
	return f.campuses, nil
}

func (f *fakeUpdatesClient) GetEventByID(ctx context.Context, id int) (*domain.EventMeta, error) {
	return f.events[id], nil
}

func (f *fakeUpdatesClient) GetRegionUpdates(ctx context.Context, campus string, events domain.RegionUpdateEventsConfig) (*domain.RegionUpdatesInfo, error) {
	f.statsCalls = append(f.statsCalls, campus)
	if f.eventsSeen == nil {
		f.eventsSeen = map[string]domain.RegionUpdateEventsConfig{}
	}
	f.eventsSeen[campus] = events
	if err := f.statsErr[campus]; err != nil {
		return nil, err
	}
	return f.stats[campus], nil
}

func TestUpdatesUseCaseGetRegionUpdates_ContinuesAfterRegionError(t *testing.T) {
	client := &fakeUpdatesClient{
		campuses: []string{"shymkent", "semey"},
		stats: map[string]*domain.RegionUpdatesInfo{
			"shymkent": {
				Region:                    "shymkent",
				SignedUpWithoutOnboarding: 12,
				SucceededOnboardingGames:  34,
				CheckinRegistrations:      56,
				PiscineGoRegistrations:    78,
				CoreUsers:                 90,
			},
		},
		statsErr: map[string]error{
			"semey": errors.New("upstream failed"),
		},
	}

	report, err := NewUpdatesUseCase(client, nil).GetRegionUpdates(context.Background())
	if err != nil {
		t.Fatalf("GetRegionUpdates returned fatal error: %v", err)
	}

	if len(report.Regions) != 1 {
		t.Fatalf("regions len = %d, want 1", len(report.Regions))
	}
	if got := report.Regions[0].Region; got != "shymkent" {
		t.Errorf("region = %q, want shymkent", got)
	}
	if len(report.Errors) != 1 {
		t.Fatalf("errors len = %d, want 1", len(report.Errors))
	}
	if got := report.Errors[0].Region; got != "semey" {
		t.Errorf("error region = %q, want semey", got)
	}
	if got := client.statsCalls; len(got) != 2 || got[0] != "shymkent" || got[1] != "semey" {
		t.Errorf("stats calls = %v, want [shymkent semey]", got)
	}
}

func TestUpdatesUseCaseGetRegionUpdates_EmptyCampuses(t *testing.T) {
	_, err := NewUpdatesUseCase(&fakeUpdatesClient{}, nil).GetRegionUpdates(context.Background())
	if !errors.Is(err, domain.ErrNoCampuses) {
		t.Fatalf("error = %v, want ErrNoCampuses", err)
	}
}

// TestUpdatesUseCaseGetRegionUpdates_PassesPinnedEvents verifies the usecase
// looks up each campus's pinned event config (case-insensitively) and forwards
// it to the client, while campuses without config receive a zero-valued config.
func TestUpdatesUseCaseGetRegionUpdates_PassesPinnedEvents(t *testing.T) {
	client := &fakeUpdatesClient{
		campuses: []string{"astanahub", "shymkent"},
		stats: map[string]*domain.RegionUpdatesInfo{
			"astanahub": {Region: "astanahub"},
			"shymkent":  {Region: "shymkent"},
		},
	}
	regionEvents := map[string]domain.RegionUpdateEventsConfig{
		"astanahub": {CheckinEventID: 111, PiscineEventID: 222, ModuleEventID: 333},
	}

	if _, err := NewUpdatesUseCase(client, regionEvents).GetRegionUpdates(context.Background()); err != nil {
		t.Fatalf("GetRegionUpdates returned error: %v", err)
	}

	if got := client.eventsSeen["astanahub"]; got != regionEvents["astanahub"] {
		t.Errorf("astanahub events = %+v, want %+v", got, regionEvents["astanahub"])
	}
	if got := client.eventsSeen["shymkent"]; got != (domain.RegionUpdateEventsConfig{}) {
		t.Errorf("shymkent events = %+v, want zero (path-based fallback)", got)
	}
}
