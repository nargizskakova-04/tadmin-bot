package oneedu

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"admin-bot/internal/domain"
	"admin-bot/internal/infra/oneedu/queries"
)

const (
	httpTimeout        = 30 * time.Second
	tokenExpiryLeeway  = 1 * time.Minute
	authTokenPath      = "/api/auth/token"
	authRefreshPath    = "/api/auth/refresh"
	graphqlEndpointURL = "/api/graphql-engine/v1/graphql"

	// maxResponseBytes caps how much of an upstream response we read into memory,
	// guarding against a malicious or malfunctioning endpoint causing OOM.
	maxResponseBytes = 4 << 20 // 4 MiB

	// errBodyLimit bounds how much of an upstream body we splice into an error
	// message. The body is capped at maxResponseBytes for reading, but an error
	// string should stay small.
	errBodyLimit = 1024
)

var defaultQueryFiles = []string{
	"raids.graphql",
	"updates.graphql",
}

// Client communicates with the 01-edu GraphQL API.
type Client struct {
	httpClient  *http.Client
	baseURL     string
	accessToken string
	logger      *slog.Logger

	queryFiles []string
	mu         sync.RWMutex
	jwtToken   string
	jwtExp     time.Time
}

func NewClient(baseURL, accessToken string, logger *slog.Logger) *Client {
	return &Client{
		httpClient:  &http.Client{Timeout: httpTimeout},
		baseURL:     baseURL,
		accessToken: accessToken,
		logger:      logger,
		queryFiles:  defaultQueryFiles,
	}
}

// scrub redacts the platform access token from an error before it is logged or
// returned. The access token is immutable after construction, so reading it
// needs no lock — which is why this helper is safe to call from both locked
// paths (RefreshToken, refreshJWT) and unlocked ones (ensureToken, doGraphQL).
//
// It deliberately does NOT touch the JWT: the JWT only ever travels in request
// headers (x-jwt-token / Authorization), and net/http never embeds headers in
// the *url.Error it returns on transport failure. Response *bodies* that might
// echo a JWT are cleaned with scrubSecrets instead.
func (c *Client) scrub(err error) error {
	if err == nil || c.accessToken == "" {
		return err
	}
	if s := err.Error(); strings.Contains(s, c.accessToken) {
		return errors.New(strings.ReplaceAll(s, c.accessToken, "[REDACTED_ACCESS_TOKEN]"))
	}
	return err
}

// scrubString removes the given secrets from s. Pure helper with no receiver so
// callers control exactly which (lock-protected) values they pass in.
func scrubString(s, accessToken, jwt string) string {
	if accessToken != "" {
		s = strings.ReplaceAll(s, accessToken, "[REDACTED_ACCESS_TOKEN]")
	}
	if jwt != "" {
		s = strings.ReplaceAll(s, jwt, "[REDACTED_JWT]")
	}
	return s
}

// scrubSecrets redacts the access token and the currently-held JWT from a
// string (e.g. an upstream response body surfaced for debugging).
//
// CONTRACT: callers MUST hold c.mu (read or write). It reads c.jwtToken
// directly rather than via tokenState() on purpose: its only callers
// (requestToken, refreshJWT) already hold the write lock, and taking RLock here
// would deadlock the non-reentrant RWMutex.
func (c *Client) scrubSecrets(s string) string {
	return strings.TrimSpace(scrubString(s, c.accessToken, c.jwtToken))
}

// clip trims and bounds a string for safe inclusion in an error message, so a
// pathological upstream body can't produce a multi-megabyte error.
func clip(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) > max {
		return s[:max] + "…(truncated)"
	}
	return s
}

// readCapped reads at most maxResponseBytes from r, scrubbing nothing (callers
// scrub before logging). Returns the bytes read so far on error.
func readCapped(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, maxResponseBytes))
}

// RefreshToken obtains a new JWT from the 01-edu auth endpoint.
func (c *Client) RefreshToken(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	token, exp, err := c.requestToken(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", domain.ErrTokenRefresh, c.scrub(err))
	}
	c.jwtToken = token
	c.jwtExp = exp
	c.logger.Info("01-edu JWT refreshed", "expires", exp)
	return nil
}

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

