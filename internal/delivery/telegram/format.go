package telegram

import (
	"fmt"
	"strings"
	"time"

	"admin-bot/internal/domain"
)

// formatRegionUpdatesMessage renders one region's block, mirroring the Astana
// reference format (dated header + metric lines). Metrics whose pinned event
// failed verification are shown as unavailable instead of a stale number, so a
// single bad event ID never masquerades as real data.
func formatRegionUpdatesMessage(info domain.RegionUpdatesInfo, date string) string {
	region := strings.TrimSpace(info.Region)
	if region == "" {
		region = "unknown"
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "### %s - %s\n", date, escapeHTML(region))
	fmt.Fprintf(&sb, "- %d заявок\n", info.SignedUpWithoutOnboarding)
	fmt.Fprintf(&sb, "- %d прошли игры\n", info.SucceededOnboardingGames)
	writeRegionMetric(&sb, info, domain.EventCheckin, info.CheckinRegistrations, "reg на check-in")
	writeRegionMetric(&sb, info, domain.EventPiscineGo, info.PiscineGoRegistrations, "reg на piscine")
	return sb.String()
}

// writeRegionMetric writes a metric line, or an "unavailable" notice when the
// metric's pinned event was flagged stale (missing / wrong region / ended).
func writeRegionMetric(sb *strings.Builder, info domain.RegionUpdatesInfo, t domain.EventType, count int, label string) {
	if info.IsStale(t) {
		fmt.Fprintf(sb, "- ⚠️ %s: данные неактуальны (ивент недоступен или завершён)\n", label)
		return
	}
	fmt.Fprintf(sb, "- %d %s\n", count, label)
}

// htmlEscaper escapes the characters significant in Telegram's HTML parse mode.
// strings.Replacer runs in a single left-to-right pass, so "&" -> "&amp;" is not
// re-escaped. (go-telegram/bot has no EscapeHTML helper — only EscapeMarkdown.)
var htmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// escapeHTML escapes externally-sourced text before interpolation into an
// HTML-parse-mode message.
func escapeHTML(s string) string { return htmlEscaper.Replace(s) }

// parsePiscineFromCallback extracts the piscine type from callback data.
func parsePiscineFromCallback(data, prefix string) string {
	if !strings.HasPrefix(data, prefix) {
		return ""
	}
	return strings.TrimPrefix(data, prefix)
}

// nextMonday returns the date of the next Monday from the given time, at
// midnight in the input's location. Pass a time already converted to the
// desired location (e.g. time.Now().In(loc)) to avoid timezone drift.
func nextMonday(t time.Time) time.Time {
	daysUntilMonday := (8 - int(t.Weekday())) % 7
	if daysUntilMonday == 0 {
		daysUntilMonday = 7
	}
	monday := t.AddDate(0, 0, daysUntilMonday)
	return time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, t.Location())
}
