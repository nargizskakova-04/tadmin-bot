package oneedu

import (
	"errors"
	"io"
	"net/url"
	"strings"
)

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
