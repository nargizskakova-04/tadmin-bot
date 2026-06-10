package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"admin-bot/internal/domain"
)

// --- fake OneEduClient ---

type fakeEduClient struct {
	piscine       *domain.PiscineInfo
	piscineErr    error
	raids         []domain.RaidInfo
	raidsErr      error
	raidByName    *domain.RaidInfo
	raidByNameErr error
}

func (f *fakeEduClient) RefreshToken(_ context.Context) error { return nil }
func (f *fakeEduClient) GetCurrentPiscineID(_ context.Context, _ domain.PiscineType) (*domain.PiscineInfo, error) {
	return f.piscine, f.piscineErr
}
func (f *fakeEduClient) GetRaidsByPiscineID(_ context.Context, _ domain.PiscineType, _ int) ([]domain.RaidInfo, error) {
	return f.raids, f.raidsErr
}
func (f *fakeEduClient) GetRaidByName(_ context.Context, _, _ string) (*domain.RaidInfo, error) {
	return f.raidByName, f.raidByNameErr
}
func (f *fakeEduClient) GetUserByLogin(_ context.Context, _ string) (*domain.FaceUser, error) {
	return nil, nil
}
func (f *fakeEduClient) DownloadPhoto(_ context.Context, _ string) ([]byte, string, error) {
	return nil, "", nil
}

// --- helper builders ---

func mkRaid(name string, week int, start, end time.Time) domain.RaidInfo {
	return domain.RaidInfo{
		RaidName:   name,
		WeekNumber: week,
		StartDate:  start,
		EndDate:    end,
		TeamsCount: 9,
	}
}

func newUC(client domain.OneEduClient) *RaidUseCase {
	return &RaidUseCase{
		eduClient:  client,
		strategies: nil,
	}
}

// --- DetectCurrentWeek tests ---

func TestDetectCurrentWeek_ActiveRaid(t *testing.T) {
	now := time.Now()
	week1Start := now.Add(-3 * 24 * time.Hour)
	week1End := now.Add(2 * 24 * time.Hour)

	fc := &fakeEduClient{
		piscine: &domain.PiscineInfo{ID: 1},
		raids: []domain.RaidInfo{
			mkRaid("quad", 1, week1Start, week1End),
		},
	}
	uc := newUC(fc)

	info, err := uc.DetectCurrentWeek(context.Background(), domain.PiscineGo)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if info.ActiveRaid == nil || info.ActiveRaid.RaidName != "quad" {
		t.Errorf("expected active raid 'quad', got %+v", info.ActiveRaid)
	}
	if info.WeekNumber != 1 {
		t.Errorf("WeekNumber=%d, want 1", info.WeekNumber)
	}
}

func TestDetectCurrentWeek_FinalWeek_AllRaidsEnded(t *testing.T) {
	now := time.Now()
	past := now.Add(-7 * 24 * time.Hour)
	earlier := now.Add(-21 * 24 * time.Hour)

	fc := &fakeEduClient{
		piscine: &domain.PiscineInfo{ID: 1},
		raids: []domain.RaidInfo{
			mkRaid("quad", 1, earlier, earlier.Add(2*24*time.Hour)),
			mkRaid("sudoku", 2, past.Add(-7*24*time.Hour), past.Add(-5*24*time.Hour)),
			mkRaid("quadchecker", 3, past.Add(-2*24*time.Hour), past),
		},
	}
	uc := newUC(fc)

	info, err := uc.DetectCurrentWeek(context.Background(), domain.PiscineGo)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if info.ActiveRaid != nil {
		t.Errorf("expected nil ActiveRaid on final week, got %+v", info.ActiveRaid)
	}
	if info.WeekNumber != domain.TotalWeeks(domain.PiscineGo) {
		t.Errorf("WeekNumber=%d, want %d (final)", info.WeekNumber, domain.TotalWeeks(domain.PiscineGo))
	}
}

