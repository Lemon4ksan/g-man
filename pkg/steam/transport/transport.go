// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport

import (
	"context"
	"net/http"
	"net/url"
	"reflect"
)

// Transport is the core interface that unifies different network implementations.
type Transport interface {
	Do(ctx context.Context, req *Request) (*Response, error)
}

// Target represents the logical destination of a Steam request.
// It is a marker interface implemented by protocol-specific targets.
type Target interface {
	String() string
}

// Request is a protocol-agnostic container for a Steam API call.
// It holds all the information necessary for either HTTP or socket transports
// to build and send a message.
type Request struct {
	target  Target
	body    []byte
	params  url.Values
	headers http.Header
}

// NewRequest creates a new Request with a target and payload.
func NewRequest(target Target, body []byte) *Request {
	return &Request{
		target:  target,
		body:    body,
		params:  make(url.Values),
		headers: make(http.Header),
	}
}

// WithParam adds a key-value parameter (e.g., a URL query string).
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

// WithHeader adds metadata to the request (e.g., HTTP headers).
func (r *Request) WithHeader(key, value string) *Request {
	r.headers.Add(key, value)
	return r
}

func (r *Request) Target() Target      { return r.target }
func (r *Request) Body() []byte        { return r.body }
func (r *Request) Params() url.Values  { return r.params }
func (r *Request) Header() http.Header { return r.headers }
func (r *Request) Token() string       { return r.params.Get("access_token") }

// Response represents the result of a Steam API call. It is a protocol-agnostic
// container for the body and transport-specific metadata.
type Response struct {
	Body     []byte
	metadata any
}

// NewResponse creates a new Response with a body and associated metadata.
func NewResponse(body []byte, meta any) *Response {
	return &Response{
		Body:     body,
		metadata: meta,
	}
}

// As provides a type-safe way to extract protocol-specific metadata from a Response.
// It functions similarly to errors.As, populating the target if the types match.
//
// Example:
//
//	var meta transport.HTTPMetadata
//	if resp.As(&meta) {
//	    fmt.Println("Status Code:", meta.StatusCode)
//	}
func (r *Response) As(target any) bool {
	if r.metadata == nil {
		return false
	}

	val := reflect.ValueOf(target)
	if val.Kind() != reflect.Pointer || val.IsNil() {
		panic("transport: target must be a non-nil pointer")
	}

	targetVal := val.Elem()
	metaVal := reflect.ValueOf(r.metadata)

	if metaVal.Type().AssignableTo(targetVal.Type()) {
		targetVal.Set(metaVal)
		return true
	}

	return false
}

// HTTP is a convenient helper to extract HTTPMetadata.
func (r *Response) HTTP() (meta HTTPMetadata, ok bool) {
	meta, ok = r.metadata.(HTTPMetadata)
	return
}

// Socket is a convenient helper to extract SocketMetadata.
func (r *Response) Socket() (meta SocketMetadata, ok bool) {
	meta, ok = r.metadata.(SocketMetadata)
	return
}
