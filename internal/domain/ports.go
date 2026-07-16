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

	// GetAstanaUpdates returns the latest updates for Astana.
	GetAstanaUpdates(ctx context.Context) (*AstanaUpdatesInfo, error)

	// GetCampuses returns all campus names from OneEdu.
	GetCampuses(ctx context.Context) ([]string, error)

	// GetEventByID returns the metadata for a single event, or nil if no event
	// with that ID exists. Used to verify pinned region events.
	GetEventByID(ctx context.Context, id int) (*EventMeta, error)

	// GetRegionUpdates returns onboarding and registration stats for a campus.
	// events pins the authoritative event IDs for the region; a zero-valued
	// config makes every metric fall back to the default path-based lookup.
	GetRegionUpdates(ctx context.Context, campus string, events RegionUpdateEventsConfig) (*RegionUpdatesInfo, error)
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

// AccessStore persists access requests. Implementations must be safe for
// concurrent use.
type AccessStore interface {
	// Get returns the stored request for a user and whether one exists.
	Get(userID int64) (AccessRequest, bool)
	// Save inserts or overwrites the request and durably persists the store.
	Save(req AccessRequest) error
	// ListPending returns every request with status pending, ordered by
	// RequestedAt (oldest first).
	ListPending() ([]AccessRequest, error)
}
