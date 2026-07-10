package domain

import "time"

type AstanaUpdatesInfo struct {
	Total     int
	Succeeded int
	Checkin   int
	Piscinego int
}

type RegionUpdatesInfo struct {
	Region                    string
	SignedUpWithoutOnboarding int
	SucceededOnboardingGames  int
	CheckinRegistrations      int
	PiscineGoRegistrations    int
	CoreUsers                 int

	// StaleEvents lists pinned events (see RegionUpdateEventsConfig) that were
	// resolved but failed verification — they do not exist, belong to another
	// region, or have already ended. The counts for the corresponding metrics
	// are NOT trustworthy and callers should surface them as unavailable rather
	// than silently reporting a stale (often zero) number.
	StaleEvents []StaleEvent
}

// IsStale reports whether the given event type was flagged unusable for this
// region (missing / wrong region / ended). See StaleEvents.
func (r RegionUpdatesInfo) IsStale(t EventType) bool {
	for _, s := range r.StaleEvents {
		if s.Type == t {
			return true
		}
	}
	return false
}

// StaleEvent records why a pinned region event could not be used.
type StaleEvent struct {
	Type    EventType
	EventID int
	Reason  string // "not found" | "region mismatch" | "ended" | "lookup failed"
}

// EventMeta is the subset of a 01-edu event needed to validate a pinned event
// ID before trusting it as the source of truth for a region's stats.
type EventMeta struct {
	ID         int
	Path       string
	ObjectType string
	ObjectName string
	StartAt    time.Time
	EndAt      time.Time
}

type RegionUpdatesError struct {
	Region string
	Err    error
}

type RegionUpdatesReport struct {
	Regions []RegionUpdatesInfo
	Errors  []RegionUpdatesError
}
