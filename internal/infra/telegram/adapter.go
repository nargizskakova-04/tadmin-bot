package telegram

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// Adapter wraps the go-telegram/bot library.
type Adapter struct {
	bot    *bot.Bot
	token  string // kept so we can redact it from library errors
	logger *slog.Logger
}

func NewAdapter(token string, logger *slog.Logger) (*Adapter, error) {
	a := &Adapter{token: token, logger: logger}

	// The go-telegram/bot library issues requests to
	// https://api.telegram.org/bot<TOKEN>/... — the token is in the URL path.
	// On a transport failure net/http embeds the full URL (token included) in
	// *url.Error. The library's DEFAULT errors handler would print that during
	// long-polling, leaking the token. Install our own scrubbing handler.
	b, err := bot.New(token, bot.WithErrorsHandler(func(err error) {
		logger.Error("telegram polling error", "err", a.scrub(err))
	}))
	if err != nil {
		// bot.New validates the token (getMe), so even this error can carry it.
		return nil, fmt.Errorf("create telegram bot: %w", a.scrub(err))
	}
	a.bot = b
	return a, nil
}

// scrub redacts the bot token from an error before it is logged or returned.
// The token is URL-safe (digits, letters, ':', '_', '-'), so no percent-encoding
// occurs in the path and a raw substring match is sufficient.
func (a *Adapter) scrub(err error) error {
	if err == nil || a.token == "" {
		return err
	}
	if s := err.Error(); strings.Contains(s, a.token) {
		return errors.New(strings.ReplaceAll(s, a.token, "[REDACTED_BOT_TOKEN]"))
	}
	return err
}

// Bot returns the underlying *bot.Bot for handler registration.
func (a *Adapter) Bot() *bot.Bot { return a.bot }

// SendMessage sends a plain HTML-formatted message.
func (a *Adapter) SendMessage(ctx context.Context, chatID int64, text string) error {
	_, err := a.bot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      text,
		ParseMode: models.ParseModeHTML,
	})
	if err != nil {
		err = a.scrub(err)
		a.logger.Error("telegram send failed", "chat_id", chatID, "err", err)
		return fmt.Errorf("send message: %w", err)
	}
	return nil
}

// SendMessageWithKeyboard sends a message with an inline keyboard.
func (a *Adapter) SendMessageWithKeyboard(ctx context.Context, chatID int64, text string, keyboard *models.InlineKeyboardMarkup) error {
	_, err := a.bot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: keyboard,
	})
	if err != nil {
		err = a.scrub(err)
		a.logger.Error("telegram send with keyboard failed", "chat_id", chatID, "err", err)
		return fmt.Errorf("send message with keyboard: %w", err)
	}
	return nil
}

// SendDocument uploads an in-memory file (e.g. a zip or xlsx) to the chat with
// an optional HTML caption.
func (a *Adapter) SendDocument(ctx context.Context, chatID int64, filename string, data []byte, caption string) error {
	_, err := a.bot.SendDocument(ctx, &bot.SendDocumentParams{
		ChatID: chatID,
		Document: &models.InputFileUpload{
			Filename: filename,
			Data:     bytes.NewReader(data),
		},
		Caption:   caption,
		ParseMode: models.ParseModeHTML,
	})
	if err != nil {
		err = a.scrub(err)
		a.logger.Error("telegram send document failed", "chat_id", chatID, "filename", filename, "err", err)
		return fmt.Errorf("send document: %w", err)
	}
	return nil
}

// Start begins long-polling for updates.
func (a *Adapter) Start(ctx context.Context) {
	a.bot.Start(ctx)
}
