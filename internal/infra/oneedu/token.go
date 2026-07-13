package oneedu

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"admin-bot/internal/domain"
)

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
