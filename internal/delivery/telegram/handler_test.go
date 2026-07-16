package telegram

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"admin-bot/internal/domain"
	"admin-bot/internal/infra/accessstore"
	"admin-bot/internal/usecase"
)

// TestIsAuthorized covers the private-chat / group / super-admin authorization
// matrix. Telegram guarantees chatID == userID for private chats, so an
// approved user is authorized in their DM with no per-chat allowlisting.
func TestIsAuthorized(t *testing.T) {
	const (
		superAdmin  int64 = 999
		approved    int64 = 100
		pending     int64 = 200
		rejected    int64 = 300
		unknown     int64 = 400
		allowedChat int64 = -100
		otherChat   int64 = -500
	)

	store, err := accessstore.New(filepath.Join(t.TempDir(), "access.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	uc := usecase.NewAccessUseCase(store)
	if _, err := uc.Approve(approved); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if _, err := uc.RequestAccess(pending, "", ""); err != nil {
		t.Fatalf("request pending: %v", err)
	}
	if _, err := uc.RequestAccess(rejected, "", ""); err != nil {
		t.Fatalf("request rejected: %v", err)
	}
	if _, err := uc.Reject(rejected); err != nil {
		t.Fatalf("reject: %v", err)
	}

	h := &Handler{
		accessUC:     uc,
		superAdminID: superAdmin,
		authorized:   map[int64]bool{allowedChat: true},
	}

	cases := []struct {
		name           string
		chatID, userID int64
		want           bool
	}{
		{"super_admin_in_group", allowedChat, superAdmin, true},
		{"super_admin_in_dm", superAdmin, superAdmin, true},
		{"super_admin_in_random_group", otherChat, superAdmin, true},
		{"approved_in_own_dm", approved, approved, true},
		{"approved_in_allowed_group", allowedChat, approved, true},
		{"approved_in_other_group", otherChat, approved, false},
		{"pending_in_dm", pending, pending, false},
		{"rejected_in_dm", rejected, rejected, false},
		{"unknown_in_dm", unknown, unknown, false},
		{"unknown_in_allowed_group", allowedChat, unknown, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := h.isAuthorized(tc.chatID, tc.userID); got != tc.want {
				t.Errorf("isAuthorized(%d, %d) = %v, want %v", tc.chatID, tc.userID, got, tc.want)
			}
		})
	}
}

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

func TestFormatRegionUpdatesMessage(t *testing.T) {
	got := formatRegionUpdatesMessage(domain.RegionUpdatesInfo{
		Region:                    "a<b",
		SignedUpWithoutOnboarding: 12,
		SucceededOnboardingGames:  34,
		CheckinRegistrations:      56,
		PiscineGoRegistrations:    78,
		CoreUsers:                 90,
	}, "02.07.2026")

	wantParts := []string{
		"### 02.07.2026 - a&lt;b",
		"- 12 заявок",
		"- 34 прошли игры",
		"- 56 reg на check-in",
		"- 78 reg на piscine",
	}
	for _, part := range wantParts {
		if !strings.Contains(got, part) {
			t.Errorf("formatted message missing %q:\n%s", part, got)
		}
	}
}

// TestFormatRegionUpdatesMessage_StaleEvent verifies a metric backed by a stale
// pinned event is shown as unavailable rather than as a (misleading) count.
func TestFormatRegionUpdatesMessage_StaleEvent(t *testing.T) {
	got := formatRegionUpdatesMessage(domain.RegionUpdatesInfo{
		Region:                 "shymkent",
		CheckinRegistrations:   56,
		PiscineGoRegistrations: 78,
		StaleEvents: []domain.StaleEvent{
			{Type: domain.EventPiscineGo, EventID: 222, Reason: "ended"},
		},
	}, "02.07.2026")

	if !strings.Contains(got, "- 56 reg на check-in") {
		t.Errorf("check-in should still show its count:\n%s", got)
	}
	if strings.Contains(got, "78 reg на piscine") {
		t.Errorf("stale piscine metric must not show its count:\n%s", got)
	}
	if !strings.Contains(got, "⚠️ reg на piscine") {
		t.Errorf("stale piscine metric should be flagged unavailable:\n%s", got)
	}
}
