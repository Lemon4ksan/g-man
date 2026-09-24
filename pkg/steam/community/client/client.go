// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package client executes HTTP requests to steamcommunity.com and validates Steam response error states.
package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/codec/extract"
	log "github.com/lemon4ksan/foundation/async/logkit"

	"github.com/lemon4ksan/g-man/pkg/steam/auth/websession"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
)

// BaseURL root base URL for Steam Community endpoints.
const BaseURL = "https://steamcommunity.com/"

var (
	// ErrFamilyViewRestricted indicates an operation was blocked by Family View PIN controls.
	ErrFamilyViewRestricted = errors.New("community: family view restricted")
	// ErrRateLimited indicates request rate limits were exceeded on Steam Community servers.
	ErrRateLimited = service.ErrRateLimited
	// ErrAPITokenNotFound indicates automatic WebAPI key retrieval or registration failed.
	ErrAPITokenNotFound = errors.New(
		"community: could not find api key or registration form (account might be limited)",
	)
	// ErrRedirectLoop indicates an expired session caused a 302 redirect loop to the login page.
	ErrRedirectLoop = service.NewSteamAPIError(
		"session expired during redirect loop",
		http.StatusFound,
		service.ErrSessionExpired,
	)
	// ErrWebSessionUnauthenticated indicates dev/apikey access was rejected due to an unauthenticated web session.
	ErrWebSessionUnauthenticated = errors.New(
		"community: web session not authenticated when accessing dev/apikey (redirected to login)",
	)
	// ErrAccountLimited indicates WebAPI key registration failed because the account is limited.
	ErrAccountLimited = errors.New("community: account is limited ($5 USD required) and cannot create a WebAPI key")
	// ErrMissingSessionIDCookie indicates the sessionid cookie was missing when submitting key registration.
	ErrMissingSessionIDCookie = errors.New("community: missing sessionid cookie for registerkey request")
)

