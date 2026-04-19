// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package community

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/rest"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
)

var (
	// ErrFamilyViewRestricted indicates the account is currently in Family View mode.
	ErrFamilyViewRestricted = errors.New("steam community: family view restricted")

	// ErrRateLimited indicates Steam is blocking requests due to high frequency.
	ErrRateLimited = errors.New("steam community: rate limit exceeded")
)

// Requester defines the requirements for making Community requests.
// It embeds rest.Requester and adds Steam session management.
type Requester interface {
	rest.Requester
	// SessionID returns the current Steam session identifier for the given base URL.
	SessionID(baseURL string) string
}

// BaseURL is the base url for community requests.
const BaseURL = "https://steamcommunity.com/"

var (
	rxFamilyView = regexp.MustCompile(
		`<div id="parental_notice_instructions">Enter your PIN below to exit Family View\.<\/div>`,
	)
	rxSorry      = regexp.MustCompile(`<h1>Sorry!<\/h1>[\s\S]*?<h3>(.+?)<\/h3>`)
	rxTradeError = regexp.MustCompile(`<div id="error_msg">\s*([^<]+)\s*<\/div>`)
	rxApiKey     = regexp.MustCompile(`Key: (?i)[0-9A-F]{32}`)
)

// ErrAPITokenNotFound is returned when automatic key registration fails.
var ErrAPITokenNotFound = errors.New(
	"community: could not find api key or registration form (account might be limited)",
)

// Client handles communication with Steam Community, backed by a generic REST client.
type Client struct {
	restClient  rest.Requester
	sessionFunc func(string) string
	logger      log.Logger
}

// WithLogger sets a custom logger for the client.
func WithLogger(l log.Logger) bus.Option[*Client] {
	return func(c *Client) {
		c.logger = l.With(log.Module("community"))
	}
}

// WithRest sets a custom rest client for performing requests.
func WithRest(r *rest.Client) bus.Option[*Client] {
	return func(c *Client) {
		c.restClient = r
	}
}