func TestDetectCurrentWeek_BetweenRaids_PicksEarliestUpcoming(t *testing.T) {
	now := time.Now()

	earlyEnded := mkRaid("quad", 1, now.Add(-10*24*time.Hour), now.Add(-3*24*time.Hour))
	soon := mkRaid("sudoku", 2, now.Add(2*24*time.Hour), now.Add(4*24*time.Hour))
	later := mkRaid("quadchecker", 3, now.Add(10*24*time.Hour), now.Add(12*24*time.Hour))

	fc := &fakeEduClient{
		piscine: &domain.PiscineInfo{ID: 1},
		// Intentionally out of order to make sure we pick by date, not slice position.
		raids: []domain.RaidInfo{later, earlyEnded, soon},
	}
	uc := newUC(fc)

	info, err := uc.DetectCurrentWeek(context.Background(), domain.PiscineGo)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if info.ActiveRaid == nil || info.ActiveRaid.RaidName != "sudoku" {
		t.Errorf("expected next raid 'sudoku', got %+v", info.ActiveRaid)
	}
	if info.WeekNumber != 2 {
		t.Errorf("WeekNumber=%d, want 2", info.WeekNumber)
	}
}

func TestDetectCurrentWeek_PiscineFetchError(t *testing.T) {
	fc := &fakeEduClient{piscineErr: errors.New("boom")}
	uc := newUC(fc)
	if _, err := uc.DetectCurrentWeek(context.Background(), domain.PiscineGo); err == nil {
		t.Fatal("expected error from piscine fetch, got nil")
	}
}

func TestDetectCurrentWeek_PiscineNotFound(t *testing.T) {
	fc := &fakeEduClient{piscine: nil}
	uc := newUC(fc)
	_, err := uc.DetectCurrentWeek(context.Background(), domain.PiscineGo)
	if err == nil {
		t.Fatal("expected error when no piscine returned")
	}
}

func TestDetectCurrentWeek_RaidsFetchError(t *testing.T) {
	fc := &fakeEduClient{
		piscine:  &domain.PiscineInfo{ID: 1},
		raidsErr: errors.New("boom"),
	}
	uc := newUC(fc)
	if _, err := uc.DetectCurrentWeek(context.Background(), domain.PiscineGo); err == nil {
		t.Fatal("expected error from raids fetch, got nil")
	}
}

// --- helper-function unit tests ---

func TestFindActiveRaid(t *testing.T) {
	now := time.Now()
	r1 := mkRaid("a", 1, now.Add(-2*time.Hour), now.Add(-time.Hour)) // ended
	r2 := mkRaid("b", 2, now.Add(-time.Hour), now.Add(time.Hour))    // active
	r3 := mkRaid("c", 3, now.Add(time.Hour), now.Add(2*time.Hour))   // future

	got := findActiveRaid([]domain.RaidInfo{r1, r2, r3}, now)
	if got == nil || got.RaidName != "b" {
		t.Errorf("findActiveRaid = %+v, want raid 'b'", got)
	}

	if got := findActiveRaid([]domain.RaidInfo{r1, r3}, now); got != nil {
		t.Errorf("findActiveRaid with no active should be nil, got %+v", got)
	}
}

func TestCountEndedRaids(t *testing.T) {
	now := time.Now()
	raids := []domain.RaidInfo{
		mkRaid("a", 1, now.Add(-3*time.Hour), now.Add(-2*time.Hour)), // ended
		mkRaid("b", 2, now.Add(-time.Hour), now.Add(time.Hour)),      // active (not ended)
		mkRaid("c", 3, now.Add(-5*time.Hour), now.Add(-4*time.Hour)), // ended
	}
	if got, want := countEndedRaids(raids, now), 2; got != want {
		t.Errorf("countEndedRaids = %d, want %d", got, want)
	}
}

func TestFindNextUpcomingRaid_PicksEarliestFuture(t *testing.T) {
	now := time.Now()
	past := mkRaid("p", 1, now.Add(-2*time.Hour), now.Add(-time.Hour))
	soon := mkRaid("s", 2, now.Add(1*time.Hour), now.Add(2*time.Hour))
	later := mkRaid("l", 3, now.Add(5*time.Hour), now.Add(6*time.Hour))

	got := findNextUpcomingRaid([]domain.RaidInfo{later, past, soon}, now)
	if got == nil || got.RaidName != "s" {
		t.Errorf("findNextUpcomingRaid = %+v, want raid 's'", got)
	}
}

func TestFindNextUpcomingRaid_NoneFuture(t *testing.T) {
	now := time.Now()
	r := mkRaid("p", 1, now.Add(-2*time.Hour), now.Add(-time.Hour))

	if got := findNextUpcomingRaid([]domain.RaidInfo{r}, now); got != nil {
		t.Errorf("findNextUpcomingRaid with no future raid should be nil, got %+v", got)
	}
}
