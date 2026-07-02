package telegram

import (
	"context"
	"errors"
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
	raidUC     *usecase.RaidUseCase
	updatesUC  *usecase.UpdatesUseCase
	adapter    *tgAdapter.Adapter
	sheets     *sheets.Client // nil if Google Sheets is not configured
	sheetIDs   map[domain.PiscineType]map[int]string
	sheetURLs  map[domain.PiscineType]map[int]string
	authorized map[int64]bool // allowlist of chat IDs permitted to issue commands
	loc        *time.Location // configured timezone, used for date arithmetic
	logger     *slog.Logger
}

const (
	msgSheetsNotConfigured = "⚠️ Google Sheets не настроен. Добавьте GOOGLE_CREDENTIALS_FILE в .env"
	msgSheetNotConfigured  = "⚠️ Таблица для этой недели не настроена. Добавьте SHEET_*_WEEK* в .env"
)

// NewHandler wires the dependencies needed for command and callback handling.
//
// authorizedChatIDs is the allowlist of chats permitted to issue commands and
// press inline buttons. loc is the configured timezone used for the defense
// date (nextMonday) so the date does not drift between the container clock
// (UTC) and the cron timezone.
func NewHandler(
	raidUC *usecase.RaidUseCase,
	updatesUC *usecase.UpdatesUseCase,
	adapter *tgAdapter.Adapter,
	sheetsClient *sheets.Client,
	sheetIDs map[domain.PiscineType]map[int]string,
	sheetURLs map[domain.PiscineType]map[int]string,
	authorizedChatIDs []int64,
	loc *time.Location,
	logger *slog.Logger,
) *Handler {
	allow := make(map[int64]bool, len(authorizedChatIDs))
	for _, id := range authorizedChatIDs {
		allow[id] = true
	}
	if loc == nil {
		loc = time.UTC
	}
	return &Handler{
		raidUC:     raidUC,
		updatesUC:  updatesUC,
		adapter:    adapter,
		sheets:     sheetsClient,
		sheetIDs:   sheetIDs,
		sheetURLs:  sheetURLs,
		authorized: allow,
		loc:        loc,
		logger:     logger,
	}
}

// isAuthorized reports whether the given chat may issue commands. If the
// allowlist is empty the bot is "locked" (deny-all) rather than open — this is
// fail-closed by design.
func (h *Handler) isAuthorized(chatID int64) bool {
	return h.authorized[chatID]
}

func (h *Handler) lookupSheetID(piscine domain.PiscineType, week int) string {
	if m, ok := h.sheetIDs[piscine]; ok {
		return m[week]
	}
	return ""
}

// now returns the current time in the configured location.
func (h *Handler) now() time.Time { return time.Now().In(h.loc) }

// --- Commands ---

