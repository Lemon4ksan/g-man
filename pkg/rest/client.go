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

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// RequestModifier allows modifying the request before it's sent.
type RequestModifier func(req *http.Request)

// Client is a generic REST HTTP client.
type Client struct {
	http    HTTPDoer
	baseURL string
	headers http.Header
}

// NewClient creates a new REST client.
func NewClient(httpClient HTTPDoer, baseURL string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{
		http:    httpClient,
		baseURL: strings.TrimRight(baseURL, "/"),
		headers: make(http.Header),
	}
}

// SetHeader sets a default header for all requests made by this client.
func (c *Client) SetHeader(key, value string) *Client {
	c.headers.Set(key, value)
	return c
}

// Request executes an HTTP request.
func (c *Client) Request(ctx context.Context, method, path string, body []byte, params url.Values, mods ...RequestModifier) (*http.Response, error) {
	var bodyReader io.Reader

	fullURL := c.baseURL + "/" + strings.TrimLeft(path, "/")

	if len(params) > 0 {
		if method == http.MethodGet {
			fullURL += "?" + params.Encode()
		} else {
			bodyReader = strings.NewReader(params.Encode())
		}
	}

	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("rest: failed to create request: %w", err)
	}

	// Add default headers
	maps.Copy(req.Header, c.headers)

	// Apply custom modifications (like specific headers for this request)
	for _, mod := range mods {
		mod(req)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rest: request failed: %w", err)
	}

	return resp, nil
}

// GetJSON is a high-level helper to fetch and unmarshal JSON directly.
func (c *Client) GetJSON(ctx context.Context, path string, params url.Values, target any, mods ...RequestModifier) error {
	resp, err := c.Request(ctx, http.MethodGet, path, nil, params, mods...)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("rest: unexpected status code %d", resp.StatusCode)
	}

	if target == nil {
		return nil
	}

	return json.NewDecoder(resp.Body).Decode(target)
}

// PostJSON is a high-level helper to send a JSON payload and decode the response.
func (c *Client) PostJSON(ctx context.Context, path string, payload any, target any, mods ...RequestModifier) error {
	var body []byte
	var err error

	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("rest: failed to marshal payload: %w", err)
		}
	}

	// Ensure Content-Type is set to JSON
	jsonMod := func(req *http.Request) {
		req.Header.Set("Content-Type", "application/json")
	}
	mods = append([]RequestModifier{jsonMod}, mods...)

	resp, err := c.Request(ctx, http.MethodPost, path, body, nil, mods...)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("rest: unexpected status code %d", resp.StatusCode)
	}

	if target == nil {
		return nil
	}

	return json.NewDecoder(resp.Body).Decode(target)
}
