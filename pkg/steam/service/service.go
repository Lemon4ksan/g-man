// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package service implements transport-agnostic execution engines for Steam WebAPI, Unified Services, and legacy protocols.
package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/miyako/generic"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/steam/encoding"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
)

// WebAPIBase is the default base URL used for standard Steam Web API endpoints.
const WebAPIBase = "https://api.steampowered.com/"

// ErrInvalidMessage is returned when a provided Protobuf message structure is invalid or nil.
var ErrInvalidMessage = errors.New("service: invalid protobuf message")

// Doer executes transport-agnostic requests and returns responses or errors.
type Doer interface {
	// Do executes the specified request using the underlying transport layers.
	Do(ctx context.Context, req *tr.Request) (*tr.Response, error)
}

// NoResponse indicates that response body marshaling and decoding should be skipped entirely.
// It is used as a generic parameter in [Execute] or [Unified] calls where response content is ignored.
// The helper automatically drains and closes the response body to prevent resource leaks.
type NoResponse = aoni.NoResponse

// CallOption defines a functional configuration option used to modify a [tr.Request] before execution.
type CallOption func(req *tr.Request)

// WithHTTPMethod overrides the default HTTP verb for the request.
// If the method argument is empty, the default request method remains unchanged.
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

// WithVersion configures the explicit API version for the request.
// If the version argument is negative, the behavior is transport-dependent.
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

// WithDecoder configures a custom [aoni.Decoder] for decoding the response body.
// It will panic if the provided decoder argument d is nil.
func WithDecoder(d aoni.Decoder) CallOption {
	return func(req *tr.Request) {
		req.SetDecoder(d)
		req.WithModifier(aoni.WithDecoder(d))
	}
}

// WithFormat configures the expected [encoding.ResponseFormat] for the request.
// It maps the format to pre-configured Steam decoders and configures the request modifiers.
// If the format is unknown or invalid, the request configuration remains unchanged.
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

// WithModifier adds the aoni.RequestModifier to the service request.
func WithModifier(m aoni.RequestModifier) CallOption {
	return func(req *tr.Request) {
		req.WithModifier(m)
	}
}

// WithRoutingAppID configures the routing AppID inside the outer packet header.
// This is typically required when routing EMsg messages to specific Game Coordinators.
func WithRoutingAppID(appID uint32) CallOption {
	return func(req *tr.Request) {
		req.WithRoutingAppID(appID)
	}
}

// Client executes requests on Steam services by wrapping and decorating a [tr.Transport].
// It automatically handles WebAPI key and OAuth2 access token injection.
// It validates standard [enums.EResult] responses upon execution.
type Client struct {
	transport   tr.Transport
	apiKey      string
	accessToken string
}

// APIKey returns the configured WebAPI key.
func (c *Client) APIKey() string { return c.apiKey }

// AccessToken returns the configured OAuth2 access token.
func (c *Client) AccessToken() string { return c.accessToken }

// New creates a new [Client] instance wrapping the specified [tr.Transport].
// It will panic if the provided transport argument is nil.
func New(tr tr.Transport) *Client {
	c := &Client{transport: tr}
	return c
}

// WithAPIKey creates a shallow copy of the client configured with the specified WebAPI key.
// Subsequent requests executed via the returned clone will inject the provided key.
func (c *Client) WithAPIKey(key string) *Client {
	clone := *c
	clone.apiKey = key
	return &clone
}

// WithAccessToken creates a shallow copy of the client configured with the specified OAuth2 access token.
// Subsequent requests executed via the returned clone will inject the provided token.
func (c *Client) WithAccessToken(token string) *Client {
	clone := *c
	clone.accessToken = token
	return &clone
}

// Do executes the specified request using the underlying [tr.Transport].
// It injects configured credentials and validates the response [enums.EResult] code.
// It returns an error if transport execution fails or if the response status is unsuccessful.
// It will panic if the provided request argument is nil.
func (c *Client) Do(ctx context.Context, req *tr.Request) (*tr.Response, error) {
	if c.apiKey != "" {
		req.WithParam("key", c.apiKey)
	}

	if c.accessToken != "" {
		req.WithParam("access_token", c.accessToken)
	}

	resp, err := c.transport.Do(ctx, req)
	if err != nil {
		return nil, NewSteamAPIError("transport error", 0, err)
	}

	if resp == nil {
		return nil, nil
	}

	if err := c.validateEResult(resp); err != nil {
		return nil, err
	}

	return resp, nil
}

func (c *Client) validateEResult(resp *tr.Response) error {
	var res enums.EResult

	if meta, ok := resp.HTTP(); ok {
		if meta.StatusCode == http.StatusUnauthorized {
			return NewSteamAPIError("session expired", meta.StatusCode, ErrSessionExpired)
		}

		res = generic.Coalesce(meta.Result, enums.EResult_OK)
	} else if meta, ok := resp.Socket(); ok {
		res = meta.Result
	}

	if IsAuthError(res) {
		return NewEResultError(res, ErrSessionExpired)
	}

	if res != enums.EResult_OK {
		return NewEResultError(res, nil)
	}

	return nil
}

