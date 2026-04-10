// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/lemon4ksan/g-man/pkg/rest"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
)

// HTTPUserAgent is the user agent string used by the official Steam Client.
const HTTPUserAgent = "Valve/Steam HTTP Client 1.0"

// HTTPMetadata holds context-specific information from an HTTP response.
type HTTPMetadata struct {
	Result     protocol.EResult // EResult code from the 'x-eresult' header.
	StatusCode int              // Standard HTTP status code (e.g., 200, 404).
	Header     http.Header      // Full HTTP response headers.
}

// HTTPTransport implements the Transport interface for HTTP-based communication.
// It translates abstract Requests into concrete http.Requests.
type HTTPTransport struct {
	client *rest.Client
}

// HTTPTarget is an extension of the Target interface for destinations that can be
// reached via HTTP.
type HTTPTarget interface {
	Target
	HTTPPath() string
	HTTPMethod() string
}

// NewHTTPTransport creates a new HTTP transport layer.
// It uses the provided rest.HTTPDoer for executing requests.
func NewHTTPTransport(doer rest.HTTPDoer, baseURL string) *HTTPTransport {
	return &HTTPTransport{
		client: rest.NewClient(doer).
			WithBaseURL(baseURL).
			WithHeader("User-Agent", HTTPUserAgent),
	}
}

// Do executes a Request over HTTP. It performs the following steps:
// 1. Asserts that the request's Target is an HTTPTarget.
// 2. Encodes the request body into a base64 string for the 'input_protobuf_encoded' parameter.
// 3. Builds and sends the request using the underlying rest.Client.
// 4. Parses the EResult from the 'x-eresult' header.
// 5. Wraps the result in a generic Response container.
func (t *HTTPTransport) Do(ctx context.Context, req *Request) (*Response, error) {
	target, ok := req.Target().(HTTPTarget)
	if !ok {
		return nil, fmt.Errorf("http: target %T does not support HTTP transport", req.Target())
	}

	params := req.Params()

	// Steam's modern WebAPI expects the Protobuf payload to be base64-encoded in a form field.
	if len(req.Body()) > 0 {
		params.Set("input_protobuf_encoded", base64.StdEncoding.EncodeToString(req.Body()))
	}

	// Create a modifier to pass our abstract headers to the concrete http.Request.
	mods := []rest.RequestModifier{
		func(r *http.Request) {
			for key, values := range req.Header() {
				for _, val := range values {
					r.Header.Add(key, val)
				}
			}
			r.Header.Set("Accept", "text/html,*/*;q=0.9")
		},
	}

	httpResp, err := t.client.Request(ctx, target.HTTPMethod(), target.HTTPPath(), nil, params, mods...)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("http: failed to read response: %w", err)
	}

	return NewResponse(body, HTTPMetadata{
		Result:     t.parseEResult(httpResp),
		Header:     httpResp.Header,
		StatusCode: httpResp.StatusCode,
	}), nil
}

// parseEResult extracts the Steam EResult from the 'x-eresult' response header.
// Returns EResult_OK if the header is missing or invalid.
func (t *HTTPTransport) parseEResult(resp *http.Response) protocol.EResult {
	if resHeader := resp.Header.Get("x-eresult"); resHeader != "" {
		if val, err := strconv.Atoi(resHeader); err == nil {
			return protocol.EResult(val)
		}
	}
	return protocol.EResult_OK
}
