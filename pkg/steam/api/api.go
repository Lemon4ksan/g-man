// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"

	"github.com/andygrunwald/vdf"
	"github.com/mitchellh/mapstructure"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// VDFUnmarshaler is an interface that can be implemented by types
// to provide custom VDF (Valve Data Format) decoding logic.
type VDFUnmarshaler interface {
	UnmarshalVDF(data []byte) error
}

// ResponseFormat defines the expected encoding of the Steam API response.
type ResponseFormat int

const (
	// FormatUnknown is the default state.
	FormatUnknown ResponseFormat = iota
	// FormatRaw returns the response body as-is without parsing.
	FormatRaw
	// FormatProtobuf parses binary or JSON-encoded Protobuf messages.
	FormatProtobuf
	// FormatJSON parses standard JSON, automatically unwrapping the "response" field if present.
	FormatJSON
	// FormatVDF parses KeyValues/VDF text format.
	FormatVDF
)

// CallConfig holds internal configuration for an API call.
type CallConfig struct {
	Format ResponseFormat
}

// CallOption allows modifying the request (headers, params) or the CallConfig
// before the request is executed.
type CallOption func(req *tr.Request, cfg *CallConfig)

type httpMethodSetter interface {
	SetHTTPMethod(string)
}

type versionSetter interface {
	SetVersion(int)
}

// WithHTTPMethod overrides the default HTTP verb (e.g., "POST" instead of "GET").
func WithHTTPMethod(method string) CallOption {
	return func(req *tr.Request, cfg *CallConfig) {
		if t, ok := req.Target().(httpMethodSetter); ok {
			t.SetHTTPMethod(method)
		}
	}
}

// WithVersion specifies the API version (e.g., 1 for v0001).
func WithVersion(version int) CallOption {
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

// HttpTarget implements the transport.Target interface for basic HTTP calls.
type HttpTarget struct {
	HttpMethod string
	URL        string
}

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
func NewHttpRequest(httpMethod string, url string, body []byte) *tr.Request {
	return tr.NewRequest(HttpTarget{HttpMethod: httpMethod, URL: url}, body)
}

// UnmarshalResponse is the primary dispatcher for decoding Steam API responses
// based on the selected ResponseFormat.
func UnmarshalResponse(data []byte, target any, format ResponseFormat) error {
	if len(data) == 0 {
		return nil
	}
	switch format {
	case FormatRaw:
		if ptr, ok := target.(*[]byte); ok {
			*ptr = append([]byte(nil), data...)
			return nil
		}
		return fmt.Errorf("api: FormatRaw requires *[]byte as output type, got %T", target)
	case FormatProtobuf:
		return UnmarshalProtobuf(data, target)
	case FormatJSON:
		return UnmarshalJSON(data, target)
	case FormatVDF:
		return UnmarshalVDFText(data, target)
	default:
		return fmt.Errorf("api: unsupported format %v", format)
	}
}

// UnmarshalProtobuf decodes Protobuf data. It automatically detects if the
// source is JSON-encoded Protobuf or standard binary wire format.
func UnmarshalProtobuf(data []byte, target any) error {
	pm, ok := target.(proto.Message)
	if !ok {
		return errors.New("api: target is not a proto.Message")
	}
	if len(data) > 0 && data[0] == '{' {
		return protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal(data, pm)
	}
	return proto.Unmarshal(data, pm)
}

// UnmarshalJSON decodes JSON data. If the JSON contains a top-level "response"
// key (common in Steam Web API), it automatically drills down into it.
func UnmarshalJSON(data []byte, target any) error {
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(data, &wrapper); err == nil {
		if inner, ok := wrapper["response"]; ok {
			return json.Unmarshal(inner, target)
		}
	}
	return json.Unmarshal(data, target)
}

// UnmarshalVDFText parses Valve Data Format (KeyValues) text. Like UnmarshalJSON,
// it automatically handles the "response" wrapper.
func UnmarshalVDFText(data []byte, target any) error {
	p := vdf.NewParser(bytes.NewReader(data))
	m, err := p.Parse()
	if err != nil {
		return err
	}

	config := &mapstructure.DecoderConfig{
		WeaklyTypedInput: true,
		Result:           target,
		Squash:           true,
	}

	decoder, err := mapstructure.NewDecoder(config)
	if err != nil {
		return err
	}

	if res, ok := m["response"].(map[string]any); ok {
		return decoder.Decode(res)
	}

	return decoder.Decode(m)
}