// SteamErrorsValidator validates HTTP response streams for soft Steam Community error HTML payloads.
func SteamErrorsValidator(resp *http.Response) error {
	if resp == nil || resp.Body == nil {
		return nil
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "" &&
		!strings.Contains(contentType, "html") &&
		!strings.Contains(contentType, "json") &&
		!strings.Contains(contentType, "javascript") {
		return CheckSteamErrors(resp.StatusCode, resp.Header, nil)
	}

	peekBuf := aoni.ResolvePeekableReader(resp)
	resp.Body = io.NopCloser(peekBuf)

	peekBytes, peekErr := peekBuf.Peek(4096)
	if peekErr != nil && !errors.Is(peekErr, io.EOF) {
		return peekErr
	}

	return CheckSteamErrors(resp.StatusCode, resp.Header, peekBytes)
}

// Requester executes HTTP requests against steamcommunity.com with session state awareness.
type Requester interface {
	aoni.HTTPRequester
	SessionID(baseURL string) string
	GetOrRegisterAPIKey(ctx context.Context, domain string) (string, error)
}

// SessionProvider retrieves active Steam session identifiers.
type SessionProvider interface {
	SessionID(baseURL string) string
}

// Client executes HTTP requests against the Steam Community website.
type Client struct {
	r       *aoni.Client
	session SessionProvider
	logger  log.Logger
}

// New constructs a Client configured for steamcommunity.com.
func New(doer aoni.RequestDoer, session SessionProvider) *Client {
	c := aoni.NewClient(doer,
		option.WithBaseURL(BaseURL),
		option.WithOrigin(BaseURL),
		option.WithBlockRedirectTo("/login/home", "/login"),
	)

	return &Client{
		r:       c,
		session: session,
		logger:  log.Discard,
	}
}

// With applies options and returns an updated client instance.
func (c *Client) With(opts ...aoni.ClientOption) *Client {
	if len(opts) == 0 {
		return c
	}

	return &Client{
		r:       c.r.With(opts...),
		session: c.session,
		logger:  c.logger,
	}
}

// WithLogger sets the logger instance.
func (c *Client) WithLogger(l log.Logger) *Client {
	copy := *c
	copy.logger = l.With(log.Module("community"))

	return &copy
}

// WithREST sets the underlying requester instance.
func (c *Client) WithREST(doer any) *Client {
	copy := *c
	if cl, ok := doer.(*aoni.Client); ok {
		copy.r = cl
	} else {
		copy.r = aoni.NewClient(doer)
	}

	return &copy
}

// Unwrap returns the underlying *aoni.Client.
func (c *Client) Unwrap() *aoni.Client {
	return c.r
}

// SessionID retrieves the sessionid cookie for targetURI.
func (c *Client) SessionID(targetURI string) string {
	if c.session == nil || targetURI == "" {
		return ""
	}

	return c.session.SessionID(targetURI)
}

// Refresher describes session providers capable of re-authenticating cookies when expired.
type Refresher interface {
	Refresh(ctx context.Context) error
}

// Request executes HTTP calls and validates response headers and body content for Steam errors.
func (c *Client) Request(
	ctx context.Context,
	method, path string,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	c.logger.Debug("Community Request", log.String("method", method), log.String("path", path))

	resp, err := c.r.Request(ctx, method, path, mods...)
	if err != nil {
		if IsSessionExpiredError(err) {
			c.logger.Warn("Session expired during redirect loop, attempting auto-refresh")

			if r, ok := c.session.(Refresher); ok {
				if rErr := r.Refresh(ctx); rErr == nil {
					c.logger.Info("Auto-refresh succeeded, retrying community request")

					if newSID := c.SessionID(BaseURL); newSID != "" {
						mods = UpdateSessionIDInMods(mods, newSID)
					}

					return c.r.Request(ctx, method, path, mods...)
				} else {
					return nil, fmt.Errorf("community: auto-refresh failed: %w", rErr)
				}
			}

			return nil, ErrRedirectLoop
		}

		return nil, err
	}

	if err := SteamErrorsValidator(resp); err != nil {
		if IsSessionExpiredError(err) {
			_ = resp.Body.Close()

			c.logger.Warn("Session expired, attempting auto-refresh")

			if r, ok := c.session.(Refresher); ok {
				if rErr := r.Refresh(ctx); rErr == nil {
					c.logger.Info("Auto-refresh succeeded, retrying community request")

					if newSID := c.SessionID(BaseURL); newSID != "" {
						mods = UpdateSessionIDInMods(mods, newSID)
					}

					retryResp, retryErr := c.r.Request(ctx, method, path, mods...)
					if retryErr != nil {
						return nil, retryErr
					}

					if valErr := SteamErrorsValidator(retryResp); valErr != nil {
						_ = retryResp.Body.Close()
						return nil, valErr
					}

					return retryResp, nil
				} else {
					return nil, fmt.Errorf("community: auto-refresh failed: %w", rErr)
				}
			}

			return nil, ErrRedirectLoop
		}

		return nil, err
	}

	return resp, nil
}

// GetOrRegisterAPIKey fetches an existing WebAPI key or submits a key registration form for domain.
func (c *Client) GetOrRegisterAPIKey(ctx context.Context, domain string) (string, error) {
	resp, err := c.Request(ctx, http.MethodGet, "dev/apikey")
	if err != nil {
		return "", fmt.Errorf("community: get apikey page: %w", err)
	}

	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("community: read apikey page: %w", err)
	}

	bodyStr := string(bodyBytes)

	if strings.Contains(bodyStr, "login_form") || strings.Contains(bodyStr, "/login/home") {
		return "", ErrWebSessionUnauthenticated
	}

	if strings.Contains(bodyStr, "Access Denied") || strings.Contains(bodyStr, "does not meet the requirements") {
		return "", ErrAccountLimited
	}

	if key, err := extract.AttrString(bodyBytes, "#apiKey", "value").Unwrap(); err == nil && len(key) == 32 {
		return key, nil
	}

	if key, err := extract.BetweenString(bodyBytes, "Key: ", "<").Unwrap(); err == nil {
		trimmed := strings.TrimSpace(key)
		if len(trimmed) == 32 {
			return trimmed, nil
		}
	}

	hasForm := strings.Contains(bodyStr, "registerkey") ||
		strings.Contains(bodyStr, "editForm") ||
		strings.Contains(bodyStr, "name=\"domain\"") ||
		strings.Contains(bodyStr, "name='domain'") ||
		strings.Contains(bodyStr, "register_form")

	if !hasForm {
		return "", ErrAPITokenNotFound
	}

	if domain == "" {
		domain = "localhost"
	}

	sessionID := c.SessionID(BaseURL)
	if sessionID == "" {
		return "", ErrMissingSessionIDCookie
	}

	form := url.Values{
		"domain":       {domain},
		"agreeToTerms": {"agreed"},
		"Submit":       {"Register"},
		"sessionid":    {sessionID},
	}

	regResp, err := c.Request(ctx, http.MethodPost, "dev/registerkey",
		mod.WithBody(strings.NewReader(form.Encode())),
		mod.WithContentType("application/x-www-form-urlencoded"),
	)
	if err != nil {
		return "", fmt.Errorf("community: register key submission failed: %w", err)
	}

	defer regResp.Body.Close()

	return c.GetOrRegisterAPIKey(ctx, domain)
}