func (h *Handler) HandleHelp(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil || !h.isAuthorized(update.Message.Chat.ID) {
		return
	}
	chatID := update.Message.Chat.ID

	text := "📋 <b>Команды:</b>\n\n" +
		"/help — показать это сообщение\n" +
		"/raidgo — информация о рейде Piscine Go\n" +
		"/raidjs — информация о рейде Piscine JS\n" +
		"/raidai — информация о рейде Piscine AI\n" +
		"/week — текущая неделя для всех Piscine\n" +
		"/create_tables — обновить Google Sheets таблицы защит для всех активных рейдов\n" +
		"/get_region_updates — статистика обновлений по всем регионам"

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
	if update.Message == nil || !h.isAuthorized(update.Message.Chat.ID) {
		return
	}
	chatID := update.Message.Chat.ID

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
	if update.Message == nil || !h.isAuthorized(update.Message.Chat.ID) {
		if update.Message != nil {
			h.logger.Warn("unauthorized /create_tables", "chat_id", update.Message.Chat.ID)
		}
		return
	}
	chatID := update.Message.Chat.ID

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
	if update.Message == nil || !h.isAuthorized(update.Message.Chat.ID) {
		return
	}
	chatID := update.Message.Chat.ID

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
	if update.Message == nil || !h.isAuthorized(update.Message.Chat.ID) {
		return
	}
	chatID := update.Message.Chat.ID

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

	for _, info := range report.Regions {
		if err := h.adapter.SendMessage(ctx, chatID, formatRegionUpdatesMessage(info)); err != nil {
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

func (h *Handler) updateTableForActiveRaid(ctx context.Context, spreadsheetID string, raid *domain.RaidInfo, defenseDate time.Time) (string, error) {
	schedule := usecase.CalculateDefenseSchedule(raid.TeamsCount)
	return h.sheets.UpdateDefenseTable(ctx, spreadsheetID, sheets.DefenseTableParams{
		RaidName:    raid.RaidName,
		DefenseDate: defenseDate,
		Schedule:    schedule,
	})
}

// --- Callback Queries (inline keyboard buttons) ---

// callbackChatID safely extracts the originating chat ID from a callback query.
// Telegram callbacks can carry a nil or inaccessible Message; callers must
// check ok before using the result.
func callbackChatID(cb *models.CallbackQuery) (int64, bool) {
	// cb.Message is a value of type models.MaybeInaccessibleMessage (not a
	// pointer), so it can't be nil-compared. Its inner .Message is *models.Message
	// and is nil when the callback's source message is inaccessible (too old, or
	// an inline-message origin) — that's the case we must guard against.
	if cb == nil || cb.Message.Message == nil {
		return 0, false
	}
	return cb.Message.Message.Chat.ID, true
}

// answer is a best-effort callback acknowledgement that logs (rather than
// ignores) failures.
func (h *Handler) answer(ctx context.Context, b *bot.Bot, id, text string) {
	if _, err := b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: id,
		Text:            text,
	}); err != nil {
		h.logger.Warn("answer callback failed", "err", err)
	}
}

// HandleCallbackCreateTable handles the "Обновить таблицу" button press.
func (h *Handler) HandleCallbackCreateTable(ctx context.Context, b *bot.Bot, update *models.Update) {
	cb := update.CallbackQuery
	chatID, ok := callbackChatID(cb)
	if !ok {
		if cb != nil {
			h.answer(ctx, b, cb.ID, "Ошибка: сообщение недоступно")
		}
		return
	}

	if !h.isAuthorized(chatID) {
		h.logger.Warn("unauthorized callback", "data", "defense_create", "chat_id", chatID)
		h.answer(ctx, b, cb.ID, "Недостаточно прав")
		return
	}

	piscine := parsePiscineFromCallback(cb.Data, "defense_create:")
	if piscine == "" {
		h.answer(ctx, b, cb.ID, "Ошибка: неизвестный тип Piscine")
		return
	}

	h.answer(ctx, b, cb.ID, "Обновляю таблицу…")

	if h.sheets == nil {
		_ = h.adapter.SendMessage(ctx, chatID, msgSheetsNotConfigured)
		return
	}

	weekInfo, err := h.raidUC.DetectCurrentWeek(ctx, domain.PiscineType(piscine))
	if err != nil || weekInfo.ActiveRaid == nil {
		_ = h.adapter.SendMessage(ctx, chatID, "⚠️ Не удалось получить данные о рейде.")
		return
	}

	spreadsheetID := h.lookupSheetID(domain.PiscineType(piscine), weekInfo.WeekNumber)
	if spreadsheetID == "" {
		h.logger.Warn("no sheet configured for week", "piscine", piscine, "week", weekInfo.WeekNumber)
		_ = h.adapter.SendMessage(ctx, chatID, msgSheetNotConfigured)
		return
	}

	raid := weekInfo.ActiveRaid
	url, err := h.updateTableForActiveRaid(ctx, spreadsheetID, raid, nextMonday(h.now()))
	if err != nil {
		h.logger.Error("update defense table failed", "err", err)
		_ = h.adapter.SendMessage(ctx, chatID, "⚠️ Не удалось обновить таблицу. Попробуйте позже.")
		return
	}

	text := fmt.Sprintf("✅ Таблица защит обновлена!\n📊 %s\n🔗 %s", escapeHTML(raid.RaidName), url)
	if err := h.adapter.SendMessage(ctx, chatID, text); err != nil {
		h.logger.Error("send table url failed", "err", err)
	}
}

// HandleCallbackEditParams handles the "Изменить параметры" button press.
func (h *Handler) HandleCallbackEditParams(ctx context.Context, b *bot.Bot, update *models.Update) {
	cb := update.CallbackQuery
	chatID, ok := callbackChatID(cb)
	if !ok {
		if cb != nil {
			h.answer(ctx, b, cb.ID, "Ошибка: сообщение недоступно")
		}
		return
	}

	if !h.isAuthorized(chatID) {
		h.logger.Warn("unauthorized callback", "data", "defense_edit", "chat_id", chatID)
		h.answer(ctx, b, cb.ID, "Недостаточно прав")
		return
	}

	h.answer(ctx, b, cb.ID, "Изменение параметров")

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

func formatRegionUpdatesMessage(info domain.RegionUpdatesInfo) string {
	region := strings.TrimSpace(info.Region)
	if region == "" {
		region = "unknown"
	}

	return fmt.Sprintf(
		"📍 Region: %s\n\n"+
			"Signed up without onboarding: %d\n"+
			"Succeeded onboarding games: %d\n"+
			"Check-in registrations: %d\n"+
			"Piscine Go registrations: %d\n"+
			"Core/module users: %d",
		escapeHTML(region),
		info.SignedUpWithoutOnboarding,
		info.SucceededOnboardingGames,
		info.CheckinRegistrations,
		info.PiscineGoRegistrations,
		info.CoreUsers,
	)
}

func (h *Handler) handleRaidInfo(ctx context.Context, update *models.Update, piscine domain.PiscineType) {
	if update.Message == nil || !h.isAuthorized(update.Message.Chat.ID) {
		return
	}
	chatID := update.Message.Chat.ID

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
