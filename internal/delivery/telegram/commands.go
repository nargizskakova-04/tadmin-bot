package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"admin-bot/internal/domain"
	"admin-bot/internal/infra/sheets"
	"admin-bot/internal/usecase"
)

func (h *Handler) HandleHelp(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID, ok := h.guard(ctx, update)
	if !ok {
		return
	}

	text := "📋 <b>Команды:</b>\n\n" +
		"/help — показать это сообщение\n" +
		"/raidgo — информация о рейде Piscine Go\n" +
		"/raidjs — информация о рейде Piscine JS\n" +
		"/raidai — информация о рейде Piscine AI\n" +
		"/week — текущая неделя для всех Piscine\n" +
		"/create_tables — обновить Google Sheets таблицы защит для всех активных рейдов\n" +
		"/get_region_updates — статистика обновлений по всем регионам\n" +
		"/get_astana_updates — статистика обновлений Astana\n"

	if err := h.adapter.SendMessage(ctx, chatID, text); err != nil {
		h.logger.Error("send help failed", "err", err)
	}
}

func (h *Handler) HandleRaidGo(ctx context.Context, b *bot.Bot, update *models.Update) {
	h.handleRaidInfo(ctx, update, domain.PiscineGo)
}

func (h *Handler) HandleRaidJS(ctx context.Context, b *bot.Bot, update *models.Update) {
	h.handleRaidInfo(ctx, update, domain.PiscineJS)
}

func (h *Handler) HandleRaidAI(ctx context.Context, b *bot.Bot, update *models.Update) {
	h.handleRaidInfo(ctx, update, domain.PiscineAI)
}

func (h *Handler) HandleWeek(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID, ok := h.guard(ctx, update)
	if !ok {
		return
	}

	var sb strings.Builder
	for _, p := range domain.AllPiscines() {
		weekInfo, err := h.raidUC.DetectCurrentWeek(ctx, p)
		if err != nil {
			// Do NOT echo err.Error() into the chat: an upstream error can carry
			// sensitive fragments, and chat messages persist on Telegram's
			// servers. Log the detail server-side, show a generic line here.
			h.logger.Error("detect week failed", "piscine", p, "err", err)
			fmt.Fprintf(&sb, "❌ %s: не удалось получить данные\n", escapeHTML(string(p)))
			continue
		}

		raidName := "—"
		if weekInfo.ActiveRaid != nil {
			raidName = weekInfo.ActiveRaid.RaidName
		}

		weekLabel := fmt.Sprintf("Неделя %d", weekInfo.WeekNumber)
		if domain.IsFinalWeek(p, weekInfo.WeekNumber) {
			weekLabel += " (Final Exam)"
		}

		fmt.Fprintf(&sb, "📌 <b>%s</b>: %s | Рейд: %s\n",
			escapeHTML(string(p)), weekLabel, escapeHTML(raidName))
	}

	if err := h.adapter.SendMessage(ctx, chatID, sb.String()); err != nil {
		h.logger.Error("send week info failed", "err", err)
	}
}

// HandleTables handles the /create_tables command.
func (h *Handler) HandleTables(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID, ok := h.guard(ctx, update)
	if !ok {
		return
	}

	if h.sheets == nil {
		_ = h.adapter.SendMessage(ctx, chatID, msgSheetsNotConfigured)
		return
	}

	// Defense is on Monday — computed in the configured timezone so the date
	// matches what admins see locally regardless of the container clock.
	defenseDate := nextMonday(h.now())

	var lines []string
	updatedCount := 0

	for _, piscine := range domain.AllPiscines() {
		weekInfo, err := h.raidUC.DetectCurrentWeek(ctx, piscine)
		if err != nil {
			h.logger.Warn("detect week failed", "piscine", piscine, "err", err)
			lines = append(lines, fmt.Sprintf("❌ Ошибка при обновлении таблицы (%s)", piscine))
			continue
		}

		if weekInfo.ActiveRaid == nil {
			h.logger.Info("skip: no active raid", "piscine", piscine)
			continue
		}

		raid := weekInfo.ActiveRaid

		spreadsheetID := h.lookupSheetID(piscine, weekInfo.WeekNumber)
		if spreadsheetID == "" {
			h.logger.Warn("no sheet configured for week", "piscine", piscine, "week", weekInfo.WeekNumber)
			lines = append(lines, fmt.Sprintf("⚠️ %s — таблица для недели %d не настроена", piscine, weekInfo.WeekNumber))
			continue
		}

		url, err := h.updateTableForActiveRaid(ctx, spreadsheetID, raid, defenseDate)
		if err != nil {
			h.logger.Error("update defense table failed", "piscine", piscine, "raid", raid.RaidName, "err", err)
			lines = append(lines, fmt.Sprintf("❌ Ошибка при обновлении таблицы (%s)", piscine))
			continue
		}

		updatedCount++
		lines = append(lines, fmt.Sprintf("✅ Таблица обновлена (%s — %s): %s",
			escapeHTML(string(piscine)), escapeHTML(raid.RaidName), url))
	}

	resp := "ℹ️ На этой неделе нет активных рейдов — обновлять нечего."
	if len(lines) > 0 {
		resp = strings.Join(lines, "\n")
	}

	if err := h.adapter.SendMessage(ctx, chatID, resp); err != nil {
		h.logger.Error("send create_tables result failed", "err", err)
	}

	h.logger.Info("create_tables finished", "updated", updatedCount, "total_lines", len(lines))
}

