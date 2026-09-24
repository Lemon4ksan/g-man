// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package transport provides protocol-agnostic Request and Response structures unifying HTTP WebAPI and Socket messaging.
package transport

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"reflect"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/decode"
)

type Transport interface {
	Do(ctx context.Context, req *Request) (*Response, error)
}

type Target interface {
	String() string
}

// Request holds parameters, query values, headers, and payload readers for Steam requests.
type Request struct {
	target       Target
	Body         io.Reader
	BodyBytes    []byte
	params       url.Values
	headers      http.Header
	routingAppID uint32
	forceProto   bool
	mods         []aoni.RequestModifier
	decoder      decode.Decoder
}

// NewRequest constructs a Request.
func NewRequest(target Target, body io.Reader) *Request {
	return &Request{
		target:  target,
		Body:    body,
		params:  make(url.Values),
		headers: make(http.Header),
	}
}

func (r *Request) SetDecoder(d decode.Decoder) {
	r.decoder = d
}

func (r *Request) WithModifier(mods ...aoni.RequestModifier) *Request {
	r.mods = append(r.mods, mods...)

	return r
}

func (r *Request) WithParam(key, value string) *Request {
	r.params.Set(key, value)

	return r
}

func (r *Request) WithParams(params url.Values) *Request {
	for k, vs := range params {
		for _, v := range vs {
			r.params.Add(k, v)
		}
	}

	return r
}

func (r *Request) WithHeader(key, value string) *Request {
	r.headers.Add(key, value)

	return r
}

func (r *Request) Modifiers() []aoni.RequestModifier { return r.mods }

func (r *Request) Decoder(def decode.Decoder) decode.Decoder {
	if r.decoder == nil {
		return def
	}

	return r.decoder
}

func (r *Request) Target() Target      { return r.target }
func (r *Request) Params() url.Values  { return r.params }
func (r *Request) Header() http.Header { return r.headers }
func (r *Request) Token() string       { return r.params.Get("access_token") }

func (r *Request) WithRoutingAppID(appID uint32) *Request {
	r.routingAppID = appID

	return r
}

func (r *Request) RoutingAppID() uint32 { return r.routingAppID }

func (r *Request) WithForceProto() *Request {
	r.forceProto = true

	return r
}

func (r *Request) IsForceProto() bool { return r.forceProto }

// Response encapsulates response streams and protocol metadata.
type Response struct {
	Body     io.ReadCloser
	metadata any
}

// NewResponse constructs a Response instance.
func NewResponse(body io.ReadCloser, meta any) *Response {
	return &Response{
		Body:     body,
		metadata: meta,
	}
}

// As populates target with protocol-specific metadata if types match.
//
// Preconditions:
//   - target MUST be a non-nil pointer.
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

func (r *Response) HTTP() (HTTPMetadata, bool) {
	meta, ok := r.metadata.(HTTPMetadata)

	return meta, ok
}

func (r *Response) Socket() (SocketMetadata, bool) {
	meta, ok := r.metadata.(SocketMetadata)

	return meta, ok
}
