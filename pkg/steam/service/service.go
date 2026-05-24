// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/rest"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
)

// WebAPIBase is a base url for steam web api endpoints.
const WebAPIBase = "https://api.steampowered.com/"

// ErrInvalidMessage is returned if the protobuf message is provided.
var ErrInvalidMessage = errors.New("service: invalid protobuf message")

// Doer defines the interface for executing transport-agnostic requests.
type Doer interface {
	Do(ctx context.Context, req *tr.Request) (*tr.Response, error)
}

// NoResponse is a sentinel type that indicates that the marshaling should be skipped entirely.
type NoResponse struct{}

// Option defines a functional configuration for the Client.
type Option = bus.Option[*Client]

// Client is the primary entry point for calling Steam Services.
// It acts as a decorator for a [tr.Transport], automatically injecting
// API keys or Access Tokens and validating Steam-specific error results.
type Client struct {
	transport   tr.Transport
	apiKey      string
	accessToken string
	registry    *api.UnmarshalRegistry
}

// Registry returns the underlying registry of decoders.
// Implements [api.RegistryProvider].
func (c *Client) Registry() *api.UnmarshalRegistry {
	return c.registry
}

// APIKey returns the underlying api key.
func (c *Client) APIKey() string {
	return c.apiKey
}

// AccessToken returns the underlying access token.
func (c *Client) AccessToken() string {
	return c.accessToken
}

// WithRegistry sets a custom unmarshal registry for the client.
func WithRegistry(r *api.UnmarshalRegistry) Option {
	return func(c *Client) {
		c.registry = r
	}
}

