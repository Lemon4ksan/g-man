// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPDoer is an interface for objects that can execute an [http.Request].
// It is satisfied by [http.Client].
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// DoerFunc is a function type that implements HTTPDoer.
type DoerFunc func(req *http.Request) (*http.Response, error)

// Do implements the HTTPDoer interface for DoerFunc.
func (f DoerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

// Middleware wraps an HTTPDoer with additional logic.
type Middleware func(next HTTPDoer) HTTPDoer

// Chain applies a series of middlewares to an HTTPDoer, returning the final HTTPDoer.
// The first middleware in the list will be the outermost one (called first).
//
// Example:
//
//	rotator, _ := rest.NewProxyRotator(cfg, proxy1, proxy2)
//	logMiddleware := rest.LoggingMiddleware(logger)
//
//	// Build a chain: Client -> Logging -> Proxy Rotator
//	httpClient := rest.Chain(rotator, logMiddleware)
//	client := rest.NewClient(httpClient)
func Chain(doer HTTPDoer, middlewares ...Middleware) HTTPDoer {
	for i := len(middlewares) - 1; i >= 0; i-- {
		doer = middlewares[i](doer)
	}

	return doer
}

// Requester defines the requirements for performing raw HTTP requests
// with path joining and query parameter encoding.
type Requester interface {
	Request(
		ctx context.Context,
		method, path string,
		body []byte,
		query any,
		mods ...RequestModifier,
	) (*http.Response, error)
}

// BaseResponseProvider is an optional interface that a Requester can implement
// to provide a BaseResponse wrapper for JSON requests.
type BaseResponseProvider interface {
	BaseResponse() BaseResponse
}

// RequestModifier is a function that can modify an *http.Request before it is sent.
// This is used for adding one-off headers, authentication tokens, or logging.
type RequestModifier func(req *http.Request)

// WithVar replaces a placeholder in the path (e.g., "{id}") with a value.
// The value is automatically URL-escaped.
func WithVar(key string, value any) RequestModifier {
	return func(req *http.Request) {
		placeholder := "{" + key + "}"
		escapedValue := url.PathEscape(fmt.Sprint(value))

		req.URL.Path = strings.ReplaceAll(req.URL.Path, placeholder, escapedValue)
		if req.URL.RawPath != "" {
			req.URL.RawPath = strings.ReplaceAll(req.URL.RawPath, placeholder, escapedValue)
		}
	}
}

// WithVars replaces multiple placeholders in the path.
// It accepts pairs of key-value arguments.
func WithVars(pairs ...any) RequestModifier {
	return func(req *http.Request) {
		if len(pairs)%2 != 0 {
			return
		}

		for i := 0; i < len(pairs); i += 2 {
			key := fmt.Sprint(pairs[i])
			value := fmt.Sprint(pairs[i+1])
			WithVar(key, value)(req)
		}
	}
}

// BaseResponse is an interface for response wrappers that include
// status information and a data payload.
//
// If a Client is configured with a BaseResponse provider, it will
// automatically unwrap the response and check for success.
type BaseResponse interface {
	// IsSuccess returns true if the response indicates a successful operation,
	// even if the HTTP status code is 200.
	IsSuccess() bool

	// Error returns an error if IsSuccess is false.
	Error() error

	// SetData provides a pointer where the data payload should be decoded.
	// This is called by the client before unmarshaling the JSON body.
	SetData(data any)
}

// Client is a concrete implementation of the Requester interface.
// It maintains a base URL and a set of default headers applied to every request.
type Client struct {
	http         HTTPDoer
	baseURL      *url.URL
	headers      http.Header
	baseResponse func() BaseResponse
}

// NewClient initializes a REST client.
// If httpClient is nil, a default http.Client with a 15-second timeout is used.
func NewClient(httpClient HTTPDoer) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}

	return &Client{
		http:    httpClient,
		baseURL: &url.URL{},
		headers: make(http.Header),
	}
}

// WithBaseResponse returns a new Client instance that uses the provided
// function to create a BaseResponse wrapper for every JSON request.
func (c *Client) WithBaseResponse(provider func() BaseResponse) *Client {
	newClient := &Client{
		http:         c.http,
		baseURL:      c.baseURL,
		headers:      c.headers.Clone(),
		baseResponse: provider,
	}

	return newClient
}

// WithBaseURL returns a new Client instance with the specified base URL.
// It ensures the base URL has exactly one trailing slash to make
// url.ResolveReference work correctly with relative paths.
func (c *Client) WithBaseURL(raw string) *Client {
	if raw == "" {
		return &Client{
			http:         c.http,
			baseURL:      &url.URL{},
			headers:      c.headers.Clone(),
			baseResponse: c.baseResponse,
		}
	}

	if !strings.HasSuffix(raw, "/") {
		raw += "/"
	}

	baseURL, _ := url.Parse(raw)

	return &Client{
		http:         c.http,
		baseURL:      baseURL,
		headers:      c.headers.Clone(),
		baseResponse: c.baseResponse,
	}
}

