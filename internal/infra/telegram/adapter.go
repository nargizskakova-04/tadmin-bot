package telegram

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// Adapter wraps the go-telegram/bot library.
type Adapter struct {
	bot    *bot.Bot
	logger *slog.Logger
}

func NewAdapter(token string, logger *slog.Logger) (*Adapter, error) {
	b, err := bot.New(token)
	if err != nil {
		return nil, fmt.Errorf("create telegram bot: %w", err)
	}
	return &Adapter{bot: b, logger: logger}, nil
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
		a.logger.Error("telegram send with keyboard failed", "chat_id", chatID, "err", err)
		return fmt.Errorf("send message with keyboard: %w", err)
	}
	return nil
}

// Start begins long-polling for updates.
func (a *Adapter) Start(ctx context.Context) {
	a.bot.Start(ctx)
}
