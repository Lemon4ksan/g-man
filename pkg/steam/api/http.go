// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package api

import (
	"net/url"
	"strings"

	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
)

// CallConfig holds internal configuration for an API call.
type CallConfig struct {
	Format   ResponseFormat
	Registry *UnmarshalRegistry
}

// CallOption allows modifying the request (headers, params) or the CallConfig
// before the request is executed.
type CallOption func(req *tr.Request, cfg *CallConfig)

// WithHTTPMethod overrides the default HTTP verb (e.g., "POST" instead of "GET").
func WithHTTPMethod(method string) CallOption {
	type httpMethodSetter interface {
		SetHTTPMethod(string)
	}

	return func(req *tr.Request, cfg *CallConfig) {
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

	return func(req *tr.Request, cfg *CallConfig) {
		if t, ok := req.Target().(versionSetter); ok {
			t.SetVersion(version)
		}
	}
}

// WithHeader adds a custom HTTP header to the request.
func WithHeader(key, value string) CallOption {
	return func(req *tr.Request, _ *CallConfig) {
		req.WithHeader(key, value)
	}
}

// WithFormat tells the unmarshaler how to process the response body.
func WithFormat(f ResponseFormat) CallOption {
	return func(_ *tr.Request, cfg *CallConfig) {
		cfg.Format = f
	}
}

// WithQueryParam adds a single key=value pair to the URL query string.
func WithQueryParam(key, value string) CallOption {
	return func(req *tr.Request, _ *CallConfig) {
		req.WithParam(key, value)
	}
}

// WithQueryParams adds multiple key=value pairs to the URL query string.
func WithQueryParams(v url.Values) CallOption {
	return func(req *tr.Request, _ *CallConfig) {
		req.WithParams(v)
	}
}

// WithOverrideAPIKey sets or overrides the "key" parameter in the request.
func WithOverrideAPIKey(key string) CallOption {
	return func(req *tr.Request, _ *CallConfig) {
		req.WithParam("key", key)
	}
}

// WithCustomRegistry sets the underlying registry for response decoding.
func WithCustomRegistry(r *UnmarshalRegistry) CallOption {
	return func(_ *tr.Request, cfg *CallConfig) {
		cfg.Registry = r
	}
}

// HttpTarget implements the transport.Target interface for basic HTTP calls.
type HttpTarget struct {
	HttpMethod string
	URL        string
}

// String returns the underlying url.
func (c HttpTarget) String() string { return c.URL }

// HTTPMethod returns the configured method or "GET" as a default.
func (c HttpTarget) HTTPMethod() string {
	if c.HttpMethod != "" {
		return c.HttpMethod
	}

	return "GET"
}

// HTTPPath extracts the path component from the URL.
func (c HttpTarget) HTTPPath() string {
	u, _ := url.Parse(c.URL)
	return strings.TrimPrefix(u.Path, "/")
}

// NewHttpRequest creates a new transport request for a generic HTTP endpoint.
func NewHttpRequest(httpMethod, url string, body []byte) *tr.Request {
	return tr.NewRequest(HttpTarget{HttpMethod: httpMethod, URL: url}, body)
}
