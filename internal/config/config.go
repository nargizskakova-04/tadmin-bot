// Package config loads application configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds every configurable value for the bot.
type Config struct {
	// Telegram
	TelegramToken string
	ChatIDs       []int64

	// 01-edu
	OneEduBaseURL     string
	OneEduAccessToken string

	// Templates
	TemplatesPath string

	// Timezone for cron scheduler (e.g. "Asia/Almaty")
	Timezone string

	// Google Sheets (optional)
	GoogleCredentialsFile string
}

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

	eduToken, err := requireEnv("PLATFORM_ACCESS_TOKEN")
	if err != nil {
		return nil, err
	}

	chatIDs, err := parseChatIDs(os.Getenv("CHAT_IDS"))
	if err != nil {
		return nil, fmt.Errorf("CHAT_IDS: %w", err)
	}

	return &Config{
		TelegramToken:         token,
		ChatIDs:               chatIDs,
		OneEduBaseURL:         eduURL,
		OneEduAccessToken:     eduToken,
		TemplatesPath:         envOr("TEMPLATES_PATH", "messages"),
		Timezone:              envOr("TIMEZONE", "Asia/Almaty"),
		GoogleCredentialsFile: os.Getenv("GOOGLE_CREDENTIALS_FILE"),
	}, nil
}

// requireEnv reads an environment variable and errors if it's empty.
func requireEnv(name string) (string, error) {
	v := os.Getenv(name)
	if v == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return v, nil
}

// envOr reads an environment variable and returns def when it's empty.
func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// ensureScheme prepends "https://" if the URL has no http(s) scheme.
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
