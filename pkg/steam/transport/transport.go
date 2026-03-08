// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package transport provides an abstraction layer over Steam API communication,
// allowing seamless switching between HTTP WebAPI and Connection Manager sockets.
package transport

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"google.golang.org/protobuf/proto"
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
	targetJobID uint64
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

// WithTargetJobID sets the target job ID for legacy response routing over CM.
func (r *Request) WithTargetJobID(id uint64) *Request {
	r.targetJobID = id
	return r
}

func (r *Request) Context() context.Context { return r.ctx }
func (r *Request) Target() Target           { return r.target }
func (r *Request) Body() []byte             { return r.body }
func (r *Request) Params() url.Values       { return r.params }
func (r *Request) Header() http.Header      { return r.headers }
func (r *Request) TargetJobID() uint64      { return r.targetJobID }

// Response represents the result of a Steam API call.
type Response struct {
	StatusCode  int
	Header      http.Header
	Result      protocol.EResult
	Body        []byte
	SourceJobID uint64
}

// UnmarshalTo deserializes the response body into the provided protobuf message.
func (r *Response) UnmarshalTo(msg proto.Message) error {
	if len(r.Body) == 0 {
		return fmt.Errorf("transport: response body is empty")
	}
	return proto.Unmarshal(r.Body, msg)
}

// Transport executes a Request and returns a Response.
// Implementations must be safe for concurrent use.
type Transport interface {
	Do(req *Request) (*Response, error)
	Close() error
}
