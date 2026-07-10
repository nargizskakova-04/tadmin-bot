package domain

import "strings"

// EventType identifies which pinned region event a value refers to.
type EventType string

const (
	EventCheckin   EventType = "checkin"
	EventPiscineGo EventType = "piscinego"
	EventModule    EventType = "module"
)

// RegionUpdateEventsConfig pins the 01-edu event IDs that are the source of
// truth for a region's updates.
//
// The platform can hold non-linear event data — a piscine created in the wrong
// order, a duplicate/extra event — so region stats must NOT be derived from
// event name or creation order. When an ID is pinned here it becomes
// authoritative: the event is fetched by ID, verified (exists, right region,
// not ended), and its path drives the aggregate counts.
//
// A zero value for any field means "not pinned": that metric falls back to the
// region's default path-based lookup, preserving historical behavior.
type RegionUpdateEventsConfig struct {
	CheckinEventID int
	PiscineEventID int
	ModuleEventID  int
}

// DefaultRegionUpdateEvents returns the built-in per-region event-ID pins.
//
// It is intentionally empty. The platform's event IDs are operational data that
// changes each cohort, not source constants, so out of the box every region
// uses its default path-based lookup — behavior is unchanged from before this
// feature existed. Operators pin IDs per region via environment variables
//
//	REGION_<NAME>_CHECKIN_EVENT_ID
//	REGION_<NAME>_PISCINE_EVENT_ID
//	REGION_<NAME>_MODULE_EVENT_ID
//
// which are merged onto this map field-by-field (see config.Load). To ship a
// hard-coded default for a region, add an entry here, e.g.:
//
//	"astanahub": {CheckinEventID: 123, PiscineEventID: 456, ModuleEventID: 789},
func DefaultRegionUpdateEvents() map[string]RegionUpdateEventsConfig {
	return map[string]RegionUpdateEventsConfig{}
}

// RegionOfPath extracts the campus/region segment from an event path such as
// "/astanahub/onboarding/checkin" -> "astanahub". Returns "" when the path has
// no leading segment.
func RegionOfPath(path string) string {
	p := strings.Trim(strings.TrimSpace(path), "/")
	if p == "" {
		return ""
	}
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return p
}
