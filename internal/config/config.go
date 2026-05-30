// Package config loads application configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"admin-bot/internal/domain"
)

// Config holds every configurable value for the bot.
type Config struct {
	// Telegram
	TelegramToken string
	ChatIDs       []int64

	// AdminChatIDs is the authorization allowlist for INCOMING commands and
	// callbacks. Defaults to ChatIDs when ADMIN_CHAT_IDS is unset, so the bot
	// only accepts commands from the same chats it broadcasts to.
	AdminChatIDs []int64

	// 01-edu
	OneEduBaseURL     string
	OneEduAccessToken string

	// Templates
	TemplatesPath string

	// Timezone for cron scheduler (e.g. "Asia/Almaty")
	Timezone string

	// Google Sheets (optional)
	GoogleCredentialsFile string

	// Pre-configured Google Sheets defense tables, indexed by piscine and week.
	SheetIDs  map[domain.PiscineType]map[int]string
	SheetURLs map[domain.PiscineType]map[int]string
}

// spreadsheetIDRe extracts the spreadsheet ID from a Google Sheets URL.
var spreadsheetIDRe = regexp.MustCompile(`spreadsheets/d/([a-zA-Z0-9_-]+)`)

var sheetEnvMap = []struct {
	env     string
	piscine domain.PiscineType
	week    int
}{
	{"SHEET_GO_WEEK1", domain.PiscineGo, 1},
	{"SHEET_GO_WEEK2", domain.PiscineGo, 2},
	{"SHEET_GO_WEEK3", domain.PiscineGo, 3},
	{"SHEET_JS_WEEK1", domain.PiscineJS, 1},
	{"SHEET_JS_WEEK2", domain.PiscineJS, 2},
	{"SHEET_JS_WEEK3", domain.PiscineJS, 3},
	{"SHEET_AI_WEEK1", domain.PiscineAI, 1},
	{"SHEET_AI_WEEK2", domain.PiscineAI, 2},
}

// Load reads configuration from the environment.
// Load reads configuration from the environment.
func Load() (*Config, error) {
	token, err := requireEnv("TELEGRAM_TOKEN")
	if err != nil {
		return nil, err
	}

	eduURL, err := requireEnv("ONEEDU_BASE_URL")
	if err != nil {
		return nil, err
	}
	eduURL = ensureScheme(eduURL)

	// SECURITY: refuse cleartext HTTP for the upstream that carries the access
	// token and all GraphQL traffic, unless an explicit opt-out is set (local dev).
	if strings.HasPrefix(eduURL, "http://") && os.Getenv("ONEEDU_ALLOW_INSECURE") != "1" {
		return nil, fmt.Errorf("ONEEDU_BASE_URL must use https:// (set ONEEDU_ALLOW_INSECURE=1 to override for local dev)")
	}

	eduToken, err := requireEnv("PLATFORM_ACCESS_TOKEN")
	if err != nil {
		return nil, err
	}

	chatIDs, err := parseChatIDs(os.Getenv("CHAT_IDS"))
	if err != nil {
		return nil, fmt.Errorf("CHAT_IDS: %w", err)
	}
	// CHAT_IDS is required: with no chats the scheduler broadcasts to nobody and
	// (since AdminChatIDs falls back to it) every command is rejected — a deploy
	// that looks healthy but is completely inert. Fail loudly at startup instead.
	if len(chatIDs) == 0 {
		return nil, fmt.Errorf("CHAT_IDS is required: provide a comma-separated list of chat IDs")
	}

	// Authorization allowlist for incoming commands. Falls back to CHAT_IDS.
	adminIDs, err := parseChatIDs(os.Getenv("ADMIN_CHAT_IDS"))
	if err != nil {
		return nil, fmt.Errorf("ADMIN_CHAT_IDS: %w", err)
	}
	if len(adminIDs) == 0 {
		adminIDs = chatIDs
	}

	sheetIDs, sheetURLs := loadSheetMaps()

	return &Config{
		TelegramToken:         token,
		ChatIDs:               chatIDs,
		AdminChatIDs:          adminIDs,
		OneEduBaseURL:         eduURL,
		OneEduAccessToken:     eduToken,
		TemplatesPath:         envOr("TEMPLATES_PATH", "messages"),
		Timezone:              envOr("TIMEZONE", "Asia/Almaty"),
		GoogleCredentialsFile: os.Getenv("GOOGLE_CREDENTIALS_FILE"),
		SheetIDs:              sheetIDs,
		SheetURLs:             sheetURLs,
	}, nil
}

func loadSheetMaps() (ids map[domain.PiscineType]map[int]string, urls map[domain.PiscineType]map[int]string) {
	ids = make(map[domain.PiscineType]map[int]string)
	urls = make(map[domain.PiscineType]map[int]string)

	for _, e := range sheetEnvMap {
		raw := strings.TrimSpace(os.Getenv(e.env))
		if raw == "" {
			continue
		}
		m := spreadsheetIDRe.FindStringSubmatch(raw)
		if len(m) < 2 || m[1] == "" {
			continue
		}
		spreadsheetID := m[1]

		if _, ok := ids[e.piscine]; !ok {
			ids[e.piscine] = make(map[int]string)
		}
		if _, ok := urls[e.piscine]; !ok {
			urls[e.piscine] = make(map[int]string)
		}
		ids[e.piscine][e.week] = spreadsheetID
		urls[e.piscine][e.week] = raw
	}
	return ids, urls
}

func requireEnv(name string) (string, error) {
	v := os.Getenv(name)
	if v == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return v, nil
}

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func ensureScheme(url string) string {
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		return url
	}
	return "https://" + url
}

func parseChatIDs(raw string) ([]int64, error) {
	if raw == "" {
		return nil, nil
	}
	var ids []int64
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid chat ID %q: %w", s, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}
