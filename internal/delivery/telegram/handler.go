package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"admin-bot/internal/domain"
	"admin-bot/internal/infra/sheets"
	tgAdapter "admin-bot/internal/infra/telegram"
	"admin-bot/internal/usecase"
)

// Handler processes Telegram commands and callback queries.
type Handler struct {
	raidUC    *usecase.RaidUseCase
	adapter   *tgAdapter.Adapter
	sheets    *sheets.Client // nil if Google Sheets is not configured
	sheetIDs  map[domain.PiscineType]map[int]string
	sheetURLs map[domain.PiscineType]map[int]string
	logger    *slog.Logger
}

const (
	msgSheetsNotConfigured = "⚠️ Google Sheets не настроен. Добавьте GOOGLE_CREDENTIALS_FILE в .env"
	msgSheetNotConfigured  = "⚠️ Таблица для этой недели не настроена. Добавьте SHEET_*_WEEK* в .env"
)

// NewHandler wires the dependencies needed for command and callback handling.
//
// sheetIDs / sheetURLs are the pre-configured Google Sheets tables, keyed by
// piscine and week. Either may be empty — in that case table-related features
// gracefully degrade.
func NewHandler(
	raidUC *usecase.RaidUseCase,
	adapter *tgAdapter.Adapter,
	sheetsClient *sheets.Client,
	sheetIDs map[domain.PiscineType]map[int]string,
	sheetURLs map[domain.PiscineType]map[int]string,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		raidUC:    raidUC,
		adapter:   adapter,
		sheets:    sheetsClient,
		sheetIDs:  sheetIDs,
		sheetURLs: sheetURLs,
		logger:    logger,
	}
}

// lookupSheetID returns the configured spreadsheet ID for (piscine, week)
// or "" if none is set.
func (h *Handler) lookupSheetID(piscine domain.PiscineType, week int) string {
	if m, ok := h.sheetIDs[piscine]; ok {
		return m[week]
	}
	return ""
}

// --- Commands ---

