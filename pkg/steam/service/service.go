// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package service provides transport-agnostic execution engines for Steam Unified Services, WebAPI, and EMsg socket calls.
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
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/codec/values"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/miyako/generic"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/steam/encoding"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
)

type FastFormEncoder interface {
	EncodeFormString() (string, error)
}

const WebAPIBase = "https://api.steampowered.com/"

var ErrInvalidMessage = errors.New("service: invalid protobuf message")

// Doer executes abstract transport requests.
type Doer interface {
	Do(ctx context.Context, req *tr.Request) (*tr.Response, error)
}

type NoResponse struct{}

type CallOption func(req *tr.Request)

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

func WithDecoder(d decode.Decoder) CallOption {
	return func(req *tr.Request) {
		req.SetDecoder(d)
		req.WithModifier(mod.WithDecoder(d))
	}
}

func WithFormat(f encoding.ResponseFormat) CallOption {
	return func(req *tr.Request) {
		switch f {
		case encoding.FormatJSON:
			WithDecoder(encoding.SteamJSONDecoder)(req)
		case encoding.FormatProtobuf:
			WithDecoder(encoding.ProtobufDecoder)(req)
		case encoding.FormatVDF:
			WithDecoder(encoding.VDFDecoder)(req)
		case encoding.FormatBinaryVDF:
			WithDecoder(encoding.BinaryVDFDecoder)(req)
		case encoding.FormatRaw:
			WithDecoder(decode.RawDecoder)(req)
		}
	}
}

func WithModifier(m aoni.RequestModifier) CallOption {
	return func(req *tr.Request) {
		req.WithModifier(m)
	}
}

func WithRoutingAppID(appID uint32) CallOption {
	return func(req *tr.Request) {
		req.WithRoutingAppID(appID)
	}
}

// Client wraps a transport layer and injects configured WebAPI keys and OAuth access tokens.
type Client struct {
	transport   tr.Transport
	apiKey      string
	accessToken string
}

func (c *Client) APIKey() string { return c.apiKey }

func (c *Client) AccessToken() string { return c.accessToken }

func New(tr tr.Transport) *Client {
	return &Client{transport: tr}
}

func (c *Client) WithAPIKey(key string) *Client {
	clone := *c
	clone.apiKey = key

	return &clone
}

func (c *Client) WithAccessToken(token string) *Client {
	clone := *c
	clone.accessToken = token

	return &clone
}

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

// Unified executes a Service method using Protobuf type reflection to infer interface and method names.
func Unified[Resp any](ctx context.Context, d Doer, msg proto.Message, opts ...CallOption) (*Resp, error) {
	iface, method, err := inferUnifiedMethod(msg)
	if err != nil {
		return nil, err
	}

	return UnifiedExplicit[Resp](ctx, d, http.MethodPost, iface, method, 1, msg, opts...)
}

// UnifiedExplicit executes a Service method with explicit interface and method path overrides.
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

// WebAPI executes a standard WebAPI request.
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
		if encoder, ok := reqMsg.(FastFormEncoder); ok {
			encoded, err := encoder.EncodeFormString()
			if err != nil {
				return nil, err
			}

			req.WithParam("input_protobuf_encoded", encoded)
		} else {
			qStr, err := values.StructToQueryString(reqMsg)
			if err != nil {
				return nil, err
			}

			if qStr != "" {
				for pair := range strings.SplitSeq(qStr, "&") {
					if k, v, ok := strings.Cut(pair, "="); ok {
						req.WithParam(k, v)
					}
				}
			}
		}
	}

	return Execute[Resp](ctx, d, req, encoding.SteamJSONDecoder, opts...)
}

// Legacy executes an EMsg socket request.
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

// LegacyProto executes an EMsg socket request forcing a Protobuf outer header.
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

// Execute transmits a Request and unmarshals the response payload into Resp.
func Execute[Resp any](
	ctx context.Context,
	d Doer,
	req *tr.Request,
	defDecoder decode.Decoder,
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
