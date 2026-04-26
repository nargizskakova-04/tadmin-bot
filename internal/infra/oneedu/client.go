package oneedu

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"admin-bot/internal/domain"
	"admin-bot/internal/infra/oneedu/queries"
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
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		baseURL:     baseURL,
		accessToken: accessToken,
		logger:      logger,
	}
}

// RefreshToken obtains a new JWT from the 01-edu auth endpoint.
func (c *Client) RefreshToken(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	token, exp, err := c.requestToken(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", domain.ErrTokenRefresh, err)
	}
	c.jwtToken = token
	c.jwtExp = exp
	c.logger.Info("01-edu JWT refreshed", "expires", exp)
	return nil
}

// GetCurrentPiscineID fetches the active piscine event by name.
func (c *Client) GetCurrentPiscineID(ctx context.Context, piscine domain.PiscineType) (*domain.PiscineInfo, error) {
	if err := c.ensureToken(ctx); err != nil {
		return nil, err
	}

	query, err := queries.LoadOperation("raids.graphql", "GetCurrentPiscineId")
	if err != nil {
		return nil, fmt.Errorf("load query: %w", err)
	}

	vars := map[string]interface{}{
		"name": string(piscine),
	}

	var resp piscineResponse
	if err := c.doGraphQL(ctx, query, vars, &resp); err != nil {
		return nil, err
	}

	if len(resp.Data.Event) == 0 {
		c.logger.Warn("no active piscine found", "name", piscine)
		return nil, nil
	}

	ev := resp.Data.Event[0]
	return &domain.PiscineInfo{
		ID:      ev.ID,
		StartAt: ev.StartAt,
		EndAt:   ev.EndAt,
	}, nil
}

// GetRaidsByPiscineID fetches all raid events for a given piscine event ID.
// Uses the piscine-specific query (GetRaidsByPiscineGoId, etc.).
func (c *Client) GetRaidsByPiscineID(ctx context.Context, piscine domain.PiscineType, piscineEventID int) ([]domain.RaidInfo, error) {
	if err := c.ensureToken(ctx); err != nil {
		return nil, err
	}

	opName := domain.GetRaidQueryName(piscine)
	if opName == "" {
		return nil, fmt.Errorf("%w: %s", domain.ErrPiscineNotFound, piscine)
	}

	query, err := queries.LoadOperation("raids.graphql", opName)
	if err != nil {
		return nil, fmt.Errorf("load query %s: %w", opName, err)
	}

	vars := map[string]interface{}{
		"id": piscineEventID,
	}

	var resp raidsResponse
	if err := c.doGraphQL(ctx, query, vars, &resp); err != nil {
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
	if err := c.ensureToken(ctx); err != nil {
		return nil, err
	}

	query, err := queries.LoadOperation("raids.graphql", "GetRaidByName")
	if err != nil {
		return nil, fmt.Errorf("load query: %w", err)
	}

	vars := map[string]interface{}{
		"name":    name,
		"startAt": startAt,
	}

	var resp raidsResponse
	if err := c.doGraphQL(ctx, query, vars, &resp); err != nil {
		return nil, err
	}

	if len(resp.Data.Event) == 0 {
		return nil, nil
	}

	info := mapEventToRaidInfo("", resp.Data.Event[0])
	return &info, nil
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

func (c *Client) ensureToken(ctx context.Context) error {
	c.mu.RLock()
	valid := c.jwtToken != "" && time.Now().Before(c.jwtExp.Add(-1*time.Minute))
	c.mu.RUnlock()
	if valid {
		return nil
	}

	// Try refresh first if we have an existing token.
	c.mu.RLock()
	hasToken := c.jwtToken != ""
	c.mu.RUnlock()

	if hasToken {
		if err := c.refreshJWT(ctx); err != nil {
			c.logger.Warn("refresh failed, requesting new token", "err", err)
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/auth/refresh", nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-jwt-token", c.jwtToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("refresh endpoint returned %d: %s", resp.StatusCode, body)
	}

	token := strings.Trim(strings.TrimSpace(string(body)), "\"")
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

// parseJWTExpiry decodes the JWT payload and extracts the "exp" claim.
func parseJWTExpiry(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, fmt.Errorf("invalid JWT: expected 3 parts, got %d", len(parts))
	}

	// Base64url decode the payload (part[1]).
	payload := parts[1]
	// Add padding if needed.
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}
	// Replace url-safe characters.
	payload = strings.ReplaceAll(payload, "-", "+")
	payload = strings.ReplaceAll(payload, "_", "/")

	decoded, err := base64Decode(payload)
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

// base64Decode is a helper for standard base64 decoding.
func base64Decode(s string) ([]byte, error) {
	return io.ReadAll(
		base64.NewDecoder(base64.StdEncoding, strings.NewReader(s)),
	)
}

func (c *Client) requestToken(ctx context.Context) (string, time.Time, error) {
	url := fmt.Sprintf("%s/api/auth/token?token=%s", c.baseURL, c.accessToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", time.Time{}, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, body)
	}

	// Response is a raw JWT string (possibly JSON-quoted).
	token := strings.Trim(strings.TrimSpace(string(body)), "\"")
	if token == "" {
		return "", time.Time{}, fmt.Errorf("empty token in response")
	}

	exp, err := parseJWTExpiry(token)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("parse JWT: %w", err)
	}

	return token, exp, nil
}

func (c *Client) doGraphQL(ctx context.Context, query string, variables map[string]interface{}, dest interface{}) error {
	body, _ := json.Marshal(map[string]interface{}{
		"query":     query,
		"variables": variables,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/graphql-engine/v1/graphql", bytes.NewReader(body))
	if err != nil {
		return err
	}

	c.mu.RLock()
	token := c.jwtToken
	c.mu.RUnlock()

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", domain.ErrGraphQL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%w: status %d — %s", domain.ErrGraphQL, resp.StatusCode, respBody)
	}

	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("%w: decode: %v", domain.ErrGraphQL, err)
	}
	return nil
}