// Unified executes a modern Service method using a Protobuf message via the POST method.
// It automatically infers the interface and method path from the protobuf type name.
// It returns [ErrInvalidMessage] if the protobuf message is nil or malformed.
// It returns transport or decoding errors if request execution fails.
func Unified[Resp any](ctx context.Context, d Doer, msg proto.Message, opts ...CallOption) (*Resp, error) {
	iface, method, err := inferUnifiedMethod(msg)
	if err != nil {
		return nil, err
	}

	return UnifiedExplicit[Resp](ctx, d, http.MethodPost, iface, method, 1, msg, opts...)
}

// UnifiedExplicit executes a modern Service method with an explicitly specified path and version.
// It returns [ErrInvalidMessage] if the protobuf message is nil or malformed.
// It returns transport or decoding errors if request execution fails.
func UnifiedExplicit[Resp any](
	ctx context.Context,
	d Doer,
	httpMethod, iface, method string,
	version int,
	msg proto.Message,
	opts ...CallOption,
) (*Resp, error) {
	req, err := NewUnifiedRequest(httpMethod, iface, method, version, msg)
	if err != nil {
		return nil, err
	}

	return Execute[Resp](ctx, d, req, encoding.ProtobufDecoder, opts...)
}

// WebAPI executes a standard Steam WebAPI request.
// It serializes the reqMsg query parameters if they are not nil.
// It returns transport or decoding errors if request execution fails.
func WebAPI[Resp any](
	ctx context.Context,
	d Doer,
	httpMethod, iface, method string,
	version int,
	reqMsg any,
	opts ...CallOption,
) (*Resp, error) {
	req := NewWebAPIRequest(httpMethod, iface, method, version)

	if reqMsg != nil {
		params, err := aoni.StructToValues(reqMsg)
		if err != nil {
			return nil, err
		}

		req.WithParams(params)
	}

	return Execute[Resp](ctx, d, req, encoding.SteamJSONDecoder, opts...)
}

// Legacy executes a low-level Protobuf request matched against a specific [enums.EMsg].
// It returns transport or decoding errors if request execution fails.
func Legacy[Resp any](
	ctx context.Context,
	d Doer,
	eMsg enums.EMsg,
	reqMsg proto.Message,
	opts ...CallOption,
) (*Resp, error) {
	req, err := NewLegacyRequest(eMsg, reqMsg)
	if err != nil {
		return nil, err
	}

	return Execute[Resp](ctx, d, req, encoding.ProtobufDecoder, opts...)
}

// LegacyProto executes a low-level Protobuf request, forcing a Protobuf header on the outer packet.
// It returns transport or decoding errors if request execution fails.
func LegacyProto[Resp any](
	ctx context.Context,
	d Doer,
	eMsg enums.EMsg,
	reqMsg proto.Message,
	opts ...CallOption,
) (*Resp, error) {
	req, err := NewLegacyProtoRequest(eMsg, reqMsg)
	if err != nil {
		return nil, err
	}

	return Execute[Resp](ctx, d, req, encoding.ProtobufDecoder, opts...)
}

// Execute sends a [tr.Request] using a [Doer] and decodes the response body.
// It evaluates options, manages the request payload lifecycle, and automatically closes the response body.
// It returns transport, formatting, or decoding errors if execution fails.
// It will panic if either d or req is nil.
func Execute[Resp any](
	ctx context.Context,
	d Doer,
	req *tr.Request,
	defDecoder aoni.Decoder,
	opts ...CallOption,
) (*Resp, error) {
	for _, opt := range opts {
		opt(req)
	}

	isNoResponse := reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]()
	if isNoResponse {
		req.WithParam("__no_response", "true")
	}

	resp, err := d.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if isNoResponse {
		return nil, nil
	}

	result := new(Resp)
	if err := req.Decoder(defDecoder).Decode(resp.Body, result); err != nil {
		return nil, err
	}

	return result, nil
}

var methodCache sync.Map

type methodInfo struct {
	Iface, Method string
}

func inferUnifiedMethod(req proto.Message) (string, string, error) {
	if req == nil {
		return "", "", fmt.Errorf("%w: request message cannot be nil", ErrInvalidMessage)
	}

	t := reflect.TypeOf(req)
	if val, ok := methodCache.Load(t); ok {
		res := val.(methodInfo)
		return res.Iface, res.Method, nil
	}

	cacheKey := t
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	name := t.Name()

	parts := strings.Split(name, "_")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("%w: cannot infer unified method from %q", ErrInvalidMessage, name)
	}

	iface := parts[0]
	if strings.HasPrefix(iface, "C") && len(iface) > 1 {
		iface = iface[1:]
	}

	endIdx := len(parts)
	if parts[len(parts)-1] == "Request" {
		endIdx--
	}

	if endIdx <= 1 {
		return "", "", fmt.Errorf("%w: invalid unified request format %q", ErrInvalidMessage, name)
	}

	method := strings.Join(parts[1:endIdx], "_")
	methodCache.Store(cacheKey, methodInfo{Iface: iface, Method: method})

	return iface, method, nil
}
