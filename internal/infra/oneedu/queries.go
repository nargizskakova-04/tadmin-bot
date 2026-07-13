package oneedu

import (
	"context"
	"fmt"
	"strings"
	"time"

	"admin-bot/internal/domain"
)

// GetCurrentPiscineID fetches the active piscine event by name.
func (c *Client) GetCurrentPiscineID(ctx context.Context, piscine domain.PiscineType) (*domain.PiscineInfo, error) {
	vars := map[string]interface{}{"name": string(piscine)}

	var resp piscineResponse
	if err := c.runQuery(ctx, "GetCurrentPiscineId", vars, &resp); err != nil {
		return nil, err
	}

	if len(resp.Data.Event) == 0 {
		c.logger.Warn("no active piscine found", "name", piscine)
		return nil, nil
	}

	ev := resp.Data.Event[0]
	return &domain.PiscineInfo{ID: ev.ID, StartAt: ev.StartAt, EndAt: ev.EndAt}, nil
}

// GetRaidsByPiscineID fetches all raid events for a given piscine event ID.
func (c *Client) GetRaidsByPiscineID(ctx context.Context, piscine domain.PiscineType, piscineEventID int) ([]domain.RaidInfo, error) {
	opName := domain.GetRaidQueryName(piscine)
	if opName == "" {
		return nil, fmt.Errorf("%w: %s", domain.ErrPiscineNotFound, piscine)
	}

	vars := map[string]interface{}{"id": piscineEventID}

	var resp raidsResponse
	if err := c.runQuery(ctx, opName, vars, &resp); err != nil {
		return nil, err
	}

	raids := make([]domain.RaidInfo, 0, len(resp.Data.Event))
	for _, ev := range resp.Data.Event {
		raids = append(raids, mapEventToRaidInfo(piscine, ev))
	}
	return raids, nil
}

// GetRaidByName fetches a specific raid event by name.
func (c *Client) GetRaidByName(ctx context.Context, name string, startAt string) (*domain.RaidInfo, error) {
	vars := map[string]interface{}{"name": name, "startAt": startAt}

	var resp raidsResponse
	if err := c.runQuery(ctx, "GetRaidByName", vars, &resp); err != nil {
		return nil, err
	}

	if len(resp.Data.Event) == 0 {
		return nil, nil
	}
	info := mapEventToRaidInfo("", resp.Data.Event[0])
	return &info, nil
}

// GetAstanaUpdates returns the latest updates for Astana.
func (c *Client) GetAstanaUpdates(ctx context.Context) (*domain.AstanaUpdatesInfo, error) {
	now := time.Now()
	vars := map[string]interface{}{
		"endDate":   now.Format("2006-01-02T15:04"),
		"startDate": now.AddDate(0, 0, -regionUpdatesLookbackDays).Format("2006-01-02T15:04"),
	}

	var resp astanaUpdatesResponse
	if err := c.runQuery(ctx, "GetAstanaUpdates", vars, &resp); err != nil {
		return nil, err
	}

	info := domain.AstanaUpdatesInfo{
		Total:     resp.Data.TotalAstana.Aggregate.Count,
		Succeeded: resp.Data.SucceededAstana.Aggregate.Count,
		Checkin:   resp.Data.CheckinAstana.Aggregate.Count,
		Piscinego: resp.Data.PiscinegoAstana.Aggregate.Count,
	}
	return &info, nil
}

// GetCampuses fetches all campus names from OneEdu.
func (c *Client) GetCampuses(ctx context.Context) ([]string, error) {
	var resp campusesResponse
	if err := c.runQuery(ctx, "all_campuses", map[string]interface{}{}, &resp); err != nil {
		return nil, err
	}
	if resp.Data == nil {
		return nil, fmt.Errorf("%w: empty campuses response", domain.ErrGraphQL)
	}
	if len(resp.Data.Object) == 0 {
		return nil, nil
	}

	campuses := make([]string, 0, len(resp.Data.Object))
	for _, obj := range resp.Data.Object {
		name := strings.TrimSpace(obj.Name)
		if name == "" {
			c.logger.Warn("skip campus with empty name")
			continue
		}
		campuses = append(campuses, name)
	}
	if len(campuses) == 0 {
		return nil, fmt.Errorf("%w: campuses response contains no valid names", domain.ErrGraphQL)
	}
	return campuses, nil
}

