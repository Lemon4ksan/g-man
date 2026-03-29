// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package service

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/rest"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"

	"google.golang.org/protobuf/proto"
)

// Requester defines the interface for executing transport-agnostic requests.
type Requester interface {
	Do(ctx context.Context, req *tr.Request) (*tr.Response, error)
}

const WebAPIBase = "https://api.steampowered.com/"

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
	c.apiKey = key
	return c
}

// WithAccessToken sets the modern OAuth2 access token for Unified Services.
func (c *Client) WithAccessToken(token string) *Client {
	c.accessToken = token
	return c
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
	if meta, ok := resp.HTTP(); ok {
		if meta.StatusCode != http.StatusOK {
			return api.SteamAPIError{StatusCode: meta.StatusCode}
		}
		if meta.Result != protocol.EResult_OK && meta.Result != 0 {
			return api.EResultError{EResult: meta.Result}
		}
		return nil
	}

	if meta, ok := resp.Socket(); ok {
		if meta.Result != protocol.EResult_OK {
			return api.EResultError{EResult: meta.Result}
		}
	}

	return nil
}

// Unified executes a modern Service method using Protobuf.
// It automatically infers the Interface and Method name from the reqMsg type.
//
// Example:
//
//	res, err := service.Unified[PlayerResponse](ctx, client, &CPlayer_GetGameBadgeLevels_Request{...})
func Unified[Resp any](ctx context.Context, c Requester, reqMsg proto.Message, opts ...api.CallOption) (*Resp, error) {
	iface, method, err := inferUnifiedMethod(reqMsg)
	if err != nil {
		return nil, err
	}
	return UnifiedExplicit[Resp](ctx, c, http.MethodPost, iface, method, 1, reqMsg, opts...)
}

// UnifiedExplicit is like Unified but requires manual specification of service path and version.
func UnifiedExplicit[Resp any](ctx context.Context, c Requester, httpMethod, iface, method string, version int, reqMsg proto.Message, opts ...api.CallOption) (*Resp, error) {
	req, err := NewUnifiedRequest(httpMethod, iface, method, version, reqMsg)
	if err != nil {
		return nil, err
	}
	return execute[Resp](ctx, c, req, api.FormatProtobuf, opts...)
}

// WebAPI executes a standard JSON-based WebAPI request.
func WebAPI[Resp any](ctx context.Context, c Requester, httpMethod, iface, method string, version int, reqMsg any, opts ...api.CallOption) (*Resp, error) {
	req := NewWebAPIRequest(httpMethod, iface, method, version)
	if reqMsg != nil {
		params, _ := rest.StructToValues(reqMsg)
		req.WithParams(params)
	}
	return execute[Resp](ctx, c, req, api.FormatJSON, opts...)
}

// Legacy executes a low-level Protobuf request based on an EMsg.
// This is primarily used for Socket communication.
func Legacy[Resp any](ctx context.Context, c Requester, eMsg protocol.EMsg, reqMsg proto.Message, opts ...api.CallOption) (*Resp, error) {
	req, err := NewLegacyRequest(eMsg, reqMsg)
	if err != nil {
		return nil, err
	}
	return execute[Resp](ctx, c, req, api.FormatProtobuf, opts...)
}

func execute[Resp any](ctx context.Context, c Requester, req *tr.Request, def api.ResponseFormat, opts ...api.CallOption) (*Resp, error) {
	cfg := &api.CallConfig{Format: def}
	for _, opt := range opts {
		opt(req, cfg)
	}

	resp, err := c.Do(ctx, req)
	if err != nil {
		return nil, err
	}

	// Handle cases where the caller doesn't expect a response body
	if reflect.TypeFor[Resp]().Kind() == reflect.Interface {
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
		return "", "", fmt.Errorf("service: request message cannot be nil")
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
		return "", "", fmt.Errorf("service: cannot infer unified method from %q (expected format CInterface_Method_Request)", name)
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
		return "", "", fmt.Errorf("service: invalid unified request format %q", name)
	}

	method := strings.Join(parts[1:endIdx], "_")
	methodCache.Store(t, methodInfo{Iface: iface, Method: method})

	return iface, method, nil
}
