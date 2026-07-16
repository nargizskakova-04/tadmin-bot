package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"admin-bot/internal/domain"
)

const (
	msgAlreadyAuthorized = "✅ Вы уже авторизованы. /help — список команд."
	msgAccessPending     = "⏳ Ваша заявка на рассмотрении у администратора."
	msgAccessRejected    = "Ваша заявка была отклонена. Обратитесь к администратору напрямую."
	msgAccessSubmitted   = "✅ Заявка отправлена. Ждите одобрения."
	msgAccessError       = "⚠️ Не удалось обработать заявку. Попробуйте позже."
	msgAccessGranted     = "✅ Доступ одобрен! Можете пользоваться ботом."
	msgAccessDenied      = "❌ Ваша заявка отклонена."
)

// HandleStart is the entry point for new users. It has no isAuthorized guard —
// it is the mechanism by which unauthorized users request access.
func (h *Handler) HandleStart(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil || update.Message.From == nil {
		return
	}
	h.handleAccessEntry(ctx, update.Message)
}

// handleAccessEntry replies according to the sender's current access state and,
// for a genuinely new user, creates a pending request and notifies the
// super-admin. It is shared by /start and by the private-chat fallback so a
// user never has to know /start exists. Only ever called for private chats
// (chatID == userID).
func (h *Handler) handleAccessEntry(ctx context.Context, msg *models.Message) {
	from := msg.From
	userID := from.ID
	chatID := msg.Chat.ID // == userID in a private chat

	if userID == h.superAdminID {
		_ = h.adapter.SendMessage(ctx, chatID, msgAlreadyAuthorized)
		return
	}

	// Branch on the exact stored status without creating a request, so repeated
	// /start calls don't re-notify the super-admin.
	if existing, ok := h.accessUC.Get(userID); ok {
		switch existing.Status {
		case domain.AccessApproved:
			_ = h.adapter.SendMessage(ctx, chatID, msgAlreadyAuthorized)
		case domain.AccessRejected:
			_ = h.adapter.SendMessage(ctx, chatID, msgAccessRejected)
		default: // pending
			_ = h.adapter.SendMessage(ctx, chatID, msgAccessPending)
		}
		return
	}

	req, err := h.accessUC.RequestAccess(userID, from.Username, from.FirstName)
	if err != nil {
		h.logger.Error("request access failed", "user_id", userID, "err", err)
		_ = h.adapter.SendMessage(ctx, chatID, msgAccessError)
		return
	}

	if err := h.adapter.SendMessage(ctx, chatID, msgAccessSubmitted); err != nil {
		h.logger.Error("send access submitted failed", "err", err)
	}
	h.notifySuperAdmin(ctx, req)
}

// notifySuperAdmin sends the super-admin an approve/reject prompt for a new
// request. The super-admin's ID doubles as their private chat ID.
func (h *Handler) notifySuperAdmin(ctx context.Context, req domain.AccessRequest) {
	if h.superAdminID == 0 {
		h.logger.Warn("no super admin configured — access request goes unreviewed", "user_id", req.UserID)
		return
	}

	name := strings.TrimSpace(req.FirstName)
	if name == "" {
		name = "—"
	}
	username := "—"
	if req.Username != "" {
		username = "@" + req.Username
	}

	text := fmt.Sprintf(
		"🔔 <b>Новая заявка на доступ</b>\n\n"+
			"👤 %s (%s)\n"+
			"🆔 <code>%d</code>\n"+
			"🕒 %s",
		escapeHTML(name), escapeHTML(username), req.UserID,
		req.RequestedAt.In(h.loc).Format("02.01.2006 15:04"),
	)

	keyboard := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "✅ Одобрить", CallbackData: fmt.Sprintf("access_approve:%d", req.UserID)},
				{Text: "❌ Отклонить", CallbackData: fmt.Sprintf("access_reject:%d", req.UserID)},
			},
		},
	}

	if err := h.adapter.SendMessageWithKeyboard(ctx, h.superAdminID, text, keyboard); err != nil {
		h.logger.Error("notify super admin failed", "user_id", req.UserID, "err", err)
	}
}

// HandleCallbackAccessApprove handles the "✅ Одобрить" button.
func (h *Handler) HandleCallbackAccessApprove(ctx context.Context, b *bot.Bot, update *models.Update) {
	h.handleAccessDecision(ctx, b, update, true)
}

// HandleCallbackAccessReject handles the "❌ Отклонить" button.
func (h *Handler) HandleCallbackAccessReject(ctx context.Context, b *bot.Bot, update *models.Update) {
	h.handleAccessDecision(ctx, b, update, false)
}

func (h *Handler) handleAccessDecision(ctx context.Context, b *bot.Bot, update *models.Update, approve bool) {
	cb := update.CallbackQuery
	if cb == nil {
		return
	}

	// Defense in depth: only the super-admin may decide, even though the prompt
	// is only ever sent to their private chat.
	if cb.From.ID != h.superAdminID {
		h.logger.Warn("unauthorized access decision", "user_id", cb.From.ID)
		h.answer(ctx, b, cb.ID, "Недостаточно прав")
		return
	}

	prefix := "access_approve:"
	if !approve {
		prefix = "access_reject:"
	}
	userID, err := parseUserIDFromCallback(cb.Data, prefix)
	if err != nil {
		h.answer(ctx, b, cb.ID, "Ошибка: неверные данные")
		return
	}

	var req domain.AccessRequest
	if approve {
		req, err = h.accessUC.Approve(userID)
	} else {
		req, err = h.accessUC.Reject(userID)
	}
	if err != nil {
		h.logger.Error("access decision failed", "approve", approve, "user_id", userID, "err", err)
		h.answer(ctx, b, cb.ID, "Ошибка при сохранении")
		return
	}

	username := ""
	if req.Username != "" {
		username = "@" + req.Username
	} else {
		username = fmt.Sprintf("id %d", userID)
	}

	settled := fmt.Sprintf("✅ Одобрено (%s)", escapeHTML(username))
	ack := "Одобрено"
	requesterMsg := msgAccessGranted
	if !approve {
		settled = fmt.Sprintf("❌ Отклонено (%s)", escapeHTML(username))
		ack = "Отклонено"
		requesterMsg = msgAccessDenied
	}

	h.answer(ctx, b, cb.ID, ack)

	// Replace the prompt (and its buttons) with the settled decision.
	if chatID, ok := callbackChatID(cb); ok {
		if err := h.adapter.EditMessageText(ctx, chatID, cb.Message.Message.ID, settled); err != nil {
			h.logger.Warn("edit decision message failed", "err", err)
		}
	}

	// Tell the requester in their own DM (chatID == userID for private chats).
	if err := h.adapter.SendMessage(ctx, userID, requesterMsg); err != nil {
		h.logger.Warn("notify requester failed", "user_id", userID, "err", err)
	}
}

// parseUserIDFromCallback extracts the trailing int64 user ID from callback
// data of the form "<prefix><id>".
func parseUserIDFromCallback(data, prefix string) (int64, error) {
	if !strings.HasPrefix(data, prefix) {
		return 0, fmt.Errorf("callback data %q missing prefix %q", data, prefix)
	}
	return strconv.ParseInt(strings.TrimPrefix(data, prefix), 10, 64)
}
