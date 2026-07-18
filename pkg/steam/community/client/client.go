// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package client provides a client for making HTTP requests to the Steam Community website.
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

	"github.com/PuerkitoBio/goquery"
	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/miyako/generic"
	"github.com/lemon4ksan/miyako/log"

	"github.com/lemon4ksan/g-man/pkg/steam/service"
)

// BaseURL is the default root URL for the Steam Community website.
const BaseURL = "https://steamcommunity.com/"

var (
	rxSorry      = regexp.MustCompile(`<h1>Sorry!</h1>[\s\S]*?<h3>(.+?)</h3>`)
	rxTradeError = regexp.MustCompile(`<div id="error_msg">\s*([^<]+)\s*</div>`)
	rxAPIKey     = regexp.MustCompile(`Key: (?i)[0-9A-F]{32}`)
)

var (
	// ErrFamilyViewRestricted is returned when a Steam Community request is blocked by Family View parental controls.
	ErrFamilyViewRestricted = errors.New("community: family view restricted")
	// ErrRateLimited is returned when the Steam Community server rate limits the client request.
	ErrRateLimited = service.ErrRateLimited
	// ErrAPITokenNotFound is returned when automatic retrieval or registration of the Steam WebAPI key fails.
	ErrAPITokenNotFound = errors.New(
		"community: could not find api key or registration form (account might be limited)",
	)
	// ErrRedirectLoop is returned when the client detects an infinite HTTP redirect loop caused by an expired session.
	ErrRedirectLoop = service.NewSteamAPIError(
		"session expired during redirect loop",
		http.StatusFound,
		service.ErrSessionExpired,
	)
)

// SteamErrorsValidator is a steam response validation function that can be passed to [aoni.WithResponseValidator].
func SteamErrorsValidator(resp *http.Response) error {
	contentType := resp.Header.Get("Content-Type")

	if contentType != "" &&
		!strings.Contains(contentType, "html") &&
		!strings.Contains(contentType, "json") &&
		!strings.Contains(contentType, "javascript") {
		return CheckSteamErrors(resp.StatusCode, resp.Header, nil)
	}

	replayable := aoni.AsReplayable(resp.Body)
	resp.Body = replayable

	body, err := io.ReadAll(io.LimitReader(replayable, 100*1024))
	if err != nil {
		return err
	}

	replayable.Reset()

	return CheckSteamErrors(resp.StatusCode, resp.Header, body)
}

// Requester defines the contract for executing Steam Community requests.
// It extends [aoni.Requester] by integrating session identifier tracking and Steam WebAPI key lifecycle management.
type Requester interface {
	aoni.Requester
	// SessionID retrieves the active Steam session identifier associated with the given base URL.
	// It returns an empty string if no active session is found.
	SessionID(baseURL string) string
	// GetOrRegisterAPIKey retrieves the existing WebAPI key or requests a new one for the specified domain.
	// It returns [ErrAPITokenNotFound] if registration fails.
	GetOrRegisterAPIKey(ctx context.Context, domain string) (string, error)
}

// SessionProvider defines the interface for retrieving active Steam session identifiers.
// Implementations supply session tokens to authenticate outgoing requests.
type SessionProvider interface {
	// SessionID retrieves the session identifier for the specified base URL.
	// It returns an empty string if no session exists for the URL.
	SessionID(baseURL string) string
}

// Client executes HTTP requests against the Steam Community website.
// Use [New] to instantiate and initialize a ready-to-use client.
// The client relies on an internal [aoni.Requester] and an optional [SessionProvider] to manage authentication and state.
type Client struct {
	rest    aoni.Requester
	session SessionProvider
	logger  log.Logger
}

// New creates an initialized [Client] with browser-like headers.
// If the session provider is nil, the client executes requests as an unauthenticated guest.
// If the httpClient is nil, the constructor uses the default HTTP client configuration.
func New(doer aoni.HTTPDoer, session SessionProvider) *Client {
	rc := aoni.NewClient(doer,
		aoni.WithClientBaseURL(BaseURL),
		aoni.WithClientOrigin(BaseURL),
		aoni.WithClientMultiReadBody(10*1024*1024),
	)

	c := &Client{
		rest:    rc,
		session: session,
		logger:  log.Discard,
	}

	return c
}

// WithLogger returns a new [Client] with the logger set to the given logger.
func (c *Client) WithLogger(l log.Logger) *Client {
	copy := *c
	copy.logger = l.With(log.Module("community"))
	return &copy
}

// WithREST returns a new [Client] with the REST client set to the given client.
func (c *Client) WithREST(r aoni.Requester) *Client {
	copy := *c
	copy.rest = r
	return &copy
}

// Unwrap returns the underlying [aoni.Requester] wrapped by the [Client].
func (c *Client) Unwrap() aoni.Requester {
	return c.rest
}

// SessionID retrieves the active session identifier for the given target URI.
// It returns an empty string if targetURI is empty or if no [SessionProvider] is configured.
func (c *Client) SessionID(targetURI string) string {
	if c.session == nil || targetURI == "" {
		return ""
	}

	return c.session.SessionID(targetURI)
}

