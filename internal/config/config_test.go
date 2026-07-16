package config

import (
	"reflect"
	"strings"
	"testing"

	"admin-bot/internal/domain"
)

// setEnv sets envs for the duration of the test and restores them on cleanup.
// nil value means unset.
func setEnv(t *testing.T, kv map[string]*string) {
	t.Helper()
	for k, v := range kv {
		if v == nil {
			t.Setenv(k, "") // t.Setenv only sets; unset done via "" because we control reads
			continue
		}
		t.Setenv(k, *v)
	}
}

func strp(s string) *string { return &s }

// requiredEnvs returns the minimum env set for Load() to succeed.
func requiredEnvs() map[string]*string {
	return map[string]*string{
		"TELEGRAM_TOKEN":          strp("tok"),
		"ONEEDU_BASE_URL":         strp("https://learn.example.com"),
		"PLATFORM_ACCESS_TOKEN":   strp("ptok"),
		"CHAT_IDS":                strp("-100"), // now required
		"SUPER_ADMIN_USER_ID":     strp("555"),  // now required
		"TEMPLATES_PATH":          strp(""),
		"TIMEZONE":                strp(""),
		"GOOGLE_CREDENTIALS_FILE": strp(""),
	}
}

func TestLoad_Success_AllRequiredFieldsSet(t *testing.T) {
	envs := requiredEnvs()
	envs["CHAT_IDS"] = strp("-100123, 456 ,789")
	envs["TEMPLATES_PATH"] = strp("/etc/tmpl")
	envs["TIMEZONE"] = strp("Europe/Berlin")
	envs["GOOGLE_CREDENTIALS_FILE"] = strp("/creds.json")
	setEnv(t, envs)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.TelegramToken != "tok" {
		t.Errorf("TelegramToken=%q", cfg.TelegramToken)
	}
	if cfg.OneEduBaseURL != "https://learn.example.com" {
		t.Errorf("OneEduBaseURL=%q", cfg.OneEduBaseURL)
	}
	if cfg.OneEduAccessToken != "ptok" {
		t.Errorf("OneEduAccessToken=%q", cfg.OneEduAccessToken)
	}
	if cfg.TemplatesPath != "/etc/tmpl" {
		t.Errorf("TemplatesPath=%q", cfg.TemplatesPath)
	}
	if cfg.Timezone != "Europe/Berlin" {
		t.Errorf("Timezone=%q", cfg.Timezone)
	}
	if cfg.GoogleCredentialsFile != "/creds.json" {
		t.Errorf("GoogleCredentialsFile=%q", cfg.GoogleCredentialsFile)
	}
	if !reflect.DeepEqual(cfg.ChatIDs, []int64{-100123, 456, 789}) {
		t.Errorf("ChatIDs=%v", cfg.ChatIDs)
	}
}

func TestLoad_Defaults(t *testing.T) {
	setEnv(t, requiredEnvs())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.TemplatesPath != "messages" {
		t.Errorf("default TemplatesPath=%q, want %q", cfg.TemplatesPath, "messages")
	}
	if cfg.Timezone != "Asia/Almaty" {
		t.Errorf("default Timezone=%q, want %q", cfg.Timezone, "Asia/Almaty")
	}
	if cfg.GoogleCredentialsFile != "" {
		t.Errorf("default GoogleCredentialsFile=%q, want empty", cfg.GoogleCredentialsFile)
	}
}

func TestLoad_RequiredFields(t *testing.T) {
	cases := []string{"TELEGRAM_TOKEN", "ONEEDU_BASE_URL", "PLATFORM_ACCESS_TOKEN"}
	for _, missing := range cases {
		missing := missing
		t.Run("missing/"+missing, func(t *testing.T) {
			envs := requiredEnvs()
			envs[missing] = strp("")
			setEnv(t, envs)

			_, err := Load()
			if err == nil {
				t.Fatal("expected error when required env missing, got nil")
			}
			if !strings.Contains(err.Error(), missing) {
				t.Errorf("error %v should mention %q", err, missing)
			}
		})
	}
}

// TestLoad_RequiresChatIDs verifies the bot refuses to start with no chats
// configured, rather than coming up inert (no broadcast targets, all commands
// rejected via the empty admin allowlist).
func TestLoad_RequiresChatIDs(t *testing.T) {
	envs := requiredEnvs()
	envs["CHAT_IDS"] = strp("")
	setEnv(t, envs)

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when CHAT_IDS is empty")
	}
	if !strings.Contains(err.Error(), "CHAT_IDS") {
		t.Errorf("error should mention CHAT_IDS, got: %v", err)
	}
}