// GetEventByID fetches the metadata for a single event. Returns nil (no error)
// when no event with that ID exists, so callers can distinguish "not found"
// from a transport/GraphQL failure.
func (c *Client) GetEventByID(ctx context.Context, id int) (*domain.EventMeta, error) {
	var resp eventByIDResponse
	if err := c.runQuery(ctx, "GetEventByID", map[string]interface{}{"id": id}, &resp); err != nil {
		return nil, err
	}
	if len(resp.Data.Event) == 0 {
		return nil, nil
	}
	ev := resp.Data.Event[0]
	return &domain.EventMeta{
		ID:         ev.ID,
		Path:       ev.Path,
		ObjectType: ev.Object.Type,
		ObjectName: ev.Object.Name,
		StartAt:    ev.StartAt,
		EndAt:      ev.EndAt,
	}, nil
}

// GetRegionUpdates fetches onboarding and registration stats for one campus.
//
// The pinned event IDs in events are the source of truth: each configured ID is
// fetched and verified (exists, belongs to this region, not ended) before its
// authoritative path is used to count. Unpinned metrics keep the historical
// path-based lookup derived from the campus name. Events that fail verification
// are recorded in the returned RegionUpdatesInfo.StaleEvents (and logged) rather
// than being silently trusted; the rest of the region is still reported.
func (c *Client) GetRegionUpdates(ctx context.Context, campus string, events domain.RegionUpdateEventsConfig) (*domain.RegionUpdatesInfo, error) {
	campus = strings.TrimSpace(campus)
	if campus == "" {
		return nil, fmt.Errorf("empty campus name")
	}

	now := time.Now()
	startDate := now.AddDate(0, 0, -regionUpdatesLookbackDays)
	vars := buildRegionStatsVariables(campus, startDate, now)

	// Resolve each pinned event ID to its authoritative path, verifying it. A
	// verified, active event overrides the default path variable so the count
	// tracks exactly the pinned event; a failed one is flagged stale and left on
	// its default path (whose count the caller will present as unavailable).
	stale := c.resolvePinnedEvents(ctx, campus, now, events, vars)

	var resp regionUpdatesResponse
	if err := c.runQuery(ctx, "region_stats", vars, &resp); err != nil {
		return nil, err
	}
	if resp.Data == nil {
		return nil, fmt.Errorf("%w: empty region stats response for %s", domain.ErrGraphQL, campus)
	}

	info, err := mapRegionUpdates(campus, *resp.Data)
	if err != nil {
		return nil, err
	}
	info.StaleEvents = stale
	return info, nil
}

// resolvePinnedEvents validates each pinned event ID for a region and, for the
// ones that pass, overrides the matching path variable in vars so region_stats
// counts the exact pinned event. It returns the events that failed validation.
func (c *Client) resolvePinnedEvents(
	ctx context.Context,
	campus string,
	now time.Time,
	events domain.RegionUpdateEventsConfig,
	vars map[string]interface{},
) []domain.StaleEvent {
	pins := []struct {
		typ    domain.EventType
		id     int
		varKey string
	}{
		{domain.EventCheckin, events.CheckinEventID, "checkinPath"},
		{domain.EventPiscineGo, events.PiscineEventID, "piscinegoPath"},
		{domain.EventModule, events.ModuleEventID, "corePath"},
	}

	var stale []domain.StaleEvent
	for _, p := range pins {
		if p.id == 0 {
			continue // not pinned — keep the default path-based lookup
		}

		meta, err := c.GetEventByID(ctx, p.id)
		if err != nil {
			c.logger.Warn("region event lookup failed",
				"region", campus, "eventType", p.typ, "eventID", p.id, "err", err)
			stale = append(stale, domain.StaleEvent{Type: p.typ, EventID: p.id, Reason: "lookup failed"})
			continue
		}

		status := classifyPinnedEvent(campus, meta, now)
		if status.Reason != "" {
			c.logger.Warn("pinned region event unusable",
				"region", campus, "eventType", p.typ, "eventID", p.id, "reason", status.Reason)
			stale = append(stale, domain.StaleEvent{Type: p.typ, EventID: p.id, Reason: status.Reason})
			continue
		}

		// Verified and active: pin the authoritative path for counting.
		if status.Path != "" {
			vars[p.varKey] = status.Path
		}
	}
	return stale
}

