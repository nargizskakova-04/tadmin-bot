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
	token := os.Getenv("TELEGRAM_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("TELEGRAM_TOKEN is required")
	}

	eduURL := os.Getenv("ONEEDU_BASE_URL")
	if eduURL == "" {
		return nil, fmt.Errorf("ONEEDU_BASE_URL is required")
	}
	if !strings.HasPrefix(eduURL, "http://") && !strings.HasPrefix(eduURL, "https://") {
		eduURL = "https://" + eduURL
	}

	eduToken := os.Getenv("PLATFORM_ACCESS_TOKEN")
	if eduToken == "" {
		return nil, fmt.Errorf("PLATFORM_ACCESS_TOKEN is required")
	}

	tmplPath := os.Getenv("TEMPLATES_PATH")
	if tmplPath == "" {
		tmplPath = "messages"
	}

	tz := os.Getenv("TIMEZONE")
	if tz == "" {
		tz = "Asia/Almaty"
	}

	chatIDs, err := parseChatIDs(os.Getenv("CHAT_IDS"))
	if err != nil {
		return nil, fmt.Errorf("CHAT_IDS: %w", err)
	}

	googleCreds := os.Getenv("GOOGLE_CREDENTIALS_FILE")

	return &Config{
		TelegramToken:         token,
		ChatIDs:               chatIDs,
		OneEduBaseURL:         eduURL,
		OneEduAccessToken:     eduToken,
		TemplatesPath:         tmplPath,
		Timezone:              tz,
		GoogleCredentialsFile: googleCreds,
	}, nil
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
