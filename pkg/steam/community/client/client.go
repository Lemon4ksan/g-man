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
	"github.com/lemon4ksan/aoni/request"
	"github.com/lemon4ksan/miyako/log"

	"github.com/lemon4ksan/g-man/pkg/steam/service"
)

// BaseURL root base URL for Steam Community endpoints.
const BaseURL = "https://steamcommunity.com/"

var (
	rxSorry      = regexp.MustCompile(`<h1>Sorry!</h1>[\s\S]*?<h3>(.+?)</h3>`)
	rxTradeError = regexp.MustCompile(`<div id="error_msg">\s*([^<]+)\s*</div>`)

	apiKeyRegexes = []*regexp.Regexp{
		regexp.MustCompile(`(?i)Key:\s*([0-9A-F]{32})`),
		regexp.MustCompile(`(?i)id=["']apiKey["']\s+value=["']([0-9A-F]{32})["']`),
		regexp.MustCompile(`(?i)value=["']([0-9A-F]{32})["']`),
	}
)

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

	peekBuf := request.ResolvePeekableReader(resp)
	resp.Body = io.NopCloser(peekBuf)

	peekBytes, peekErr := peekBuf.Peek(4096)
	if peekErr != nil && !errors.Is(peekErr, io.EOF) {
		return peekErr
	}

	return CheckSteamErrors(resp.StatusCode, resp.Header, peekBytes)
}

// Requester executes HTTP requests against steamcommunity.com with session state awareness.
type Requester interface {
	request.Requester
	SessionID(baseURL string) string
	GetOrRegisterAPIKey(ctx context.Context, domain string) (string, error)
}

// SessionProvider retrieves active Steam session identifiers.
type SessionProvider interface {
	SessionID(baseURL string) string
}

// Client executes HTTP requests against the Steam Community website.
type Client struct {
	r       request.Requester
	session SessionProvider
	logger  log.Logger
}

// New constructs a Client configured for steamcommunity.com.
func New(doer aoni.RequestDoer, session SessionProvider) *Client {
	r := request.AsRequester(aoni.Configure(doer,
		option.WithBaseURL(BaseURL),
		option.WithOrigin(BaseURL),
	))

	return &Client{
		r:       r,
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
		r:       request.AsRequester(aoni.Configure(c.r, opts...)),
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
func (c *Client) WithREST(r request.Requester) *Client {
	copy := *c
	copy.r = r

	return &copy
}

// Unwrap returns the underlying request.Requester.
func (c *Client) Unwrap() request.Requester {
	return c.r
}

// SessionID retrieves the sessionid cookie for targetURI.
func (c *Client) SessionID(targetURI string) string {
	if c.session == nil || targetURI == "" {
		return ""
	}

	return c.session.SessionID(targetURI)
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
			c.logger.Warn("Session expired during redirect loop, triggering auto-refresh")
			return nil, ErrRedirectLoop
		}

		return nil, err
	}

	if err := SteamErrorsValidator(resp); err != nil {
		if IsSessionExpiredError(err) {
			c.logger.Warn("Session expired during redirect loop, triggering auto-refresh")
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

	for _, re := range apiKeyRegexes {
		if matches := re.FindStringSubmatch(bodyStr); len(matches) > 1 {
			return matches[1], nil
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
	patternSteamIDFalse = []byte("g_steamID = false;")
	patternSteamIDZero  = []byte(`g_steamID = "0";`)
	patternSignInTitle  = []byte("<title>Sign In</title>")
)

// IsSessionExpiredError reports whether err indicates an expired Steam web session.
func IsSessionExpiredError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, service.ErrSessionExpired) {
		return true
	}

	msg := strings.ToLower(err.Error())

	return strings.Contains(msg, "session expired") || strings.Contains(msg, "redirect")
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
		if strings.Contains(loc, "steam") && strings.Contains(loc, "/login") {
			return service.NewSteamAPIError("Session expired", statusCode, service.ErrSessionExpired)
		}
	}

	if statusCode == http.StatusForbidden && bytes.Contains(body, []byte("parental_notice_instructions")) {
		return service.NewSteamAPIError("Family View enabled", statusCode, ErrFamilyViewRestricted)
	}

	if bytes.Contains(body, patternSteamIDFalse) ||
		bytes.Contains(body, patternSteamIDZero) ||
		bytes.Contains(body, patternSignInTitle) {
		return service.NewSteamAPIError("Session expired", statusCode, service.ErrSessionExpired)
	}

	if bytes.Contains(body, []byte("<h1>Sorry!</h1>")) {
		if matches := rxSorry.FindSubmatch(body); len(matches) > 1 {
			return service.NewSteamAPIError(string(bytes.TrimSpace(matches[1])), statusCode, nil)
		}

		return service.NewSteamAPIError("unknown steam community error (Sorry page)", statusCode, nil)
	}

	if bytes.Contains(body, []byte("error_msg")) {
		if matches := rxTradeError.FindSubmatch(body); len(matches) > 1 {
			return service.NewSteamAPIError(string(bytes.TrimSpace(matches[1])), statusCode, nil)
		}
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