// pinnedEventStatus is the outcome of validating a fetched pinned event.
type pinnedEventStatus struct {
	Path   string // authoritative path to count with, when usable
	Reason string // non-empty when the event is unusable (see StaleEvent.Reason)
}

// classifyPinnedEvent validates a fetched event for a region against the clock.
// A nil meta means the pinned ID resolved to no event. Pure — no I/O, no logging
// — so the verification rules (existence, region, not-ended) are unit-testable.
func classifyPinnedEvent(campus string, meta *domain.EventMeta, now time.Time) pinnedEventStatus {
	if meta == nil {
		return pinnedEventStatus{Reason: "not found"}
	}
	if r := domain.RegionOfPath(meta.Path); r != "" && !strings.EqualFold(r, campus) {
		return pinnedEventStatus{Reason: "region mismatch"}
	}
	if !meta.EndAt.IsZero() && !meta.EndAt.After(now) {
		return pinnedEventStatus{Reason: "ended"}
	}
	return pinnedEventStatus{Path: meta.Path}
}

func buildRegionStatsVariables(region string, startDate, endDate time.Time) map[string]interface{} {
	region = strings.TrimSpace(region)
	return map[string]interface{}{
		"campus":        region,
		"startDate":     startDate.Format(time.RFC3339),
		"endDate":       endDate.Format(time.RFC3339),
		"adminRole":     "campus_admin_" + region,
		"gamesPath":     "/" + region + "/onboarding/games",
		"checkinPath":   "/" + region + "/onboarding/checkin",
		"piscinegoPath": "/" + region + "/piscinego",
		"corePath":      "/" + region + "/module",
	}
}

func mapRegionUpdates(region string, data regionUpdatesNode) (*domain.RegionUpdatesInfo, error) {
	signedUp, err := strictCount(data.SignedUpNoOnboarding, "signed_up_no_onboarding")
	if err != nil {
		return nil, err
	}
	succeeded, err := strictCount(data.Succeeded, "succeeded")
	if err != nil {
		return nil, err
	}
	checkin, err := strictCount(data.Checkin, "checkin")
	if err != nil {
		return nil, err
	}
	piscinego, err := strictCount(data.Piscinego, "piscinego")
	if err != nil {
		return nil, err
	}
	core, err := strictCount(data.Core, "core")
	if err != nil {
		return nil, err
	}

	return &domain.RegionUpdatesInfo{
		Region:                    region,
		SignedUpWithoutOnboarding: signedUp,
		SucceededOnboardingGames:  succeeded,
		CheckinRegistrations:      checkin,
		PiscineGoRegistrations:    piscinego,
		CoreUsers:                 core,
	}, nil
}

func strictCount(node strictAggregateCountNode, field string) (int, error) {
	if node.Aggregate == nil {
		return 0, fmt.Errorf("%w: missing aggregate for %s", domain.ErrGraphQL, field)
	}
	return node.Aggregate.Count, nil
}

func mapEventToRaidInfo(piscine domain.PiscineType, ev raidEventNode) domain.RaidInfo {
	teams := make([]domain.Team, 0, len(ev.Groups))
	for _, g := range ev.Groups {
		members := make([]string, 0, len(g.Members))
		for _, m := range g.Members {
			members = append(members, m.UserLogin)
		}
		teams = append(teams, domain.Team{
			Captain: g.Captain.Login,
			Members: members,
			Status:  g.GroupStatus.Status,
		})
	}

	weekNum := 0
	if piscine != "" {
		weekNum = domain.WeekNumberByRaid(piscine, ev.Object.Name)
	}

	return domain.RaidInfo{
		Piscine:    piscine,
		EventID:    ev.ID,
		RaidName:   ev.Object.Name,
		WeekNumber: weekNum,
		TeamsCount: len(teams),
		Teams:      teams,
		StartDate:  ev.StartAt,
		EndDate:    ev.EndAt,
	}
}
