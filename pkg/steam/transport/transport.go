// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package transport provides an abstraction layer over Steam API communication,
// allowing seamless switching between HTTP WebAPI and Connection Manager sockets.
package transport

import (
	"context"
	"net/http"
	"net/url"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
)

// Target represents the destination of a Steam request.
type Target interface {
	String() string
}

// Request is a protocol-agnostic container for a Steam API call.
type Request struct {
	ctx         context.Context
	target      Target
	body        []byte
	params      url.Values
	headers     http.Header
}

// NewRequest creates a new Request with the specified context, target, and payload.
func NewRequest(ctx context.Context, target Target, body []byte) *Request {
	return &Request{
		ctx:     ctx,
		target:  target,
		body:    body,
		params:  make(url.Values),
		headers: make(http.Header),
	}
}

// WithParam adds a key-value parameter (e.g., query string for WebAPI).
func (r *Request) WithParam(key, value string) *Request {
	r.params.Set(key, value)
	return r
}

// WithParams merges multiple parameters into the Request.
func (r *Request) WithParams(params url.Values) *Request {
	for k, vs := range params {
		for _, v := range vs {
			r.params.Add(k, v)
		}
	}
	return r
}

// WithHeader adds metadata (used as HTTP headers or Socket routing info).
func (r *Request) WithHeader(key, value string) *Request {
	r.headers.Add(key, value)
	return r
}

func (r *Request) Context() context.Context { return r.ctx }
func (r *Request) Target() Target           { return r.target }
func (r *Request) Body() []byte             { return r.body }
func (r *Request) Params() url.Values       { return r.params }
func (r *Request) Header() http.Header      { return r.headers }

// Response represents the result of a Steam API call.
type Response struct {
	StatusCode  int
	Header      http.Header
	Result      protocol.EResult
	Body        []byte
	SourceJobID uint64
}

// Transport executes a Request and returns a Response.
// Implementations must be safe for concurrent use.
type Transport interface {
	Do(req *Request) (*Response, error)
	Close() error
}