var (
	patternSteamIDFalse            = []byte("g_steamID = false;")
	patternSteamIDZero             = []byte(`g_steamID = "0";`)
	patternSignInTitle             = []byte("<title>Sign In</title>")
	patternLoggedInFalse           = []byte(`"Logged In":false`)
	patternLoggedInFalseSpace      = []byte(`"Logged In": false`)
	patternLowerLoggedInFalse      = []byte(`"logged in":false`)
	patternLowerLoggedInFalseSpace = []byte(`"logged in": false`)

	rxStrError = regexp.MustCompile(`"strError"\s*:\s*"([^"]+)"`)
)

// UpdateSessionIDInMods replaces sessionid occurrences in request modifiers with newSID.
func UpdateSessionIDInMods(mods []aoni.RequestModifier, newSID string) []aoni.RequestModifier {
	if len(mods) == 0 || newSID == "" {
		return mods
	}

	updated := make([]aoni.RequestModifier, len(mods))
	copy(updated, mods)

	escapedSID := []byte(url.QueryEscape(newSID))
	rawSID := []byte(newSID)

	for i, m := range updated {
		if m.Key == "sessionid" {
			m.Value = newSID
			updated[i] = m
			continue
		}

		if len(m.Bytes) > 0 {
			b := m.Bytes
			// Check if payload looks like a JSON object
			if len(b) >= 2 && b[0] == '{' && b[len(b)-1] == '}' {
				// Fast in-place replacement for {"sessionid":"..."}
				key := []byte(`"sessionid":"`)
				if idx := bytes.Index(b, key); idx != -1 {
					start := idx + len(key)
					if end := bytes.IndexByte(b[start:], '"'); end != -1 {
						res := make([]byte, 0, start+len(rawSID)+(len(b)-(start+end)))
						res = append(res, b[:start]...)
						res = append(res, rawSID...)
						res = append(res, b[start+end:]...)
						m.Bytes = res
						updated[i] = m
					}
				}
			} else {
				// Fast in-place replacement for sessionid=... in x-www-form-urlencoded
				keyStr := []byte("sessionid=")
				idx := bytes.Index(b, keyStr)

				// Ensure it's either at the start or preceded by &
				if idx != -1 && (idx == 0 || b[idx-1] == '&') {
					start := idx + len(keyStr)
					end := bytes.IndexByte(b[start:], '&')

					if end == -1 {
						end = len(b) - start
					}

					res := make([]byte, 0, start+len(escapedSID)+(len(b)-(start+end)))
					res = append(res, b[:start]...)
					res = append(res, escapedSID...)
					res = append(res, b[start+end:]...)
					m.Bytes = res
					updated[i] = m
				}
			}
		}
	}

	return updated
}

