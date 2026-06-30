package domain

import "context"

// OneEduClient communicates with the 01-edu GraphQL API.
type OneEduClient interface {
	// RefreshToken obtains or renews the JWT token.
	RefreshToken(ctx context.Context) error

	// GetCurrentPiscineID returns the active piscine event for the given piscine name.
	// Returns nil if no active piscine is found.
	GetCurrentPiscineID(ctx context.Context, piscine PiscineType) (*PiscineInfo, error)

	// GetRaidsByPiscineID returns all raid events for a given piscine event ID.
	GetRaidsByPiscineID(ctx context.Context, piscine PiscineType, piscineEventID int) ([]RaidInfo, error)

	// GetRaidByName returns a specific raid event by name, starting from a given date.
	GetRaidByName(ctx context.Context, name string, startAt string) (*RaidInfo, error)

	//GetAstanaUpdates returns the latest updates for Astana.
	GetAstanaUpdates(ctx context.Context) (*AstanaUpdatesInfo, error)
}

// TemplateRenderer renders message templates with variable substitution.
type TemplateRenderer interface {
	Render(key string, vars map[string]string) (string, error)
}

// BotSender sends messages via Telegram.
type BotSender interface {
	SendMessage(ctx context.Context, chatID int64, text string) error
}

// Scheduler manages periodic job execution.
type Scheduler interface {
	Start()
	Stop()
}
