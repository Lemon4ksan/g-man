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

	"github.com/lemon4ksan/g-man/pkg/rest"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"

	"google.golang.org/protobuf/proto"
)

const WebAPIBase = "https://api.steampowered.com/"

var (
	// ErrInvalidMessage is returned if the protobuf message is provided.
	ErrInvalidMessage = errors.New("service: invalid protobuf message")
)

// Doer defines the interface for executing transport-agnostic requests.
type Doer interface {
	Do(ctx context.Context, req *tr.Request) (*tr.Response, error)
}

type NoResponse struct{}

// Client is the primary entry point for calling Steam Services.
// It acts as a decorator for a [tr.Transport], automatically injecting
// API keys or Access Tokens and validating Steam-specific error results.
type Client struct {
	transport   tr.Transport
	apiKey      string
	accessToken string
}

// New initializes a new Service Client.
func New(tr tr.Transport) *Client {
	return &Client{transport: tr}
}

// WithAPIKey sets the WebAPI key (v000x style) for subsequent requests.
func (c *Client) WithAPIKey(key string) *Client {
	clone := *c
	clone.apiKey = key
	return &clone
}

// WithAccessToken sets the modern OAuth2 access token for Unified Services.
func (c *Client) WithAccessToken(token string) *Client {
	clone := *c
	clone.accessToken = token
	return &clone
}

// Do executes a request through the underlying transport. It automatically:
// 1. Injects credentials (key/token).
// 2. Checks HTTP status codes (for Web).
// 3. Inspects Steam EResult codes in both HTTP and Socket metadata.
func (c *Client) Do(ctx context.Context, req *tr.Request) (*tr.Response, error) {
	if c.apiKey != "" {
		req.WithParam("key", c.apiKey)
	}
	if c.accessToken != "" {
		req.WithParam("access_token", c.accessToken)
	}

	resp, err := c.transport.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("transport error: %w", err)
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
			return api.SteamAPIError{StatusCode: meta.StatusCode, Err: api.ErrSessionExpired}
		}
		res = meta.Result
		if res == 0 {
			res = enums.EResult_OK
		}
	} else if meta, ok := resp.Socket(); ok {
		res = meta.Result
	}

	if api.IsAuthError(res) {
		return api.EResultError{EResult: res, Err: api.ErrSessionExpired}
	}

	if res != enums.EResult_OK {
		return api.EResultError{EResult: res}
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
func UnifiedExplicit[Resp any](ctx context.Context, d Doer, httpMethod, iface, method string, version int, msg proto.Message, opts ...api.CallOption) (*Resp, error) {
	req, err := NewUnifiedRequest(httpMethod, iface, method, version, msg)
	if err != nil {
		return nil, err
	}
	return execute[Resp](ctx, d, req, api.FormatProtobuf, opts...)
}

// WebAPI executes a standard JSON-based WebAPI request.
func WebAPI[Resp any](ctx context.Context, d Doer, httpMethod, iface, method string, version int, reqMsg any, opts ...api.CallOption) (*Resp, error) {
	req := NewWebAPIRequest(httpMethod, iface, method, version)
	if reqMsg != nil {
		params, _ := rest.StructToValues(reqMsg)
		req.WithParams(params)
	}
	return execute[Resp](ctx, d, req, api.FormatJSON, opts...)
}

// Legacy executes a low-level Protobuf request based on an EMsg.
// This is primarily used for Socket communication.
func Legacy[Resp any](ctx context.Context, d Doer, eMsg enums.EMsg, reqMsg proto.Message, opts ...api.CallOption) (*Resp, error) {
	req, err := NewLegacyRequest(eMsg, reqMsg)
	if err != nil {
		return nil, err
	}
	return execute[Resp](ctx, d, req, api.FormatProtobuf, opts...)
}

func execute[Resp any](ctx context.Context, d Doer, req *tr.Request, def api.ResponseFormat, opts ...api.CallOption) (*Resp, error) {
	cfg := &api.CallConfig{Format: def}
	for _, opt := range opts {
		opt(req, cfg)
	}

	resp, err := d.Do(ctx, req)
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		return nil, nil
	}

	result := new(Resp)
	if err := api.UnmarshalResponse(resp.Body, result, cfg.Format); err != nil {
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

	actualType := t
	for actualType.Kind() == reflect.Pointer {
		actualType = actualType.Elem()
	}

	name := actualType.Name()
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
	methodCache.Store(t, methodInfo{Iface: iface, Method: method})

	return iface, method, nil
}