// New creates a new Community Client.
// It initializes a rest.Client with the required default browser-like headers.
func New(httpClient rest.HTTPDoer, sessionFunc func(string) string, opts ...bus.Option[*Client]) *Client {
	rc := rest.NewClient(httpClient).
		WithBaseURL(BaseURL).
		WithHeader("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36").
		WithHeader("Origin", BaseURL)

	c := &Client{
		restClient:  rc,
		sessionFunc: sessionFunc,
		logger:      log.Discard,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// SessionID retrieves the session identifier for the specified URI.
func (c *Client) SessionID(targetURI string) string {
	if c.sessionFunc == nil {
		return ""
	}

	return c.sessionFunc(targetURI)
}

// Request implements rest.Requester. It executes the HTTP request and deeply
// inspects the response for Steam-specific soft errors (like HTML redirects).
func (c *Client) Request(
	ctx context.Context,
	method, path string,
	body []byte,
	query any,
	mods ...rest.RequestModifier,
) (*http.Response, error) {
	c.logger.Debug("Community Request", log.String("method", method), log.String("path", path))

	resp, err := c.restClient.Request(ctx, method, path, body, query, mods...)
	if err != nil {
		return nil, err
	}

	// We must read the body to check for HTML errors like "Sorry!" or Family View.
	rawBody, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if err != nil {
		return nil, fmt.Errorf("failed to read community response: %w", err)
	}

	// Reconstruct the body so the caller (or UnmarshalResponse) can read it later
	resp.Body = io.NopCloser(bytes.NewReader(rawBody))

	// Catch soft Steam errors
	if err := checkSteamErrors(resp.StatusCode, resp.Header, rawBody); err != nil {
		return resp, err
	}

	return resp, nil
}

// GetOrRegisterAPIKey checks for the presence of a WebAPI key on the account.
// If no key exists, it registers a new one for the specified domain (default: localhost).
func (c *Client) GetOrRegisterAPIKey(ctx context.Context, domain string) (string, error) {
	if domain == "" {
		domain = "localhost"
	}

	body, err := GetHTML(ctx, c, "dev/apikey")
	if err != nil {
		return "", fmt.Errorf("failed to fetch apikey page: %w", err)
	}

	key := rxApiKey.FindString(string(body))
	if key != "" {
		return key[5:], nil
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return "", err
	}

	if doc.Find("#register_form").Length() > 0 {
		return c.registerAPIKey(ctx, domain)
	}

	return "", ErrAPITokenNotFound
}

func (c *Client) registerAPIKey(ctx context.Context, domain string) (string, error) {
	c.logger.Info("Registering new WebAPI key...", log.String("domain", domain))

	formData := url.Values{
		"domain":       {domain},
		"agreeToTerms": {"agreed"},
		"Submit":       {"Register"},
		"sessionid":    {c.SessionID(BaseURL)},
	}

	resp, err := c.restClient.Request(
		ctx,
		http.MethodPost,
		"dev/registerkey",
		[]byte(formData.Encode()),
		nil,
		func(req *http.Request) {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		},
	)
	if err != nil {
		return "", fmt.Errorf("registration request failed: %w", err)
	}
	defer resp.Body.Close()

	return c.GetOrRegisterAPIKey(ctx, domain)
}

// Get performs a GET request and unmarshals the resulting JSON into the Resp type.
func Get[Resp any](ctx context.Context, r Requester, path string, reqMsg any, opts ...api.CallOption) (*Resp, error) {
	var query url.Values

	if reqMsg != nil {
		var err error

		query, err = rest.StructToValues(reqMsg)
		if err != nil {
			return nil, err
		}
	}

	myOpts := append([]api.CallOption{
		api.WithHeader("Accept", "application/json, text/javascript; q=0.01"),
		api.WithHeader("X-Requested-With", "XMLHttpRequest"),
	}, opts...)

	return execute[Resp](ctx, r, http.MethodGet, path, nil, query, myOpts...)
}

// GetHTML performs a GET request specifically for raw HTML content.
func GetHTML(ctx context.Context, r Requester, path string, opts ...api.CallOption) ([]byte, error) {
	myOpts := append([]api.CallOption{
		api.WithHeader(
			"Accept",
			"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
		),
	}, opts...)

	resp, _, err := performRequest(ctx, r, http.MethodGet, path, nil, nil, myOpts...)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

// PostForm performs a POST request with application/x-www-form-urlencoded data.
// It automatically injects the "sessionid" into the form parameters.
func PostForm[Resp any](
	ctx context.Context,
	r Requester,
	path string,
	reqMsg any,
	opts ...api.CallOption,
) (*Resp, error) {
	var params url.Values

	if reqMsg != nil {
		var err error

		params, err = rest.StructToValues(reqMsg)
		if err != nil {
			return nil, err
		}
	} else {
		params = make(url.Values)
	}

	if params.Get("sessionid") == "" {
		params.Set("sessionid", r.SessionID(BaseURL))
	}

	myOpts := append([]api.CallOption{
		api.WithHeader("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8"),
		api.WithHeader("Accept", "application/json, text/javascript; q=0.01"),
	}, opts...)

	return execute[Resp](ctx, r, http.MethodPost, path, []byte(params.Encode()), nil, myOpts...)
}

// PostJSON performs a POST request with a JSON body.
// It automatically injects the "sessionid" into the URL query parameters.
func PostJSON[Resp any](
	ctx context.Context,
	r Requester,
	path string,
	reqMsg any,
	opts ...api.CallOption,
) (*Resp, error) {
	var body []byte

	if reqMsg != nil {
		var err error

		body, err = json.Marshal(reqMsg)
		if err != nil {
			return nil, err
		}
	}

	var query url.Values
	if sid := r.SessionID(BaseURL); sid != "" {
		query = url.Values{"sessionid": {sid}}
	}

	myOpts := append([]api.CallOption{
		api.WithHeader("Content-Type", "application/json; charset=UTF-8"),
		api.WithHeader("Accept", "application/json"),
	}, opts...)

	return execute[Resp](ctx, r, http.MethodPost, path, body, query, myOpts...)
}

func performRequest(
	ctx context.Context,
	r Requester,
	method, path string,
	body []byte,
	query url.Values,
	opts ...api.CallOption,
) (*http.Response, *api.CallConfig, error) {
	trReq := api.NewHttpRequest(method, BaseURL+path, body)
	if query != nil {
		trReq.WithParams(query)
	}

	cfg := &api.CallConfig{Format: api.FormatJSON}

	for _, opt := range opts {
		opt(trReq, cfg)
	}

	modifier := func(req *http.Request) {
		for k, vv := range trReq.Header() {
			for _, v := range vv {
				req.Header.Add(k, v)
			}
		}
	}

	resp, err := r.Request(ctx, method, path, body, trReq.Params(), modifier)

	return resp, cfg, err
}

func execute[Resp any](
	ctx context.Context,
	r Requester,
	method, path string,
	body []byte,
	query url.Values,
	opts ...api.CallOption,
) (*Resp, error) {
	resp, cfg, err := performRequest(ctx, r, method, path, body, query, opts...)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("community: failed to read response: %w", err)
	}

	result := new(Resp)
	if err := api.UnmarshalResponse(rawBody, result, cfg.Format); err != nil {
		return nil, err
	}

	return result, nil
}

// checkSteamErrors scrapes the response body and headers to detect
// authentication failures, rate limits, or parental blocks.
func checkSteamErrors(statusCode int, header http.Header, body []byte) error {
	if statusCode == http.StatusTooManyRequests {
		return ErrRateLimited
	}

	if statusCode >= http.StatusInternalServerError {
		return fmt.Errorf("steam server error: %d", statusCode)
	}

	// Auth Redirects (302 to login page)
	if statusCode == http.StatusFound || statusCode == http.StatusSeeOther {
		loc := header.Get("Location")
		if strings.Contains(loc, "steam") && strings.Contains(loc, "/login") {
			return api.ErrSessionExpired
		}
	}

	// Parental Control (Family View)
	if statusCode == http.StatusForbidden && rxFamilyView.Match(body) {
		return ErrFamilyViewRestricted
	}

	// Soft Auth Failure (Page loaded but user is guest)
	if bytes.Contains(body, []byte("g_steamID = false;")) ||
		bytes.Contains(body, []byte(`g_steamID = "0";`)) ||
		bytes.Contains(body, []byte("<title>Sign In</title>")) {
		return api.ErrSessionExpired
	}

	// Generic Steam Error Pages ("Sorry!")
	if bytes.Contains(body, []byte("<h1>Sorry!</h1>")) {
		if matches := rxSorry.FindSubmatch(body); len(matches) > 1 {
			return fmt.Errorf("steam community error: %s", bytes.TrimSpace(matches[1]))
		}

		return errors.New("unknown steam community error (Sorry page)")
	}

	// Embedded Trade Errors
	if bytes.Contains(body, []byte("error_msg")) {
		if matches := rxTradeError.FindSubmatch(body); len(matches) > 1 {
			return fmt.Errorf("trade error: %s", bytes.TrimSpace(matches[1]))
		}
	}

	// Fallback to generic REST API error if status is bad but no Steam error matched
	if statusCode >= http.StatusBadRequest {
		return &rest.APIError{StatusCode: statusCode, Body: body}
	}

	return nil
}