func (h *Handler) HandleHelp(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := update.Message.Chat.ID

	text := "📋 <b>Команды:</b>\n\n" +
		"/help — показать это сообщение\n" +
		"/raidgo — информация о рейде Piscine Go\n" +
		"/raidjs — информация о рейде Piscine JS\n" +
		"/raidai — информация о рейде Piscine AI\n" +
		"/week — текущая неделя для всех Piscine\n" +
		"/create_tables — обновить Google Sheets таблицы защит для всех активных рейдов"

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
	chatID := update.Message.Chat.ID

	var sb strings.Builder
	for _, p := range domain.AllPiscines() {
		weekInfo, err := h.raidUC.DetectCurrentWeek(ctx, p)
		if err != nil {
			fmt.Fprintf(&sb, "❌ %s: %v\n", p, err)
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

		fmt.Fprintf(&sb, "📌 <b>%s</b>: %s | Рейд: %s\n", p, weekLabel, raidName)
	}

	if err := h.adapter.SendMessage(ctx, chatID, sb.String()); err != nil {
		h.logger.Error("send week info failed", "err", err)
	}
}

// CreateTables handles the /create_tables command.
// It iterates over all known Piscines, finds the active raid for each one
// (if any), looks up the pre-configured spreadsheet, and updates its contents.
// The user receives a single message with a per-raid result line.
func (h *Handler) CreateTables(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := update.Message.Chat.ID

	if h.sheets == nil {
		_ = h.adapter.SendMessage(ctx, chatID, msgSheetsNotConfigured)
		return
	}

	// Defense is on Monday — the same date for all tables updated in this run.
	defenseDate := nextMonday(time.Now())

	var lines []string
	updatedCount := 0

	for _, piscine := range domain.AllPiscines() {
		weekInfo, err := h.raidUC.DetectCurrentWeek(ctx, piscine)
		if err != nil {
			h.logger.Warn("detect week failed", "piscine", piscine, "err", err)
			lines = append(lines, fmt.Sprintf("❌ Ошибка при обновлении таблицы (%s): %v", piscine, err))
			continue
		}

		// Skip piscines without an active raid (e.g. final exam week or between raids).
		if weekInfo.ActiveRaid == nil {
			h.logger.Info("skip: no active raid", "piscine", piscine)
			continue
		}

		raid := weekInfo.ActiveRaid

		spreadsheetID := h.lookupSheetID(piscine, weekInfo.WeekNumber)
		if spreadsheetID == "" {
			h.logger.Warn("no sheet configured for week",
				"piscine", piscine, "week", weekInfo.WeekNumber)
			lines = append(lines, fmt.Sprintf("⚠️ %s — таблица для недели %d не настроена", piscine, weekInfo.WeekNumber))
			continue
		}

		url, err := h.updateTableForActiveRaid(ctx, spreadsheetID, raid, defenseDate)
		if err != nil {
			h.logger.Error("update defense table failed",
				"piscine", piscine, "raid", raid.RaidName, "err", err)
			lines = append(lines, fmt.Sprintf("❌ Ошибка при обновлении таблицы (%s): %v", piscine, err))
			continue
		}

		updatedCount++
		lines = append(lines, fmt.Sprintf("✅ Таблица обновлена (%s — %s): %s", piscine, raid.RaidName, url))
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

// updateTableForActiveRaid is the shared "build params, call sheets" step used
// by both /create_tables and the inline "Создать таблицу" callback.
func (h *Handler) updateTableForActiveRaid(ctx context.Context, spreadsheetID string, raid *domain.RaidInfo, defenseDate time.Time) (string, error) {
	schedule := usecase.CalculateDefenseSchedule(raid.TeamsCount)
	return h.sheets.UpdateDefenseTable(ctx, spreadsheetID, sheets.DefenseTableParams{
		RaidName:    raid.RaidName,
		DefenseDate: defenseDate,
		Schedule:    schedule,
	})
}

// --- Callback Queries (inline keyboard buttons) ---

// HandleCallbackCreateTable handles the "Создать таблицу" button press.
// Callback data format: "defense_create:<PiscineType>"
func (h *Handler) HandleCallbackCreateTable(ctx context.Context, b *bot.Bot, update *models.Update) {
	cb := update.CallbackQuery
	if cb == nil {
		return
	}

	piscine := parsePiscineFromCallback(cb.Data, "defense_create:")
	if piscine == "" {
		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            "Ошибка: неизвестный тип Piscine",
		})
		return
	}

	b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: cb.ID,
		Text:            "Обновляю таблицу…",
	})

	chatID := cb.Message.Message.Chat.ID

	if h.sheets == nil {
		_ = h.adapter.SendMessage(ctx, chatID, msgSheetsNotConfigured)
		return
	}

	// Detect current week and get raid info.
	weekInfo, err := h.raidUC.DetectCurrentWeek(ctx, domain.PiscineType(piscine))
	if err != nil || weekInfo.ActiveRaid == nil {
		_ = h.adapter.SendMessage(ctx, chatID, "⚠️ Не удалось получить данные о рейде.")
		return
	}

	spreadsheetID := h.lookupSheetID(domain.PiscineType(piscine), weekInfo.WeekNumber)
	if spreadsheetID == "" {
		h.logger.Warn("no sheet configured for week",
			"piscine", piscine, "week", weekInfo.WeekNumber)
		_ = h.adapter.SendMessage(ctx, chatID, msgSheetNotConfigured)
		return
	}

	raid := weekInfo.ActiveRaid
	url, err := h.updateTableForActiveRaid(ctx, spreadsheetID, raid, nextMonday(time.Now()))
	if err != nil {
		h.logger.Error("update defense table failed", "err", err)
		_ = h.adapter.SendMessage(ctx, chatID, "⚠️ Не удалось обновить таблицу. Попробуйте позже.")
		return
	}

	text := fmt.Sprintf("✅ Таблица защит обновлена!\n📊 %s\n🔗 %s", raid.RaidName, url)
	if err := h.adapter.SendMessage(ctx, chatID, text); err != nil {
		h.logger.Error("send table url failed", "err", err)
	}
}

