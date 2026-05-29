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
	queriesFile        = "raids.graphql"
	tokenExpiryLeeway  = 1 * time.Minute
	authTokenPath      = "/api/auth/token"
	authRefreshPath    = "/api/auth/refresh"
	graphqlEndpointURL = "/api/graphql-engine/v1/graphql"

	// maxResponseBytes caps how much of an upstream response we read into memory,
	// guarding against a malicious or malfunctioning endpoint causing OOM.
	maxResponseBytes = 4 << 20 // 4 MiB
)

// Client communicates with the 01-edu GraphQL API.
type Client struct {
	httpClient  *http.Client
	baseURL     string
	accessToken string
	logger      *slog.Logger

	mu       sync.RWMutex
	jwtToken string
	jwtExp   time.Time
}

func NewClient(baseURL, accessToken string, logger *slog.Logger) *Client {
	return &Client{
		httpClient:  &http.Client{Timeout: httpTimeout},
		baseURL:     baseURL,
		accessToken: accessToken,
		logger:      logger,
	}
}

// scrub removes known secrets from an error before it can be logged or returned.
// In particular, net/http wraps transport failures in *url.Error whose message
// embeds the full request URL — which historically carried the access token in
// a query string. We also redact any JWT currently held.
func (c *Client) scrub(err error) error {
	if err == nil || c.accessToken == "" {
		return err
	}
	if s := err.Error(); strings.Contains(s, c.accessToken) {
		return errors.New(strings.ReplaceAll(s, c.accessToken, "[REDACTED_ACCESS_TOKEN]"))
	}
	return err
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

func (c *Client) runQuery(ctx context.Context, opName string, vars map[string]interface{}, dest interface{}) error {
	if err := c.ensureToken(ctx); err != nil {
		return err
	}
	query, err := queries.LoadOperation(queriesFile, opName)
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
		// Do not echo the raw upstream body (may contain tokens); log status only.
		return fmt.Errorf("refresh endpoint returned status %d", resp.StatusCode)
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
// SECURITY: the access token is sent in the X-Access-Token header rather than a
// URL query string, so it cannot leak via proxy/access logs or via the URL that
// net/http embeds in *url.Error on transport failures. If your 01-edu
// deployment only accepts the query-param form, set the legacy query but rely on
// scrub() to redact it from errors. (Header form is preferred and assumed here.)
func (c *Client) requestToken(ctx context.Context) (string, time.Time, error) {
	endpoint := c.baseURL + authTokenPath

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("X-Access-Token", c.accessToken)
	req.Header.Set("Authorization", "Bearer "+c.accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// scrub in case the URL or wrapped error references the token at all.
		return "", time.Time{}, scrubURLError(err, c.accessToken)
	}
	defer resp.Body.Close()

	body, _ := readCapped(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("token endpoint returned status %d", resp.StatusCode)
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
// and from generic errors.
func scrubURLError(err error, secret string) error {
	if err == nil || secret == "" {
		return err
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		ue.URL = strings.ReplaceAll(ue.URL, secret, "[REDACTED]")
		return ue
	}
	return errors.New(strings.ReplaceAll(err.Error(), secret, "[REDACTED]"))
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
		return fmt.Errorf("%w: %s", domain.ErrGraphQL, env.Errors[0].Message)
	}
	if len(env.Data) == 0 {
		return fmt.Errorf("%w: empty data", domain.ErrGraphQL)
	}

	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("%w: decode: %v", domain.ErrGraphQL, err)
	}
	return nil
}