// GetRaidByName fetches a specific raid event by name.
func (c *Client) GetAstanaUpdates(ctx context.Context) (*domain.AstanaUpdatesInfo, error) {
	now := time.Now()
	vars := map[string]interface{}{
		"endDate":   now.Format("2006-01-02T15:04"),
		"startDate": now.AddDate(0, 0, -360).Format("2006-01-02T15:04"),
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

func (c *Client) runQuery(ctx context.Context, opName string, vars map[string]interface{}, dest interface{}) error {
	if err := c.ensureToken(ctx); err != nil {
		return err
	}
	query, err := queries.LoadOperationFromFiles(c.queryFiles, opName)
	if err != nil {
		return fmt.Errorf("load query %s: %w", opName, err)
	}
	return c.doGraphQL(ctx, query, vars, dest)
}

// --- internal helpers ---

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

func (c *Client) tokenState() (token string, exp time.Time) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.jwtToken, c.jwtExp
}

func (c *Client) ensureToken(ctx context.Context) error {
	token, exp := c.tokenState()
	if token != "" && time.Now().Before(exp.Add(-tokenExpiryLeeway)) {
		return nil
	}

	if token != "" {
		if err := c.refreshJWT(ctx); err != nil {
			c.logger.Warn("refresh failed, requesting new token", "err", c.scrub(err))
		} else {
			return nil
		}
	}

	return c.RefreshToken(ctx)
}

// refreshJWT calls GET /api/auth/refresh with x-jwt-token header.
func (c *Client) refreshJWT(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+authRefreshPath, nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-jwt-token", c.jwtToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return c.scrub(err)
	}
	defer resp.Body.Close()

	body, _ := readCapped(resp.Body)
	if resp.StatusCode != http.StatusOK {
		// Surface the (scrubbed, bounded) upstream body — a refresh failure body
		// almost never contains the token itself, and scrubSecrets redacts it if
		// it does. We hold c.mu here, so scrubSecrets is safe.
		return fmt.Errorf("refresh endpoint returned status %d: %s",
			resp.StatusCode, clip(c.scrubSecrets(string(body)), errBodyLimit))
	}

	token := extractToken(body)
	if token == "" {
		return fmt.Errorf("empty token in refresh response")
	}

	exp, err := parseJWTExpiry(token)
	if err != nil {
		return fmt.Errorf("parse refreshed JWT: %w", err)
	}

	c.jwtToken = token
	c.jwtExp = exp
	c.logger.Info("01-edu JWT refreshed via /api/auth/refresh", "expires", exp)
	return nil
}

func extractToken(body []byte) string {
	return strings.Trim(strings.TrimSpace(string(body)), `"`)
}

// parseJWTExpiry decodes the JWT payload and extracts the "exp" claim.
//
// NOTE: this intentionally does NOT verify the signature. The token is issued
// to this client by the 01-edu platform over a TLS-verified channel and exp is
// used only for refresh caching, never for an authorization decision. If that
// ever changes, signature verification MUST be added.
func parseJWTExpiry(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, fmt.Errorf("invalid JWT: expected 3 parts, got %d", len(parts))
	}

	decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("decode JWT payload: %w", err)
	}

	var claims struct {
		Exp float64 `json:"exp"`
	}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return time.Time{}, fmt.Errorf("unmarshal JWT claims: %w", err)
	}
	if claims.Exp == 0 {
		return time.Time{}, fmt.Errorf("no exp claim in JWT")
	}
	return time.Unix(int64(claims.Exp), 0), nil
}

