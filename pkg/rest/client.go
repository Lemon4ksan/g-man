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

// RequestModifier is a function that can modify an *http.Request before it is sent.
// This is used for adding one-off headers, authentication tokens, or logging.
type RequestModifier func(req *http.Request)

// Client is a concrete implementation of the Requester interface.
// It maintains a base URL and a set of default headers applied to every request.
type Client struct {
	http    HTTPDoer
	baseURL *url.URL
	headers http.Header
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

// WithBaseURL returns a new Client instance with the specified base URL.
// It ensures the base URL has exactly one trailing slash to make
// url.ResolveReference work correctly with relative paths.
func (c *Client) WithBaseURL(raw string) *Client {
	if raw == "" {
		return &Client{
			http:    c.http,
			baseURL: &url.URL{},
			headers: c.headers.Clone(),
		}
	}

	if !strings.HasSuffix(raw, "/") {
		raw += "/"
	}

	baseURL, _ := url.Parse(raw)

	return &Client{
		http:    c.http,
		baseURL: baseURL,
		headers: c.headers.Clone(),
	}
}

// WithHeader returns a new Client instance with an additional default header.
// This follows the immutable/chaining pattern.
func (c *Client) WithHeader(key, value string) *Client {
	newClient := &Client{
		http:    c.http,
		baseURL: c.baseURL,
		headers: c.headers.Clone(),
	}
	newClient.headers.Set(key, value)

	return newClient
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
	if err := handleJSONResponse(resp, result); err != nil {
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
	if err := handleJSONResponse(resp, result); err != nil {
		return nil, err
	}

	return result, nil
}

// handleJSONResponse closes the body and handles status code validation.
func handleJSONResponse(resp *http.Response, target any) error {
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return &APIError{StatusCode: resp.StatusCode, Body: bodyBytes}
	}

	// If target is nil (e.g., 204 No Content), discard body and return
	if target == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	return json.NewDecoder(resp.Body).Decode(target)
}
