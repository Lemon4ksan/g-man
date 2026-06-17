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
	"github.com/lemon4ksan/aoni"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/encoding"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
)

var (
	// ErrFamilyViewRestricted is returned when the account is under Family View restrictions.
	ErrFamilyViewRestricted = service.ErrFamilyViewRestricted
	// ErrRateLimited is returned when the account is rate limited.
	ErrRateLimited = service.ErrRateLimited
)

// Requester defines the requirements for making Community requests.
// It embeds [aoni.Requester] and adds Steam session management.
type Requester interface {
	aoni.Requester
	// SessionID returns the current Steam session identifier for the given base URL.
	SessionID(baseURL string) string
}

// BaseURL is the base url for community requests.
const BaseURL = "https://steamcommunity.com/"

var (
	rxFamilyView = regexp.MustCompile(
		`<div id="parental_notice_instructions">Enter your PIN below to exit Family View\.</div>`,
	)
	rxSorry      = regexp.MustCompile(`<h1>Sorry!</h1>[\s\S]*?<h3>(.+?)</h3>`)
	rxTradeError = regexp.MustCompile(`<div id="error_msg">\s*([^<]+)\s*</div>`)
	rxAPIKey     = regexp.MustCompile(`Key: (?i)[0-9A-F]{32}`)
)

// ErrAPITokenNotFound is returned when automatic key registration fails.
var ErrAPITokenNotFound = errors.New(
	"community: could not find api key or registration form (account might be limited)",
)

// Decorate wraps an existing Requester and adds global request modifiers.
func Decorate(r Requester, mods ...aoni.RequestModifier) Requester {
	if len(mods) == 0 {
		return r
	}

	return &decoratedRequester{
		Requester:   r,
		defaultMods: mods,
	}
}

// Option defines a functional configuration for the Client.
type Option = bus.Option[*Client]

// WithLogger sets a custom logger for the client.
func WithLogger(l log.Logger) Option {
	return func(c *Client) {
		c.logger = l.With(log.Module("community"))
	}
}

// WithREST sets a custom rest client for performing requests.
func WithREST(r aoni.Requester) Option {
	return func(c *Client) {
		c.restClient = r
	}
}

// Client handles communication with Steam Community, backed by a generic REST client.
//
// It wraps a [aoni.Client] and automatically configures default headers such as
// Origin and Referer, which are required by Steam. Use [NewClient] to construct
// new instances of the client.
type Client struct {
	restClient  aoni.Requester
	sessionFunc func(string) string
	logger      log.Logger
}

// NewClient creates a new Community Client.
// It initializes a [aoni.Client] with the required default browser-like headers.
func NewClient(httpClient aoni.HTTPDoer, sessionFunc func(string) string, opts ...Option) *Client {
	rc := aoni.NewClient(httpClient).
		WithBaseURL(BaseURL).
		WithOrigin(BaseURL).
		WithMultiReadBody(10 * 1024 * 1024)

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

// Unwrap returns the underlying [aoni.Requester] if this is a wrapped client.
func (c *Client) Unwrap() aoni.Requester {
	return c.restClient
}

// SessionID retrieves the session identifier for the specified URI.
func (c *Client) SessionID(targetURI string) string {
	if c.sessionFunc == nil {
		return ""
	}

	return c.sessionFunc(targetURI)
}

// Request implements [aoni.Requester]. It executes the HTTP request and deeply
// inspects the response for Steam-specific soft errors.
//
// If an authentication failure, rate limit, family view, or generic Steam "Sorry!" error
// page is detected, Request intercepts the response and returns an [api.SteamAPIError].
func (c *Client) Request(
	ctx context.Context,
	method, path string,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	c.logger.Debug("Community Request", log.String("method", method), log.String("path", path))

	resp, err := c.restClient.Request(ctx, method, path, mods...)
	if err != nil {
		return nil, err
	}

	var mBody interface{ Reset() }

	curr := io.Closer(resp.Body)
	for {
		if rb, ok := curr.(interface{ Reset() }); ok {
			mBody = rb
			break
		}

		u, ok := curr.(interface{ Unwrap() io.Closer })
		if !ok {
			break
		}

		curr = u.Unwrap()
	}

	var rawBody []byte
	if mBody != nil {
		limitReader := io.LimitReader(resp.Body, 100*1024)

		rawBody, err = io.ReadAll(limitReader)
		if err != nil {
			_ = resp.Body.Close()
			return nil, err
		}

		mBody.Reset()
	} else {
		rawBody, err = io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if err != nil {
			return nil, err
		}

		resp.Body = io.NopCloser(bytes.NewReader(rawBody))
	}

	if err := checkSteamErrors(resp.StatusCode, resp.Header, rawBody); err != nil {
		_ = resp.Body.Close()
		return resp, err
	}

	return resp, nil
}

// GetOrRegisterAPIKey checks for the presence of a WebAPI key on the account.
// If no key exists, it registers a new one for the specified domain.
//
// If the domain is empty, it defaults to "localhost".
// It returns [ErrAPITokenNotFound] if a key cannot be found or registered.
func (c *Client) GetOrRegisterAPIKey(ctx context.Context, domain string) (string, error) {
	if domain == "" {
		domain = "localhost"
	}

	htmlStream, err := GetHTML(ctx, c, "dev/apikey")
	if err != nil {
		return "", fmt.Errorf("failed to fetch apikey page: %w", err)
	}
	defer htmlStream.Close()

	body := aoni.AsReplayable(htmlStream)

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, body); err != nil {
		return "", err
	}

	apiKey := rxAPIKey.FindString(buf.String())
	if apiKey != "" {
		return apiKey[5:], nil
	}

	body.Reset()

	doc, err := goquery.NewDocumentFromReader(body)
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
		aoni.WithBody(strings.NewReader(formData.Encode())),
		aoni.WithContentType("application/x-www-form-urlencoded"),
	)
	if err != nil {
		return "", fmt.Errorf("registration request failed: %w", err)
	}
	defer resp.Body.Close()

	return c.GetOrRegisterAPIKey(ctx, domain)
}