// IsSessionExpiredError reports whether err indicates an expired Steam web session.
func IsSessionExpiredError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, ErrFamilyViewRestricted) {
		return false
	}

	if errors.Is(err, service.ErrSessionExpired) ||
		errors.Is(err, websession.ErrSessionExpiredRedirect) ||
		errors.Is(err, aoni.ErrRedirectBlocked) {
		return true
	}

	msg := strings.ToLower(err.Error())

	return strings.Contains(msg, "session expired") ||
		strings.Contains(msg, "redirected to login") ||
		strings.Contains(msg, "redirect blocked") ||
		strings.Contains(msg, "redirect") ||
		strings.Contains(msg, "401") ||
		strings.Contains(msg, "403") ||
		strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "forbidden")
}

// CheckSteamErrors inspects HTTP status codes and response bodies for Steam error markers.
func CheckSteamErrors(statusCode int, header http.Header, body []byte) error {
	if statusCode == http.StatusTooManyRequests {
		return service.NewSteamAPIError("Rate limit exceeded", statusCode, service.ErrRateLimited)
	}

	if statusCode >= http.StatusInternalServerError {
		return service.NewSteamAPIError("Steam is down or in maintenance", statusCode, nil)
	}

	if statusCode == http.StatusFound || statusCode == http.StatusSeeOther {
		loc := header.Get("Location")
		if strings.Contains(loc, "/login/home") || strings.Contains(loc, "/login") {
			return service.NewSteamAPIError("Session expired", statusCode, service.ErrSessionExpired)
		}
	}

	if statusCode == http.StatusForbidden && bytes.Contains(body, []byte("parental_notice_instructions")) {
		return service.NewSteamAPIError("Family View enabled", statusCode, ErrFamilyViewRestricted)
	}

	if matches := rxStrError.FindSubmatch(body); len(matches) > 1 {
		msg := string(matches[1])
		if res, ok := service.ParseEResultFromMessage(msg); ok {
			return service.NewSteamAPIError(msg, statusCode, service.NewEResultError(res, nil))
		}

		return service.NewSteamAPIError(msg, statusCode, nil)
	}

	if bytes.Contains(body, []byte("<h1>Sorry!</h1>")) {
		if msg, err := extract.Between(body, "<h3>", "</h3>"); err == nil {
			return service.NewSteamAPIError(string(bytes.TrimSpace(msg)), statusCode, nil)
		}

		return service.NewSteamAPIError("unknown steam community error (Sorry page)", statusCode, nil)
	}

	if bytes.Contains(body, []byte("error_msg")) {
		if msg, err := extract.Between(body, `<div id="error_msg">`, "</div>"); err == nil {
			return service.NewSteamAPIError(string(bytes.TrimSpace(msg)), statusCode, nil)
		}
	}

	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		return service.NewSteamAPIError(
			"Session expired (unauthorized/forbidden)",
			statusCode,
			service.ErrSessionExpired,
		)
	}

	if bytes.Contains(body, patternSteamIDFalse) ||
		bytes.Contains(body, patternSteamIDZero) ||
		bytes.Contains(body, patternSignInTitle) ||
		bytes.Contains(body, patternLoggedInFalse) ||
		bytes.Contains(body, patternLoggedInFalseSpace) ||
		bytes.Contains(body, patternLowerLoggedInFalse) ||
		bytes.Contains(body, patternLowerLoggedInFalseSpace) {
		return service.NewSteamAPIError("Session expired", statusCode, service.ErrSessionExpired)
	}

	if statusCode >= http.StatusBadRequest {
		return service.NewSteamAPIError(TruncateBody(body, 500), statusCode, nil)
	}

	return nil
}

// TruncateBody limits output text lengths for logging.
func TruncateBody(body []byte, maxLen int) string {
	s := string(body)
	if len(s) > maxLen {
		return s[:maxLen] + "...[truncated]"
	}

	return s
}
