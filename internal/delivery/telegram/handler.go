package telegram

import (
	"context"
	"log/slog"
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