// Get performs a GET request and unmarshals the resulting JSON into the Resp type.
//
// If the reqMsg argument is nil, query parameters are omitted.
func Get[Resp any](
	ctx context.Context,
	r Requester,
	path string,
	reqMsg any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	if reqMsg != nil {
		mods = append([]aoni.RequestModifier{aoni.WithQuery(reqMsg)}, mods...)
	}

	mods = append([]aoni.RequestModifier{
		aoni.WithHeader("Accept", "application/json, text/javascript; q=0.01"),
		aoni.WithHeader("X-Requested-With", "XMLHttpRequest"),
	}, mods...)

	return execute[Resp](ctx, r, http.MethodGet, path, mods...)
}

// GetHTML performs a GET request specifically for raw HTML content.
//
// If the reqMsg argument is nil, query parameters are omitted.
func GetHTML(ctx context.Context, r Requester, path string, mods ...aoni.RequestModifier) (io.ReadCloser, error) {
	mods = append([]aoni.RequestModifier{
		aoni.WithHeader(
			"Accept",
			"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
		),
	}, mods...)

	resp, err := r.Request(ctx, http.MethodGet, path, mods...)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return resp.Body, nil
}

// PostForm performs a POST request with application/x-www-form-urlencoded data.
// It automatically injects the "sessionid" into the form parameters.
//
// If the reqMsg argument is nil, form parameters are initialized containing only the session ID.
func PostForm[Resp any](
	ctx context.Context,
	r Requester,
	path string,
	reqMsg any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	var params url.Values

	if reqMsg != nil {
		var err error

		params, err = aoni.StructToValues(reqMsg)
		if err != nil {
			return nil, err
		}
	} else {
		params = make(url.Values)
	}

	if params.Get("sessionid") == "" {
		params.Set("sessionid", r.SessionID(BaseURL))
	}

	mods = append([]aoni.RequestModifier{
		aoni.WithBody(strings.NewReader(params.Encode())),
		aoni.WithContentType("application/x-www-form-urlencoded; charset=UTF-8"),
		aoni.WithHeader("Accept", "application/json, text/javascript; q=0.01"),
	}, mods...)

	return execute[Resp](ctx, r, http.MethodPost, path, mods...)
}

// PostJSON performs a POST request with a JSON body.
// It automatically injects the "sessionid" into the URL query parameters.
//
// If the reqMsg argument is nil, the request payload is omitted.
func PostJSON[Resp any](
	ctx context.Context,
	r Requester,
	path string,
	reqMsg any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	var query url.Values
	if sid := r.SessionID(BaseURL); sid != "" {
		query = url.Values{"sessionid": {sid}}
	}

	if len(query) > 0 {
		mods = append([]aoni.RequestModifier{aoni.WithQuery(query)}, mods...)
	}

	if reqMsg != nil {
		bodyBytes, err := json.Marshal(reqMsg)
		if err != nil {
			return nil, err
		}

		mods = append([]aoni.RequestModifier{aoni.WithBody(bytes.NewReader(bodyBytes))}, mods...)
	}

	mods = append([]aoni.RequestModifier{
		aoni.WithContentType("application/json; charset=UTF-8"),
		aoni.WithHeader("Accept", "application/json"),
	}, mods...)

	return execute[Resp](ctx, r, http.MethodPost, path, mods...)
}

func execute[Resp any](
	ctx context.Context,
	r Requester,
	method, path string,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	resp, err := r.Request(ctx, method, path, mods...)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var zero Resp
	if _, ok := any(&zero).(*[]byte); ok {
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}

		p := any(&raw).(*Resp)

		return p, nil
	}

	result := new(Resp)
	if err := encoding.SteamJSONDecoder.Decode(resp.Body, result); err != nil {
		return nil, err
	}

	return result, nil
}

func checkSteamErrors(statusCode int, header http.Header, body []byte) error {
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
	if statusCode == http.StatusForbidden && rxFamilyView.Match(body) {
		return service.NewSteamAPIError("Family View enabled", statusCode, service.ErrFamilyViewRestricted)
	}

	// Soft Auth Failure (Page loaded but user is guest)
	if bytes.Contains(body, []byte("g_steamID = false;")) ||
		bytes.Contains(body, []byte(`g_steamID = "0";`)) ||
		bytes.Contains(body, []byte("<title>Sign In</title>")) {
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
		return service.NewSteamAPIError(string(body), statusCode, nil)
	}

	return nil
}

type decoratedRequester struct {
	Requester
	defaultMods []aoni.RequestModifier
}

func (d *decoratedRequester) Request(
	ctx context.Context,
	method, path string,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	allMods := make([]aoni.RequestModifier, 0, len(d.defaultMods)+len(mods))
	allMods = append(allMods, d.defaultMods...)
	allMods = append(allMods, mods...)

	return d.Requester.Request(ctx, method, path, allMods...)
}