// WithHeader returns a new Client instance with an additional default header.
// This follows the immutable/chaining pattern.
func (c *Client) WithHeader(key, value string) *Client {
	newClient := &Client{
		http:         c.http,
		baseURL:      c.baseURL,
		headers:      c.headers.Clone(),
		baseResponse: c.baseResponse,
	}
	newClient.headers.Set(key, value)

	return newClient
}

// BaseResponse returns a new BaseResponse wrapper if a provider is configured.
func (c *Client) BaseResponse() BaseResponse {
	if c.baseResponse == nil {
		return nil
	}

	return c.baseResponse()
}

// HTTP returns the underlying [HTTPDoer].
func (c *Client) HTTP() HTTPDoer {
	return c.http
}

// Request builds and executes an HTTP request.
// The path is joined with the client's base URL, and query values are appended to the URL.
func (c *Client) Request(
	ctx context.Context,
	method, path string,
	body []byte,
	query any,
	mods ...RequestModifier,
) (*http.Response, error) {
	rel, err := url.Parse(strings.TrimLeft(path, "/"))
	if err != nil {
		return nil, fmt.Errorf("rest: invalid path: %w", err)
	}

	u := c.baseURL.ResolveReference(rel)

	qValues, err := StructToValues(query)
	if err != nil {
		return nil, err
	}

	if len(qValues) > 0 {
		u.RawQuery = qValues.Encode()
	}

	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), bodyReader)
	if err != nil {
		return nil, fmt.Errorf("rest: failed to create request: %w", err)
	}

	maps.Copy(req.Header, c.headers)

	for _, mod := range mods {
		mod(req)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rest: request failed: %w", err)
	}

	return resp, nil
}

// GetJSON performs a GET request and decodes the JSON response body into a new instance of Resp.
// Returns an *APIError if the response status is not 2xx.
func GetJSON[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	query any,
	mods ...RequestModifier,
) (*Resp, error) {
	resp, err := c.Request(ctx, http.MethodGet, path, nil, query, mods...)
	if err != nil {
		return nil, err
	}

	result := new(Resp)
	if err := handleJSONResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// PostJSON marshals the payload to JSON, performs a POST request, and decodes the
// response body into a new instance of Resp.
// It automatically sets the Content-Type and Accept headers to application/json.
func PostJSON[Req, Resp any](
	ctx context.Context,
	c Requester,
	path string,
	payload Req,
	query any,
	mods ...RequestModifier,
) (*Resp, error) {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("rest: failed to marshal payload: %w", err)
	}

	jsonMod := func(req *http.Request) {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
	}
	// Prepend JSON headers so they can be overridden by user mods if needed
	mods = append([]RequestModifier{jsonMod}, mods...)

	resp, err := c.Request(ctx, http.MethodPost, path, bodyBytes, query, mods...)
	if err != nil {
		return nil, err
	}

	result := new(Resp)
	if err := handleJSONResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// PatchJSON marshals the payload to JSON, performs a PATCH request, and decodes the
// response body into a new instance of Resp.
// It automatically sets the Content-Type and Accept headers to application/json.
func PatchJSON[Req, Resp any](
	ctx context.Context,
	c Requester,
	path string,
	payload Req,
	query any,
	mods ...RequestModifier,
) (*Resp, error) {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("rest: failed to marshal payload: %w", err)
	}

	jsonMod := func(req *http.Request) {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
	}
	// Prepend JSON headers so they can be overridden by user mods if needed
	mods = append([]RequestModifier{jsonMod}, mods...)

	resp, err := c.Request(ctx, http.MethodPatch, path, bodyBytes, query, mods...)
	if err != nil {
		return nil, err
	}

	result := new(Resp)
	if err := handleJSONResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// DeleteJSON marshals the payload to JSON (if not nil), performs a DELETE request, and decodes the
// response body into a new instance of Resp.
func DeleteJSON[Req, Resp any](
	ctx context.Context,
	c Requester,
	path string,
	payload Req,
	query any,
	mods ...RequestModifier,
) (*Resp, error) {
	var (
		bodyBytes []byte
		err       error
	)

	bodyBytes, err = json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("rest: failed to marshal payload: %w", err)
	}

	if string(bodyBytes) == "null" {
		bodyBytes = nil
	}

	jsonMod := func(req *http.Request) {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
	}
	mods = append([]RequestModifier{jsonMod}, mods...)

	resp, err := c.Request(ctx, http.MethodDelete, path, bodyBytes, query, mods...)
	if err != nil {
		return nil, err
	}

	result := new(Resp)
	if err := handleJSONResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// handleJSONResponse closes the body and handles status code validation.
func handleJSONResponse(resp *http.Response, target any, requester Requester) error {
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return &APIError{StatusCode: resp.StatusCode, Body: bodyBytes}
	}

	// If target is nil or status is 204 No Content, discard body and return
	if target == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	if provider, ok := requester.(BaseResponseProvider); ok {
		if br := provider.BaseResponse(); br != nil {
			br.SetData(target)

			if err := json.NewDecoder(resp.Body).Decode(br); err != nil {
				return err
			}

			if !br.IsSuccess() {
				return br.Error()
			}

			return nil
		}
	}

	err := json.NewDecoder(resp.Body).Decode(target)
	if err == io.EOF {
		return nil // Success with empty body
	}

	return err
}
