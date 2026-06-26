// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package service

import (
	"io"
	"net/url"
	"strings"

	"github.com/lemon4ksan/aoni"

	"github.com/lemon4ksan/g-man/pkg/steam/encoding"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
)

// CallOption allows modifying the request (headers, params) or the CallConfig
// before the request is executed.
type CallOption func(req *tr.Request)

// WithHTTPMethod overrides the default HTTP verb (e.g., "POST" instead of "GET").
func WithHTTPMethod(method string) CallOption {
	type httpMethodSetter interface {
		SetHTTPMethod(string)
	}

	return func(req *tr.Request) {
		if t, ok := req.Target().(httpMethodSetter); ok {
			t.SetHTTPMethod(method)
		}
	}
}

// WithVersion specifies the API version (e.g., 1 for v0001).
func WithVersion(version int) CallOption {
	type versionSetter interface {
		SetVersion(int)
	}

	return func(req *tr.Request) {
		if t, ok := req.Target().(versionSetter); ok {
			t.SetVersion(version)
		}
	}
}

// WithDecoder sets the decoder for the response body.
func WithDecoder(d aoni.Decoder) CallOption {
	return func(req *tr.Request) {
		req.SetDecoder(d)
		req.WithModifier(aoni.WithDecoder(d))
	}
}

// WithFormat sets the response format for the request.
func WithFormat(f encoding.ResponseFormat) CallOption {
	return func(req *tr.Request) {
		var decoder aoni.Decoder
		switch f {
		case encoding.FormatJSON:
			decoder = encoding.SteamJSONDecoder
		case encoding.FormatProtobuf:
			decoder = encoding.ProtobufDecoder
		case encoding.FormatVDF:
			decoder = encoding.VDFDecoder
		case encoding.FormatBinaryVDF:
			decoder = encoding.BinaryVDFDecoder
		case encoding.FormatRaw:
			decoder = aoni.RawDecoder
		}

		if decoder != nil {
			req.SetDecoder(decoder)
			req.WithModifier(aoni.WithDecoder(decoder))
		}
	}
}

// WithRoutingAppID returns a CallOption that sets the routing AppID in the
// outer CM packet's proto header. Required when sending to EMsg_ClientToGC so
// Steam knows which Game Coordinator to forward the message to.
func WithRoutingAppID(appID uint32) CallOption {
	return func(req *tr.Request) {
		req.WithRoutingAppID(appID)
	}
}

// HTTPTarget implements the transport Target interface for basic HTTP calls.
//
// It represents an HTTP-specific request target. Use inline initialization
// or helper functions like [NewHTTPRequest] to build requests using this target.
type HTTPTarget struct {
	// Method is the HTTP verb (e.g., "GET", "POST").
	Method string
	// URL is the destination endpoint address.
	URL string
}

// String returns the underlying url.
func (c HTTPTarget) String() string { return c.URL }

// HTTPMethod returns the configured method or "GET" as a default.
func (c HTTPTarget) HTTPMethod() string {
	if c.Method != "" {
		return c.Method
	}

	return "GET"
}

// HTTPPath extracts the path component from the URL.
func (c HTTPTarget) HTTPPath() string {
	u, _ := url.Parse(c.URL)
	return strings.TrimPrefix(u.Path, "/")
}

// NewHTTPRequest creates a new transport request for a generic HTTP endpoint.
func NewHTTPRequest(httpMethod, url string, body io.Reader) *tr.Request {
	return tr.NewRequest(HTTPTarget{Method: httpMethod, URL: url}, body)
}
