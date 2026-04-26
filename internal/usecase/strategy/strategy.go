package strategy

import "admin-bot/internal/domain"

// PiscineStrategy provides piscine-specific configuration and template logic.
type PiscineStrategy interface {
	// Type returns the PiscineType this strategy handles.
	Type() domain.PiscineType

	// SupportsMessage returns true if this message type is applicable
	// for the given week number.
	SupportsMessage(msgType domain.MessageType, weekNumber int) bool

	// TemplateKey returns the template file key for a given message type.
	TemplateKey(msgType domain.MessageType) string

	// TemplateVars builds template variables for a given message type and raid info.
	// Extra vars (like SHEET_URL, RECOMMENDED_SCHEDULE) can be passed via extra.
	TemplateVars(msgType domain.MessageType, info *domain.RaidInfo, extra map[string]string) map[string]string
}
