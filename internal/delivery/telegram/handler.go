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
	raidUC  *usecase.RaidUseCase
	adapter *tgAdapter.Adapter
	sheets  *sheets.Client // nil if Google Sheets is not configured
	logger  *slog.Logger
}

func NewHandler(raidUC *usecase.RaidUseCase, adapter *tgAdapter.Adapter, sheetsClient *sheets.Client, logger *slog.Logger) *Handler {
	return &Handler{
		raidUC:  raidUC,
		adapter: adapter,
		sheets:  sheetsClient,
		logger:  logger,
	}
}

// --- Commands ---

func (h *Handler) HandleHelp(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := update.Message.Chat.ID

	text := "📋 <b>Команды:</b>\n\n" +
		"/help — показать это сообщение\n" +
		"/raidgo — информация о рейде Piscine Go\n" +
		"/raidjs — информация о рейде Piscine JS\n" +
		"/raidai — информация о рейде Piscine AI\n" +
		"/week — текущая неделя для всех Piscine\n"+
		"/create_table_raidgo"

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

	var text string
	for _, p := range domain.AllPiscines() {
		weekInfo, err := h.raidUC.DetectCurrentWeek(ctx, p)
		if err != nil {
			text += fmt.Sprintf("❌ %s: %v\n", p, err)
			continue
		}

		raidName := "—"
		if weekInfo.ActiveRaid != nil {
			raidName = weekInfo.ActiveRaid.RaidName
		}

		isFinal := domain.IsFinalWeek(p, weekInfo.WeekNumber)
		weekLabel := fmt.Sprintf("Неделя %d", weekInfo.WeekNumber)
		if isFinal {
			weekLabel += " (Final Exam)"
		}

		text += fmt.Sprintf("📌 <b>%s</b>: %s | Рейд: %s\n", p, weekLabel, raidName)
	}

	if err := h.adapter.SendMessage(ctx, chatID, text); err != nil {
		h.logger.Error("send week info failed", "err", err)
	}
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
		Text:            "Создаю таблицу…",
	})

	chatID := cb.Message.Message.Chat.ID

	if h.sheets == nil {
		_ = h.adapter.SendMessage(ctx, chatID, "⚠️ Google Sheets не настроен. Добавьте GOOGLE_CREDENTIALS_FILE в .env")
		return
	}

	// Detect current week and get raid info.
	weekInfo, err := h.raidUC.DetectCurrentWeek(ctx, domain.PiscineType(piscine))
	if err != nil || weekInfo.ActiveRaid == nil {
		_ = h.adapter.SendMessage(ctx, chatID, "⚠️ Не удалось получить данные о рейде.")
		return
	}

	raid := weekInfo.ActiveRaid
	schedule := usecase.CalculateDefenseSchedule(raid.TeamsCount)

	// Defense is on Monday (next day after Sunday).
	defenseDate := nextMonday(time.Now())

	url, err := h.sheets.CreateDefenseTable(ctx, sheets.DefenseTableParams{
		RaidName:    raid.RaidName,
		DefenseDate: defenseDate,
		Schedule:    schedule,
	})
	if err != nil {
		h.logger.Error("create defense table failed", "err", err)
		_ = h.adapter.SendMessage(ctx, chatID, "⚠️ Не удалось создать таблицу. Попробуйте позже.")
		return
	}

	text := fmt.Sprintf("✅ Таблица защит создана!\n📊 %s\n🔗 %s", raid.RaidName, url)
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
				{Text: "📊 Создать таблицу", CallbackData: "defense_create:" + string(piscine)},
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
