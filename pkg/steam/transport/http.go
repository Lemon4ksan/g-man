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

	"github.com/lemon4ksan/aoni"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

// HTTPUserAgent is the user agent string used by the official Steam Client.
const HTTPUserAgent = "Valve/Steam HTTP Client 1.0"

// HTTPMetadata holds context-specific information from an HTTP response.
type HTTPMetadata struct {
	// Result is the Steam result code extracted from the response headers.
	Result enums.EResult
	// StatusCode is the standard HTTP status code returned by the server.
	StatusCode int
	// Header contains the full set of HTTP response headers.
	Header http.Header
}

// HTTPTransport implements the [Transport] interface for HTTP-based communication.
// It translates abstract [Request] structures into concrete HTTP requests.
//
// Create new instances of HTTPTransport using [NewHTTPTransport].
type HTTPTransport struct {
	client *aoni.Client
}

// HTTPTarget is an extension of the Target interface for destinations that can be
// reached via HTTP.
type HTTPTarget interface {
	Target
	HTTPPath() string
	HTTPMethod() string
}

// NewHTTPTransport creates a new HTTP transport layer.
// It uses the provided aoni.HTTPDoer for executing requests.
func NewHTTPTransport(doer aoni.HTTPDoer, baseURL string) *HTTPTransport {
	return &HTTPTransport{
		client: aoni.NewClient(doer,
			aoni.WithClientBaseURL(baseURL),
			aoni.WithClientUserAgent(HTTPUserAgent),
		),
	}
}

// Do executes a [Request] over HTTP.
//
// It returns an error if the request's [Target] does not implement [HTTPTarget],
// if the underlying REST call fails, or if reading the response body fails.
func (t *HTTPTransport) Do(ctx context.Context, req *Request) (*Response, error) {
	target, ok := req.Target().(HTTPTarget)
	if !ok {
		return nil, fmt.Errorf("http: target %T does not support HTTP transport", req.Target())
	}

	params := req.Params()

	var bodyBytes []byte
	if req.Body != nil {
		var err error

		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("http: failed to read request body: %w", err)
		}
	}

	if len(bodyBytes) > 0 {
		params.Set("input_protobuf_encoded", base64.StdEncoding.EncodeToString(bodyBytes))
	}

	mods := append([]aoni.RequestModifier{
		aoni.WithQuery(params),
		func(r *http.Request) {
			for key, values := range req.Header() {
				for _, val := range values {
					r.Header.Add(key, val)
				}
			}

			r.Header.Set("Accept", "text/html,*/*;q=0.9")
		},
	}, req.Modifiers()...)

	resp, err := t.client.Request(ctx, target.HTTPMethod(), target.HTTPPath(), mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	return NewResponse(resp.Body, HTTPMetadata{
		Result:     t.parseEResult(resp),
		Header:     resp.Header,
		StatusCode: resp.StatusCode,
	}), nil
}

// parseEResult extracts the Steam EResult from the 'x-eresult' response header.
// Returns EResult_OK if the header is missing or invalid.
func (t *HTTPTransport) parseEResult(resp *http.Response) enums.EResult {
	if resHeader := resp.Header.Get("x-eresult"); resHeader != "" {
		if val, err := strconv.Atoi(resHeader); err == nil {
			return enums.EResult(val)
		}
	}

	return enums.EResult_OK
}