func (h *Handler) HandleAstanaUpdates(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID, ok := h.guard(ctx, update)
	if !ok {
		return
	}

	var sb strings.Builder

	info, err := h.updatesUC.GetAstanaUpdates(ctx)
	if err != nil {
		h.logger.Error("get astana updates failed", "err", err)
		fmt.Fprintf(&sb, "❌ Не удалось получить данные об обновлениях Astana\n")
	} else {
		date := time.Now().In(h.loc).Format("02.01.2006")

		fmt.Fprintf(&sb, "### %s - Астана\n", date)
		fmt.Fprintf(&sb, "- %d тотал заявок\n", info.Total)
		fmt.Fprintf(&sb, "- %d тотал прошли игры\n", info.Succeeded)
		fmt.Fprintf(&sb, "- %d reg на check-in\n", info.Checkin)
		fmt.Fprintf(&sb, "- %d reg на piscine\n", info.Piscinego)
	}

	if err := h.adapter.SendMessage(ctx, chatID, sb.String()); err != nil {
		h.logger.Error("send astana updates failed", "err", err)
	}
}

func (h *Handler) HandleRegionUpdates(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID, ok := h.guard(ctx, update)
	if !ok {
		return
	}

	report, err := h.updatesUC.GetRegionUpdates(ctx)
	if err != nil {
		h.logger.Error("get region updates failed", "err", err)
		text := "❌ Не удалось получить список регионов"
		if errors.Is(err, domain.ErrNoCampuses) {
			text = "⚠️ Список регионов пуст"
		}
		if sendErr := h.adapter.SendMessage(ctx, chatID, text); sendErr != nil {
			h.logger.Error("send region updates error failed", "err", sendErr)
		}
		return
	}

	for _, regionErr := range report.Errors {
		h.logger.Error("get region stats failed", "region", regionErr.Region, "err", regionErr.Err)
	}

	if len(report.Regions) == 0 {
		text := "❌ Не удалось получить статистику ни для одного региона"
		if err := h.adapter.SendMessage(ctx, chatID, text); err != nil {
			h.logger.Error("send empty region updates failed", "err", err)
		}
		return
	}

	date := h.now().Format("02.01.2006")
	for _, info := range report.Regions {
		if err := h.adapter.SendMessage(ctx, chatID, formatRegionUpdatesMessage(info, date)); err != nil {
			h.logger.Error("send region updates failed", "region", info.Region, "err", err)
		}
	}

	if len(report.Errors) > 0 {
		failedRegions := make([]string, 0, len(report.Errors))
		for _, regionErr := range report.Errors {
			region := strings.TrimSpace(regionErr.Region)
			if region == "" {
				region = "unknown"
			}
			failedRegions = append(failedRegions, escapeHTML(region))
		}
		text := "⚠️ Не удалось получить данные по регионам: " + strings.Join(failedRegions, ", ")
		if err := h.adapter.SendMessage(ctx, chatID, text); err != nil {
			h.logger.Error("send partial region updates failed", "err", err)
		}
	}
}

func (h *Handler) handleRaidInfo(ctx context.Context, update *models.Update, piscine domain.PiscineType) {
	chatID, ok := h.guard(ctx, update)
	if !ok {
		return
	}

	weekInfo, err := h.raidUC.DetectCurrentWeek(ctx, piscine)
	if err != nil {
		h.logger.Error("detect week failed", "piscine", piscine, "err", err)
		_ = h.adapter.SendMessage(ctx, chatID, "⚠️ Не удалось определить текущую неделю. Попробуйте позже.")
		return
	}

	if weekInfo.ActiveRaid == nil {
		text := fmt.Sprintf("📌 <b>%s</b> — Неделя %d (Final Exam)\nАктивных рейдов нет.",
			escapeHTML(string(piscine)), weekInfo.WeekNumber)
		_ = h.adapter.SendMessage(ctx, chatID, text)
		return
	}

	raid := weekInfo.ActiveRaid
	text := fmt.Sprintf(
		"📌 <b>%s</b> — Неделя %d\n"+
			"⚔️ Рейд: <b>%s</b>\n"+
			"👥 Команд: %d\n"+
			"📅 %s — %s",
		escapeHTML(string(piscine)),
		weekInfo.WeekNumber,
		escapeHTML(raid.RaidName),
		raid.TeamsCount,
		raid.StartDate.Format("02.01 15:04"),
		raid.EndDate.Format("02.01 15:04"),
	)

	if err := h.adapter.SendMessage(ctx, chatID, text); err != nil {
		h.logger.Error("send raid info failed", "err", err)
	}
}

func (h *Handler) updateTableForActiveRaid(ctx context.Context, spreadsheetID string, raid *domain.RaidInfo, defenseDate time.Time) (string, error) {
	schedule := usecase.CalculateDefenseSchedule(raid.TeamsCount)
	return h.sheets.UpdateDefenseTable(ctx, spreadsheetID, sheets.DefenseTableParams{
		RaidName:    raid.RaidName,
		DefenseDate: defenseDate,
		Schedule:    schedule,
	})
}