// Request executes an HTTP request and checks the response for Steam-specific soft errors.
// It returns [ErrRedirectLoop] if a login redirect loop is detected.
// It returns a [service.SteamAPIError] if an expired session, rate limit, or parental control block is encountered.
// The context parameter manages request cancellation, and nil modifiers in mods are ignored.
func (c *Client) Request(
	ctx context.Context,
	method, path string,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	c.logger.Debug("Community Request", log.String("method", method), log.String("path", path))

	mods = append([]aoni.RequestModifier{
		aoni.WithResponseValidator(SteamErrorsValidator),
	}, mods...)

	resp, err := c.rest.Request(ctx, method, path, mods...)
	if err != nil {
		if IsSessionExpiredError(err) {
			c.logger.Warn("Session expired during redirect loop, triggering auto-refresh")
			return nil, ErrRedirectLoop
		}

		return nil, err
	}

	// Fallback validation for mocked Requesters (like ServiceMock)
	// that do not natively execute response validators.
	if resp.Request != nil {
		if fn := aoni.GetResponseValidator(resp.Request.Context()); fn != nil {
			if validErr := fn(resp); validErr != nil {
				_ = resp.Body.Close()

				if IsSessionExpiredError(validErr) {
					c.logger.Warn("Session expired during redirect loop, triggering auto-refresh")
					return nil, ErrRedirectLoop
				}

				return nil, validErr
			}
		}
	}

	return resp, nil
}

// GetOrRegisterAPIKey checks for a Steam WebAPI key or registers one for the given domain.
// It defaults to registering for localhost if the domain argument is empty.
// It returns [ErrAPITokenNotFound] or underlying connection errors if registration fails.
func (c *Client) GetOrRegisterAPIKey(ctx context.Context, domain string) (string, error) {
	dataPtr, err := aoni.GetTo[[]byte](ctx, c.rest, "dev/apikey",
		aoni.WithRawDecoder(),
		aoni.WithAccept("text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8"),
	)
	if err != nil {
		return "", fmt.Errorf("failed to fetch apikey page: %w", err)
	}

	data := *dataPtr

	if apiKey := rxAPIKey.Find(data); apiKey != nil {
		return string(apiKey[5:]), nil
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(data))
	if err != nil {
		return "", err
	}

	if doc.Find("#register_form").Length() > 0 {
		return c.registerAPIKey(ctx, generic.Coalesce(domain, "localhost"))
	}

	return "", ErrAPITokenNotFound
}

// registerAPIKey registers a new WebAPI key for the specified domain.
func (c *Client) registerAPIKey(ctx context.Context, domain string) (string, error) {
	c.logger.Info("Registering new WebAPI key...", log.String("domain", domain))

	req := url.Values{
		"domain":       {domain},
		"agreeToTerms": {"agreed"},
		"Submit":       {"Register"},
		"sessionid":    {c.SessionID(BaseURL)},
	}

	_, err := aoni.PostTo[aoni.NoResponse](ctx, c.rest, "dev/registerkey", nil, aoni.WithFormValues(req))
	if err != nil {
		return "", fmt.Errorf("registration request failed: %w", err)
	}

	return c.GetOrRegisterAPIKey(ctx, domain)
}

var (
	patternSteamIDFalse = []byte("g_steamID = false;")
	patternSteamIDZero  = []byte(`g_steamID = "0";`)
	patternSignInTitle  = []byte("<title>Sign In</title>")
)

// IsSessionExpiredError reports whether the given error indicates an expired Steam session.
// It returns false if the error argument is nil.
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

// CheckSteamErrors inspects the HTTP response status, headers, and body for Steam-specific error conditions.
// It returns [service.ErrRateLimited] when the server responds with a rate limit status.
// It returns [ErrFamilyViewRestricted] if the account parental lock page is detected.
// It returns [service.ErrSessionExpired] if auth redirects or guest session indicators are found.
// It handles empty or nil body slices without panicking.
func CheckSteamErrors(statusCode int, header http.Header, body []byte) error {
	if statusCode == http.StatusTooManyRequests {
		return service.NewSteamAPIError("Rate limit exceeded", statusCode, service.ErrRateLimited)
	}

	if statusCode >= http.StatusInternalServerError {
		return service.NewSteamAPIError("Steam is down or in maintenance", statusCode, nil)
	}

	// Auth Redirects (302 to login page)
	if statusCode == http.StatusFound || statusCode == http.StatusSeeOther {
		loc := header.Get("Location")
		if strings.Contains(loc, "steam") && strings.Contains(loc, "/login") {
			return service.NewSteamAPIError("Session expired", statusCode, service.ErrSessionExpired)
		}
	}

	// Parental Control (Family View)
	if statusCode == http.StatusForbidden && bytes.Contains(body, []byte("parental_notice_instructions")) {
		return service.NewSteamAPIError("Family View enabled", statusCode, ErrFamilyViewRestricted)
	}

	// Soft Auth Failure (Page loaded but user is guest)
	if bytes.Contains(body, patternSteamIDFalse) ||
		bytes.Contains(body, patternSteamIDZero) ||
		bytes.Contains(body, patternSignInTitle) {
		return service.NewSteamAPIError("Session expired", statusCode, service.ErrSessionExpired)
	}

	// Generic Steam Error Pages ("Sorry!")
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

// TruncateBody returns a shortened string representation of the provided response body.
// It limits the output length to maxLen characters to avoid exposing excessive data in log traces.
func TruncateBody(body []byte, maxLen int) string {
	s := string(body)
	if len(s) > maxLen {
		return s[:maxLen] + "...[truncated]"
	}

	return s
}
