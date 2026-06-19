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

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/miyako/generic"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/steam/encoding"
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

// NoResponse is a sentinel type that indicates that marshaling should be skipped entirely.
type NoResponse struct{}

// Result wraps a value and an error, providing a convenient way to handle both together.
// Use Unwrap to get the value or panic on error.
type Result[T any] struct {
	Value T
	Err   error
}

// Unwrap returns the value or panics if an error is present.
func (r Result[T]) Unwrap() T {
	if r.Err != nil {
		panic(r.Err)
	}

	return r.Value
}

// Ok creates a successful Result.
func Ok[T any](val T) Result[T] {
	return Result[T]{Value: val}
}

// Err creates a failed Result.
func Err[T any](err error) Result[T] {
	return Result[T]{Err: err}
}

// FromCall executes a function that returns (T, error) and wraps the result.
func FromCall[T any](fn func() (T, error)) Result[T] {
	val, err := fn()
	return Result[T]{Value: val, Err: err}
}

// Map transforms a Result by applying a function to its value.
// If the Result contains an error, the function is not called and the error is preserved.
func Map[T, U any](r Result[T], fn func(T) U) Result[U] {
	if r.Err != nil {
		return Result[U]{Err: r.Err}
	}

	return Result[U]{Value: fn(r.Value)}
}

// MapError transforms a Result's error by applying a function to it.
// If the Result is successful, it is returned unchanged.
func MapError[T any](r Result[T], fn func(error) error) Result[T] {
	if r.Err != nil {
		return Result[T]{Value: r.Value, Err: fn(r.Err)}
	}

	return r
}

// FlatMap transforms a Result by applying a function that returns a Result.
// This is useful for chaining operations that can fail.
func FlatMap[T, U any](r Result[T], fn func(T) Result[U]) Result[U] {
	if r.Err != nil {
		return Result[U]{Err: r.Err}
	}

	return fn(r.Value)
}

// UnwrapOr returns the value if successful, or the provided default value.
func UnwrapOr[T any](r Result[T], def T) T {
	if r.Err != nil {
		return def
	}

	return r.Value
}

// IsOk returns true if the Result is successful.
func IsOk[T any](r Result[T]) bool {
	return r.Err == nil
}

// IsErr returns true if the Result contains an error.
func IsErr[T any](r Result[T]) bool {
	return r.Err != nil
}

// Option defines a functional configuration for the Client.
type Option = generic.Option[*Client]

// Client is the primary entry point for calling Steam Services.
//
// It acts as a decorator for a [tr.Transport], automatically injecting
// API keys or Access Tokens, and validating Steam-specific error results.
// Create and configure new instances of Client using [New].
type Client struct {
	transport   tr.Transport
	apiKey      string
	accessToken string
}

// APIKey returns the underlying API key.
func (c *Client) APIKey() string {
	return c.apiKey
}

// AccessToken returns the underlying access token.
func (c *Client) AccessToken() string {
	return c.accessToken
}

// New initializes a new Service Client.
func New(tr tr.Transport, opts ...Option) *Client {
	c := &Client{transport: tr}

	generic.ApplyOptions(c, opts...)

	return c
}

// WithAPIKey returns a copy of the client with the WebAPI key configured for subsequent requests.
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

// Do executes a request through the underlying transport.
//
// It automatically injects credentials (key/token) and intercepts responses
// to check for Steam-specific results. If an authentication failure occurs,
// it returns [api.ErrSessionExpired] wrapped in an [api.SteamAPIError].
// If the Steam EResult code is not OK, it returns an [api.EResultError].
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

// Unified executes a modern Service method using Protobuf using POST method.
// Only messages of the following type are accepted C[Interface]_[Method]_[Type].
// If the message doesn't match this pattern it returns the [ErrInvalidMessage] error.
//
// Example:
//
//	res, err := service.Unified[PlayerResponse](ctx, client, &CPlayer_GetGameBadgeLevels_Request{...})
func Unified[Resp any](ctx context.Context, d Doer, msg proto.Message, opts ...CallOption) (*Resp, error) {
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
	opts ...CallOption,
) (*Resp, error) {
	req, err := NewUnifiedRequest(httpMethod, iface, method, version, msg)
	if err != nil {
		return nil, err
	}

	return execute[Resp](ctx, d, req, encoding.ProtobufDecoder, opts...)
}

// WebAPI executes a standard JSON-based WebAPI request.
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

	return execute[Resp](ctx, d, req, encoding.SteamJSONDecoder, opts...)
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
	opts ...CallOption,
) (*Resp, error) {
	req, err := NewLegacyRequest(eMsg, reqMsg)
	if err != nil {
		return nil, err
	}

	return execute[Resp](ctx, d, req, encoding.ProtobufDecoder, opts...)
}

// LegacyProto is like Legacy but forces a Protobuf CM header on the outer Steam
// packet. Use this for EMsg-based messages that carry a proto body but are NOT
// Unified Service methods — most notably EMsg_ClientToGC.
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

	return execute[Resp](ctx, d, req, encoding.ProtobufDecoder, opts...)
}

func execute[Resp any](
	ctx context.Context,
	d Doer,
	req *tr.Request,
	defDecoder aoni.Decoder,
	opts ...CallOption,
) (*Resp, error) {
	for _, opt := range opts {
		opt(req)
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		req.WithParam("__no_response", "true")
	}

	resp, err := d.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		return nil, nil
	}

	result := new(Resp)
	if err := req.Decoder(defDecoder).Decode(resp.Body, result); err != nil {
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
// It returns [ErrInvalidMessage] if the provided request message req is nil,
// or if the message name cannot be parsed.
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