// HandleCallbackEditParams handles the "Изменить параметры" button press.
func (h *Handler) HandleCallbackEditParams(ctx context.Context, b *bot.Bot, update *models.Update) {
	cb := update.CallbackQuery
	if cb == nil {
		return
	}

	b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: cb.ID,
		Text:            "Изменение параметров",
	})

	chatID := cb.Message.Message.Chat.ID

	// TODO: implement parameter editing flow (e.g. change start time, break count).
	if err := h.adapter.SendMessage(ctx, chatID, "🚧 Изменение параметров — в разработке"); err != nil {
		h.logger.Error("send edit params response failed", "err", err)
	}
}

// --- Defense Reminder (called by scheduler) ---

// SendDefenseReminderWithKeyboard sends the defense reminder with inline buttons.
func (h *Handler) SendDefenseReminderWithKeyboard(ctx context.Context, chatID int64, piscine domain.PiscineType, text string, schedule *usecase.DefenseSchedule) {
	keyboard := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "📊 Обновить таблицу", CallbackData: "defense_create:" + string(piscine)},
				{Text: "✏️ Изменить параметры", CallbackData: "defense_edit:" + string(piscine)},
			},
		},
	}

	if err := h.adapter.SendMessageWithKeyboard(ctx, chatID, text, keyboard); err != nil {
		h.logger.Error("send defense reminder with keyboard failed", "chat_id", chatID, "err", err)
	}
}

// --- Helpers ---

func (h *Handler) handleRaidInfo(ctx context.Context, update *models.Update, piscine domain.PiscineType) {
	chatID := update.Message.Chat.ID

	weekInfo, err := h.raidUC.DetectCurrentWeek(ctx, piscine)
	if err != nil {
		h.logger.Error("detect week failed", "piscine", piscine, "err", err)
		_ = h.adapter.SendMessage(ctx, chatID, "⚠️ Не удалось определить текущую неделю. Попробуйте позже.")
		return
	}

	if weekInfo.ActiveRaid == nil {
		text := fmt.Sprintf("📌 <b>%s</b> — Неделя %d (Final Exam)\nАктивных рейдов нет.", piscine, weekInfo.WeekNumber)
		_ = h.adapter.SendMessage(ctx, chatID, text)
		return
	}

	raid := weekInfo.ActiveRaid
	text := fmt.Sprintf(
		"📌 <b>%s</b> — Неделя %d\n"+
			"⚔️ Рейд: <b>%s</b>\n"+
			"👥 Команд: %d\n"+
			"📅 %s — %s",
		piscine,
		weekInfo.WeekNumber,
		raid.RaidName,
		raid.TeamsCount,
		raid.StartDate.Format("02.01 15:04"),
		raid.EndDate.Format("02.01 15:04"),
	)

	if err := h.adapter.SendMessage(ctx, chatID, text); err != nil {
		h.logger.Error("send raid info failed", "err", err)
	}
}

// parsePiscineFromCallback extracts the piscine type from callback data.
// E.g. "defense_create:Piscine Go" → "Piscine Go"
func parsePiscineFromCallback(data, prefix string) string {
	if !strings.HasPrefix(data, prefix) {
		return ""
	}
	return strings.TrimPrefix(data, prefix)
}

// nextMonday returns the date of the next Monday from the given time.
func nextMonday(t time.Time) time.Time {
	daysUntilMonday := (8 - int(t.Weekday())) % 7
	if daysUntilMonday == 0 {
		daysUntilMonday = 7
	}
	monday := t.AddDate(0, 0, daysUntilMonday)
	return time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, t.Location())
}