func TestLoad_AddsHttpsSchemeWhenMissing(t *testing.T) {
	envs := requiredEnvs()
	envs["ONEEDU_BASE_URL"] = strp("learn.example.com")
	setEnv(t, envs)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OneEduBaseURL != "https://learn.example.com" {
		t.Errorf("OneEduBaseURL=%q, want https-prefixed", cfg.OneEduBaseURL)
	}
}

func TestLoad_KeepsExistingScheme(t *testing.T) {
	cases := []struct {
		url      string
		insecure bool // http:// requires the explicit ONEEDU_ALLOW_INSECURE opt-out
	}{
		{"http://insecure.example.com", true},
		{"https://secure.example.com", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.url, func(t *testing.T) {
			envs := requiredEnvs()
			envs["ONEEDU_BASE_URL"] = strp(tc.url)
			if tc.insecure {
				envs["ONEEDU_ALLOW_INSECURE"] = strp("1")
			}
			setEnv(t, envs)
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.OneEduBaseURL != tc.url {
				t.Errorf("URL=%q, want %q (unchanged)", cfg.OneEduBaseURL, tc.url)
			}
		})
	}
}

func TestLoad_BadChatID(t *testing.T) {
	envs := requiredEnvs()
	envs["CHAT_IDS"] = strp("123,not-a-number,456")
	setEnv(t, envs)
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for non-numeric chat ID")
	}
	if !strings.Contains(err.Error(), "CHAT_IDS") {
		t.Errorf("error should mention CHAT_IDS, got: %v", err)
	}
}

func TestParseChatIDs_EmptyAndWhitespace(t *testing.T) {
	cases := []struct {
		in   string
		want []int64
	}{
		{"", nil},
		{"   ", nil},
		{",,,", nil},
		{"  1  , 2 , 3  ", []int64{1, 2, 3}},
		{"-100123456789", []int64{-100123456789}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseChatIDs(tc.in)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseChatIDs(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestEnvOr(t *testing.T) {
	t.Setenv("X_PRESENT", "value")
	t.Setenv("X_EMPTY", "")
	if got := envOr("X_PRESENT", "def"); got != "value" {
		t.Errorf("envOr present = %q, want %q", got, "value")
	}
	if got := envOr("X_EMPTY", "def"); got != "def" {
		t.Errorf("envOr empty = %q, want %q", got, "def")
	}
	if got := envOr("X_UNSET_LIKELY_NOT_EXISTING", "def"); got != "def" {
		t.Errorf("envOr unset = %q, want %q", got, "def")
	}
}

func TestLoadRegionEvents_MergesPerRegionOverrides(t *testing.T) {
	// A region with all three metrics pinned, and another with only one — the
	// unset metrics of the second must stay zero (path-based fallback).
	t.Setenv("REGION_ASTANAHUB_CHECKIN_EVENT_ID", "111")
	t.Setenv("REGION_ASTANAHUB_PISCINE_EVENT_ID", "222")
	t.Setenv("REGION_ASTANAHUB_MODULE_EVENT_ID", "333")
	t.Setenv("REGION_SHYMKENT_PISCINE_EVENT_ID", "444")

	got, err := loadRegionEvents()
	if err != nil {
		t.Fatalf("loadRegionEvents: %v", err)
	}

	if got["astanahub"] != (domain.RegionUpdateEventsConfig{CheckinEventID: 111, PiscineEventID: 222, ModuleEventID: 333}) {
		t.Errorf("astanahub = %+v", got["astanahub"])
	}
	if got["shymkent"] != (domain.RegionUpdateEventsConfig{PiscineEventID: 444}) {
		t.Errorf("shymkent = %+v, want only PiscineEventID pinned", got["shymkent"])
	}
}

func TestLoadRegionEvents_RejectsNonNumeric(t *testing.T) {
	t.Setenv("REGION_ASTANAHUB_CHECKIN_EVENT_ID", "not-a-number")
	if _, err := loadRegionEvents(); err == nil {
		t.Fatal("expected error for non-numeric event ID")
	}
}

func TestLoadRegionEvents_RejectsNonPositive(t *testing.T) {
	t.Setenv("REGION_ASTANAHUB_CHECKIN_EVENT_ID", "0")
	if _, err := loadRegionEvents(); err == nil {
		t.Fatal("expected error for non-positive event ID")
	}
}

func TestEnsureScheme(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"example.com", "https://example.com"},
		{"https://example.com", "https://example.com"},
		{"http://example.com", "http://example.com"},
		{"", "https://"},
	}
	for _, tc := range cases {
		if got := ensureScheme(tc.in); got != tc.want {
			t.Errorf("ensureScheme(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
