package telegram

import (
	"github.com/go-telegram/bot"
)

// RegisterHandlers registers all command and callback handlers.
func RegisterHandlers(b *bot.Bot, h *Handler) {
	// Commands
	b.RegisterHandler(bot.HandlerTypeMessageText, "/help", bot.MatchTypeExact, h.HandleHelp)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/raidgo", bot.MatchTypeExact, h.HandleRaidGo)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/raidjs", bot.MatchTypeExact, h.HandleRaidJS)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/raidai", bot.MatchTypeExact, h.HandleRaidAI)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/week", bot.MatchTypeExact, h.HandleWeek)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/create_tables", bot.MatchTypeExact, h.HandleTables)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/get_astana_updates", bot.MatchTypeExact, h.HandleAstanaUpdates)

	// Callback queries (inline keyboard buttons).
	// Using MatchTypePrefix because callback data includes piscine type:
	// "defense_create:Piscine Go", "defense_edit:Piscine JS", etc.
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "defense_create:", bot.MatchTypePrefix, h.HandleCallbackCreateTable)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "defense_edit:", bot.MatchTypePrefix, h.HandleCallbackEditParams)
}
