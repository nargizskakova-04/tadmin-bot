package oneedu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
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

	regionUpdatesLookbackDays = 360
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

func (c *Client) tokenState() (token string, exp time.Time) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.jwtToken, c.jwtExp
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