// requestToken bootstraps a JWT using the platform access token.
//
// The 01-edu deployment exchanges the opaque access token for a JWT at
//
//	GET /api/auth/token?token=<access-token>
//
// returning the JWT as a quoted string. The token MUST ride in the query
// string: this endpoint rejects the X-Access-Token header, an Authorization:
// Bearer value, and a JSON body (verified empirically — all return 4xx/5xx).
//
// SECURITY: a token in the query string can leak into *url.Error messages on
// transport failures and into upstream access logs. scrubURLError() redacts it
// (both raw and URL-encoded forms) from any error we return or log; the
// access-log exposure is inherent to the only request form this endpoint
// accepts. Transit is still protected by TLS (config enforces https://).
func (c *Client) requestToken(ctx context.Context) (string, time.Time, error) {
	endpoint := c.baseURL + authTokenPath + "?" + url.Values{"token": {c.accessToken}}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", time.Time{}, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// The token is in the URL; redact it (raw + encoded) from any transport error.
		return "", time.Time{}, scrubURLError(err, c.accessToken)
	}
	defer resp.Body.Close()

	body, _ := readCapped(resp.Body)
	if resp.StatusCode != http.StatusOK {
		// Surface the (scrubbed, bounded) upstream body so a 4xx names its own
		// cause instead of hiding behind a bare status code. We hold c.mu here
		// (called from RefreshToken under Lock), so scrubSecrets is safe.
		return "", time.Time{}, fmt.Errorf("token endpoint returned status %d: %s",
			resp.StatusCode, clip(c.scrubSecrets(string(body)), errBodyLimit))
	}

	token := extractToken(body)
	if token == "" {
		return "", time.Time{}, fmt.Errorf("empty token in response")
	}

	exp, err := parseJWTExpiry(token)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("parse JWT: %w", err)
	}
	return token, exp, nil
}

// scrubURLError redacts the secret from a *url.Error (which embeds the full URL)
// and from generic errors. The secret rides in the query string via
// url.Values.Encode(), so it may appear percent-encoded in the URL; we redact
// both the raw and the URL-encoded forms, otherwise a token containing +, /, =
// (common in base64 tokens) would slip past redaction.
func scrubURLError(err error, secret string) error {
	if err == nil || secret == "" {
		return err
	}
	encoded := url.QueryEscape(secret) // matches url.Values.Encode()
	redact := func(s string) string {
		s = strings.ReplaceAll(s, secret, "[REDACTED]")
		if encoded != secret {
			s = strings.ReplaceAll(s, encoded, "[REDACTED]")
		}
		return s
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		ue.URL = redact(ue.URL)
		return ue
	}
	return errors.New(redact(err.Error()))
}

// gqlEnvelope lets us detect GraphQL-level errors (HTTP 200 + "errors" array)
// before unmarshalling into the typed destination.
type gqlEnvelope struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (c *Client) doGraphQL(ctx context.Context, query string, variables map[string]interface{}, dest interface{}) error {
	body, err := json.Marshal(map[string]interface{}{
		"query":     query,
		"variables": variables,
	})
	if err != nil {
		return fmt.Errorf("%w: marshal request: %v", domain.ErrGraphQL, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+graphqlEndpointURL, bytes.NewReader(body))
	if err != nil {
		return err
	}

	// Snapshot the JWT once (lock-safe). We do NOT hold c.mu in this method, so
	// this token value is also what we hand scrubString below for redaction.
	token, _ := c.tokenState()
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", domain.ErrGraphQL, c.scrub(err))
	}
	defer resp.Body.Close()

	raw, err := readCapped(resp.Body)
	if err != nil {
		return fmt.Errorf("%w: read body: %v", domain.ErrGraphQL, err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", domain.ErrGraphQL, resp.StatusCode)
	}

	// Detect GraphQL-level errors returned with a 200 status.
	var env gqlEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("%w: decode envelope: %v", domain.ErrGraphQL, err)
	}
	if len(env.Errors) > 0 {
		// Defense in depth: an upstream GraphQL message is unlikely to echo a
		// secret, but scrub it anyway before it reaches the logs.
		msg := clip(scrubString(env.Errors[0].Message, c.accessToken, token), errBodyLimit)
		return fmt.Errorf("%w: %s", domain.ErrGraphQL, msg)
	}
	if len(env.Data) == 0 {
		return fmt.Errorf("%w: empty data", domain.ErrGraphQL)
	}

	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("%w: decode: %v", domain.ErrGraphQL, err)
	}
	return nil
}