// New initializes a new Service Client.
func New(tr tr.Transport, opts ...Option) *Client {
	c := &Client{
		transport: tr,
		registry:  api.NewUnmarshalRegistry(),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// WithAPIKey returns a copy of the client with the WebAPI key (v000x style) for subsequent requests.
func (c *Client) WithAPIKey(key string) *Client {
	clone := *c
	clone.apiKey = key

	return &clone
}

// WithAccessToken returns a copy of the client with the modern OAuth2 access token for Unified Services.
func (c *Client) WithAccessToken(token string) *Client {
	clone := *c
	clone.accessToken = token

	return &clone
}

// WithRegistry returns a copy of the client with a custom unmarshal registry.
func (c *Client) WithRegistry(r *api.UnmarshalRegistry) *Client {
	clone := *c
	clone.registry = r
	return &clone
}

// Do executes a request through the underlying transport. It automatically:
//   - Injects credentials (key/token).
//   - Checks HTTP status codes (for Web).
//   - Inspects Steam EResult codes in both HTTP and Socket metadata.
func (c *Client) Do(ctx context.Context, req *tr.Request) (*tr.Response, error) {
	if c.apiKey != "" {
		req.WithParam("key", c.apiKey)
	}

	if c.accessToken != "" {
		req.WithParam("access_token", c.accessToken)
	}

	resp, err := c.transport.Do(ctx, req)
	if err != nil {
		return nil, api.NewSteamAPIError("transport error", 0, err)
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
			return api.NewSteamAPIError("session expired", meta.StatusCode, api.ErrSessionExpired)
		}

		res = meta.Result
		if res == 0 {
			res = enums.EResult_OK
		}
	} else if meta, ok := resp.Socket(); ok {
		res = meta.Result
	}

	if api.IsAuthError(res) {
		return api.NewEResultError(res, api.ErrSessionExpired)
	}

	if res != enums.EResult_OK {
		return api.NewEResultError(res, nil)
	}

	return nil
}

// Unified executes a modern Service method using Protobuf using POST method.
// Only messages of the following type are accepted C[Interface]_[Method]_[Type].
// If the message doesn't match this pattern it returns the [ErrInvalidMessage] error.
//
// Example:
//
//	res, err := service.Unified[PlayerResponse](ctx, client, &CPlayer_GetGameBadgeLevels_Request{...})
func Unified[Resp any](ctx context.Context, d Doer, msg proto.Message, opts ...api.CallOption) (*Resp, error) {
	iface, method, err := inferUnifiedMethod(msg)
	if err != nil {
		return nil, err
	}

	return UnifiedExplicit[Resp](ctx, d, http.MethodPost, iface, method, 1, msg, opts...)
}

// UnifiedExplicit is like Unified but requires manual specification of service path and version.
func UnifiedExplicit[Resp any](
	ctx context.Context,
	d Doer,
	httpMethod, iface, method string,
	version int,
	msg proto.Message,
	opts ...api.CallOption,
) (*Resp, error) {
	req, err := NewUnifiedRequest(httpMethod, iface, method, version, msg)
	if err != nil {
		return nil, err
	}

	return execute[Resp](ctx, d, req, api.FormatProtobuf, opts...)
}

// WebAPI executes a standard JSON-based WebAPI request.
func WebAPI[Resp any](
	ctx context.Context,
	d Doer,
	httpMethod, iface, method string,
	version int,
	reqMsg any,
	opts ...api.CallOption,
) (*Resp, error) {
	req := NewWebAPIRequest(httpMethod, iface, method, version)

	if reqMsg != nil {
		params, err := rest.StructToValues(reqMsg)
		if err != nil {
			return nil, err
		}

		req.WithParams(params)
	}

	return execute[Resp](ctx, d, req, api.FormatJSON, opts...)
}

// Legacy executes a low-level Protobuf request based on an EMsg.
// This is primarily used for Socket communication.
//
// Deprecated: Use LegacyProto instead. This function exists for special cases
// where the CM header is not needed.
func Legacy[Resp any](
	ctx context.Context,
	d Doer,
	eMsg enums.EMsg,
	reqMsg proto.Message,
	opts ...api.CallOption,
) (*Resp, error) {
	req, err := NewLegacyRequest(eMsg, reqMsg)
	if err != nil {
		return nil, err
	}

	return execute[Resp](ctx, d, req, api.FormatProtobuf, opts...)
}

// LegacyProto is like Legacy but forces a Protobuf CM header on the outer Steam
// packet. Use this for EMsg-based messages that carry a proto body but are NOT
// Unified Service methods — most notably EMsg_ClientToGC.
func LegacyProto[Resp any](
	ctx context.Context,
	d Doer,
	eMsg enums.EMsg,
	reqMsg proto.Message,
	opts ...api.CallOption,
) (*Resp, error) {
	req, err := NewLegacyProtoRequest(eMsg, reqMsg)
	if err != nil {
		return nil, err
	}

	return execute[Resp](ctx, d, req, api.FormatProtobuf, opts...)
}

// WithRoutingAppID returns a CallOption that sets the routing AppID in the
// outer CM packet's proto header. Required when sending to EMsg_ClientToGC so
// Steam knows which Game Coordinator to forward the message to.
func WithRoutingAppID(appID uint32) api.CallOption {
	return func(req *tr.Request, _ *api.CallConfig) {
		req.WithRoutingAppID(appID)
	}
}

func execute[Resp any](
	ctx context.Context,
	d Doer,
	req *tr.Request,
	def api.ResponseFormat,
	opts ...api.CallOption,
) (*Resp, error) {
	type registryProvider interface {
		Registry() *api.UnmarshalRegistry
	}

	cfg := &api.CallConfig{Format: def}

	if rp, ok := d.(registryProvider); ok {
		cfg.Registry = rp.Registry()
	} else {
		cfg.Registry = api.NewUnmarshalRegistry()
	}

	for _, opt := range opts {
		opt(req, cfg)
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		req.WithParam("__no_response", "true")
	}

	resp, err := d.Do(ctx, req)
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		return nil, nil
	}

	result := new(Resp)
	if err := cfg.Registry.Unmarshal(resp.Body, result, cfg.Format); err != nil {
		return nil, err
	}

	return result, nil
}

// --- Reflection Magic ---

var methodCache sync.Map // cache for reflect.Type -> methodInfo

type methodInfo struct {
	Iface, Method string
}

// inferUnifiedMethod extracts the Steam Service name and Method from a Protobuf
// message type using Go reflection and naming conventions.
//
// Converts "CPlayer_GetGameBadgeLevels_Request" -> "Player", "GetGameBadgeLevels"
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
