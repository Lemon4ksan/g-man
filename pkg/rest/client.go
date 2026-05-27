// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"
)

// UserAgent is a default User-Agent string used for HTTP requests.
const UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// DefaultClient is the shared client instance used by global helper functions.
var DefaultClient = NewClient(nil)

// Get performs a global GET request and decodes the JSON response body.
func Get[Resp any](ctx context.Context, path string, query any, mods ...RequestModifier) (*Resp, error) {
	return GetJSON[Resp](ctx, DefaultClient, path, query, mods...)
}

// Post performs a global POST request and decodes the JSON response body.
func Post[Req, Resp any](
	ctx context.Context,
	path string,
	payload Req,
	query any,
	mods ...RequestModifier,
) (*Resp, error) {
	return PostJSON[Req, Resp](ctx, DefaultClient, path, payload, query, mods...)
}

// HTTPDoer is an interface for objects that can execute an [http.Request].
// It is satisfied by [http.Client].
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// DoerFunc is a function type that implements HTTPDoer.
type DoerFunc func(req *http.Request) (*http.Response, error)

// Do implements the HTTPDoer interface for DoerFunc.
func (f DoerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

// Middleware wraps an HTTPDoer with additional logic.
type Middleware func(next HTTPDoer) HTTPDoer

// Chain applies a series of middlewares to an HTTPDoer, returning the final HTTPDoer.
// The first middleware in the list will be the outermost one (called first).
//
// Example:
//
//	rotator, _ := rest.NewProxyRotator(cfg, proxy1, proxy2)
//	logMiddleware := rest.LoggingMiddleware(logger)
//
//	// Build a chain: Client -> Logging -> Proxy Rotator
//	httpClient := rest.Chain(rotator, logMiddleware)
//	client := rest.NewClient(httpClient)
func Chain(doer HTTPDoer, middlewares ...Middleware) HTTPDoer {
	for i := len(middlewares) - 1; i >= 0; i-- {
		doer = middlewares[i](doer)
	}

	return doer
}

// Requester defines the requirements for performing raw HTTP requests
// with path joining and query parameter encoding.
type Requester interface {
	Request(
		ctx context.Context,
		method, path string,
		body any,
		query any,
		mods ...RequestModifier,
	) (*http.Response, error)
}

// BaseResponseProvider is an optional interface that a Requester can implement
// to provide a BaseResponse wrapper for JSON requests.
type BaseResponseProvider interface {
	BaseResponse() BaseResponse
}

type (
	capturerCtxKey struct{}
	decoderCtxKey  struct{}
)

// Decoder defines an interface for decoding response bodies.
type Decoder interface {
	Decode(r io.Reader, target any) error
}

// DecoderFunc is a function type that implements Decoder.
type DecoderFunc func(r io.Reader, target any) error

// Decode implements the Decoder interface for DecoderFunc.
func (f DecoderFunc) Decode(r io.Reader, target any) error {
	return f(r, target)
}

// JSONDecoder is the default decoder that uses encoding/json.
var JSONDecoder Decoder = DecoderFunc(func(r io.Reader, target any) error {
	return json.NewDecoder(r).Decode(target)
})

// RequestModifier is a function that can modify an *http.Request before it is sent.
// This is used for adding one-off headers, authentication tokens, or logging.
type RequestModifier func(req *http.Request)

// WithVar replaces a placeholder in the path (e.g., "{id}") with a value.
// The value is automatically URL-escaped.
func WithVar(key string, value any) RequestModifier {
	return func(req *http.Request) {
		placeholder := "{" + key + "}"
		escapedValue := url.PathEscape(fmt.Sprint(value))

		req.URL.Path = strings.ReplaceAll(req.URL.Path, placeholder, escapedValue)
		if req.URL.RawPath != "" {
			req.URL.RawPath = strings.ReplaceAll(req.URL.RawPath, placeholder, escapedValue)
		}
	}
}

// WithVars replaces multiple placeholders in the path.
// It accepts pairs of key-value arguments.
// If the pairs slice has an odd length, the function returns early
// without modifying the request.
func WithVars(pairs ...any) RequestModifier {
	return func(req *http.Request) {
		if len(pairs)%2 != 0 {
			return
		}

		for i := 0; i < len(pairs); i += 2 {
			key := fmt.Sprint(pairs[i])
			value := fmt.Sprint(pairs[i+1])
			WithVar(key, value)(req)
		}
	}
}

// WithBearer adds a "Authorization: Bearer <token>" header.
func WithBearer(token string) RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

// WithBasicAuth adds a "Authorization: Basic <base64>" header.
func WithBasicAuth(username, password string) RequestModifier {
	return func(req *http.Request) {
		req.SetBasicAuth(username, password)
	}
}

// WithUserAgent sets the "User-Agent" header.
func WithUserAgent(ua string) RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("User-Agent", ua)
	}
}

// WithContentType sets the "Content-Type" header.
func WithContentType(ct string) RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("Content-Type", ct)
	}
}

// WithCookie adds a cookie to the request.
func WithCookie(c *http.Cookie) RequestModifier {
	return func(req *http.Request) {
		req.AddCookie(c)
	}
}

// WithCookies adds multiple cookies to the request from a map.
func WithCookies(kv map[string]string) RequestModifier {
	return func(req *http.Request) {
		for k, v := range kv {
			req.AddCookie(&http.Cookie{Name: k, Value: v})
		}
	}
}

// WithBody overrides the request body with the provided io.Reader.
func WithBody(r io.Reader) RequestModifier {
	return func(req *http.Request) {
		rc, ok := r.(io.ReadCloser)
		if !ok && r != nil {
			rc = io.NopCloser(r)
		}

		req.Body = rc

		// Attempt to set Content-Length if it's a known size type
		if r != nil {
			if b, ok := r.(interface{ Len() int }); ok {
				req.ContentLength = int64(b.Len())
			} else if s, ok := r.(interface{ Len() int64 }); ok {
				req.ContentLength = s.Len()
			}
		}
	}
}

// WithMultipart creates a RequestModifier that sets the request body to a multipart form
// containing the specified fields and files.
// It automatically sets the Content-Type header to multipart/form-data with the correct boundary.
func WithMultipart(fields map[string]string, files map[string]io.Reader) RequestModifier {
	return func(req *http.Request) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		// Write text fields
		for k, v := range fields {
			if err := writer.WriteField(k, v); err != nil {
				return
			}
		}

		// Write files
		for key, r := range files {
			part, err := writer.CreateFormFile(key, key)
			if err != nil {
				return
			}

			if _, err := io.Copy(part, r); err != nil {
				return
			}
		}

		if err := writer.Close(); err != nil {
			return
		}

		req.Body = io.NopCloser(body)
		req.ContentLength = int64(body.Len())
		req.Header.Set("Content-Type", writer.FormDataContentType())
	}
}

// WithOrigin sets the "Origin" header.
func WithOrigin(origin string) RequestModifier {
	return func(req *http.Request) {
		req.Header.Set("Origin", origin)
	}
}

type debugCtxKey struct{}

// Debug returns a modifier that enables verbose logging for the request.
// It prints the request and response (including headers and body) to stderr.
func Debug() RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), debugCtxKey{}, true)
		*req = *req.WithContext(ctx)
	}
}

// WithDecoder sets the decoder to use for the response body.
func WithDecoder(d Decoder) RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), decoderCtxKey{}, d)
		*req = *req.WithContext(ctx)
	}
}

// CaptureResponse returns a modifier that captures the *http.Response
// of the request. This is useful for accessing headers or cookies
// when using high-level functions like GetJSON.
func CaptureResponse(target **http.Response) RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), capturerCtxKey{}, target)
		*req = *req.WithContext(ctx)
	}
}

// BaseResponse is an interface for response wrappers that include
// status information and a data payload.
//
// If a Client is configured with a BaseResponse provider, it will
// automatically unwrap the response and check for success.
type BaseResponse interface {
	// IsSuccess returns true if the response indicates a successful operation,
	// even if the HTTP status code is 200.
	IsSuccess() bool

	// Error returns an error if IsSuccess is false.
	Error() error

	// SetData provides a pointer where the data payload should be decoded.
	// This is called by the client before unmarshaling the JSON body.
	SetData(data any)
}

// Client is a concrete implementation of the [Requester] interface.
// It maintains a base URL and a set of default headers applied to every request.
//
// Create new instances of Client using the [NewClient] constructor.
type Client struct {
	http         HTTPDoer
	baseURL      *url.URL
	headers      http.Header
	baseResponse func() BaseResponse
}

// NewClient initializes a REST client.
// If httpClient is nil, a default http.Client with a 15-second timeout is used.
func NewClient(httpClient HTTPDoer) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}

	return (&Client{
		http:    httpClient,
		baseURL: &url.URL{},
		headers: make(http.Header),
	}).WithUserAgent(UserAgent)
}

// WithBaseResponse returns a new Client instance that uses the provided
// function to create a BaseResponse wrapper for every JSON request.
func (c *Client) WithBaseResponse(provider func() BaseResponse) *Client {
	newClient := &Client{
		http:         c.http,
		baseURL:      c.baseURL,
		headers:      c.headers.Clone(),
		baseResponse: provider,
	}

	return newClient
}

// WithBaseURL returns a new Client instance with the specified base URL.
// It ensures the base URL has exactly one trailing slash to make
// url.ResolveReference work correctly with relative paths.
func (c *Client) WithBaseURL(raw string) *Client {
	if raw == "" {
		return &Client{
			http:         c.http,
			baseURL:      &url.URL{},
			headers:      c.headers.Clone(),
			baseResponse: c.baseResponse,
		}
	}

	if !strings.HasSuffix(raw, "/") {
		raw += "/"
	}

	baseURL, _ := url.Parse(raw)

	return &Client{
		http:         c.http,
		baseURL:      baseURL,
		headers:      c.headers.Clone(),
		baseResponse: c.baseResponse,
	}
}

// WithHeader returns a new Client instance with an additional default header.
// This follows the immutable/chaining pattern.
func (c *Client) WithHeader(key, value string) *Client {
	newClient := &Client{
		http:         c.http,
		baseURL:      c.baseURL,
		headers:      c.headers.Clone(),
		baseResponse: c.baseResponse,
	}
	newClient.headers.Set(key, value)

	return newClient
}

// WithTimeout returns a new Client instance with the specified timeout.
// This only works if the underlying HTTPDoer is an *http.Client.
func (c *Client) WithTimeout(d time.Duration) *Client {
	newClient := c.clone()
	if httpClient, ok := newClient.http.(*http.Client); ok {
		cloned := *httpClient
		cloned.Timeout = d
		newClient.http = &cloned
	}

	return newClient
}

// WithRedirectLimit returns a new Client instance with a custom redirect policy.
// If max is 0, redirects are disabled. If max < 0, default Go behavior is used.
func (c *Client) WithRedirectLimit(max int) *Client {
	newClient := c.clone()
	if httpClient, ok := newClient.http.(*http.Client); ok {
		cloned := *httpClient
		switch {
		case max == 0:
			cloned.CheckRedirect = func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			}
		case max > 0:
			cloned.CheckRedirect = func(req *http.Request, via []*http.Request) error {
				if len(via) >= max {
					return fmt.Errorf("stopped after %d redirects", max)
				}

				return nil
			}

		default:
			cloned.CheckRedirect = nil
		}

		newClient.http = &cloned
	}

	return newClient
}

// WithLocalAddr returns a new Client instance bound to a specific local IP address.
// This only works if the underlying HTTPDoer is an *http.Client with an *http.Transport.
func (c *Client) WithLocalAddr(addr string) *Client {
	newClient := c.clone()
	if httpClient, ok := newClient.http.(*http.Client); ok {
		if transport, ok := httpClient.Transport.(*http.Transport); ok {
			clonedTransport := transport.Clone()

			localAddr, err := net.ResolveIPAddr("ip", addr)
			if err == nil {
				clonedTransport.DialContext = (&net.Dialer{
					LocalAddr: &net.TCPAddr{IP: localAddr.IP},
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext
			}

			clonedClient := *httpClient
			clonedClient.Transport = clonedTransport
			newClient.http = &clonedClient
		}
	}

	return newClient
}

func (c *Client) clone() *Client {
	return &Client{
		http:         c.http,
		baseURL:      c.baseURL,
		headers:      c.headers.Clone(),
		baseResponse: c.baseResponse,
	}
}

// WithUserAgent returns a new Client instance with a custom User-Agent header.
func (c *Client) WithUserAgent(ua string) *Client {
	return c.WithHeader("User-Agent", ua)
}

// UserAgent returns the default User-Agent header value configured on the client.
func (c *Client) UserAgent() string {
	return c.headers.Get("User-Agent")
}

// WithOrigin returns a new Client instance with a custom Origin header.
func (c *Client) WithOrigin(origin string) *Client {
	return c.WithHeader("Origin", origin)
}

// WithCookieJar returns a new Client instance with the specified cookie jar.
// This only works if the underlying HTTPDoer is an *http.Client.
func (c *Client) WithCookieJar(jar http.CookieJar) *Client {
	newClient := c.clone()
	if httpClient, ok := newClient.http.(*http.Client); ok {
		cloned := *httpClient
		cloned.Jar = jar
		newClient.http = &cloned
	}

	return newClient
}

// BaseResponse returns a new BaseResponse wrapper if a provider is configured.
func (c *Client) BaseResponse() BaseResponse {
	if c.baseResponse == nil {
		return nil
	}

	return c.baseResponse()
}

// HTTP returns the underlying [HTTPDoer].
func (c *Client) HTTP() HTTPDoer {
	return c.http
}

// Transport returns the underlying *http.Transport of the client if the HTTPDoer
// is an *http.Client and its Transport is an *http.Transport.
// Otherwise, it returns nil.
func (c *Client) Transport() *http.Transport {
	if httpClient, ok := c.http.(*http.Client); ok {
		if httpClient.Transport == nil {
			httpClient.Transport = &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				ForceAttemptHTTP2:     true,
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			}
		}

		if transport, ok := httpClient.Transport.(*http.Transport); ok {
			return transport
		}
	}

	return nil
}

// Request builds and executes an HTTP request.
// The path is joined with the client's base URL, and query values are appended to the URL.
func (c *Client) Request(
	ctx context.Context,
	method, path string,
	body any,
	query any,
	mods ...RequestModifier,
) (*http.Response, error) {
	if err := Validate(query); err != nil {
		return nil, err
	}

	rel, err := url.Parse(strings.TrimLeft(path, "/"))
	if err != nil {
		return nil, fmt.Errorf("rest: invalid path: %w", err)
	}

	u := c.baseURL.ResolveReference(rel)

	qValues, err := StructToValues(query)
	if err != nil {
		return nil, err
	}

	if len(qValues) > 0 {
		u.RawQuery = qValues.Encode()
	}

	var bodyReader io.Reader
	switch b := body.(type) {
	case io.Reader:
		bodyReader = b
	case []byte:
		if len(b) > 0 {
			bodyReader = bytes.NewReader(b)
		}
	case string:
		if len(b) > 0 {
			bodyReader = strings.NewReader(b)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), bodyReader)
	if err != nil {
		return nil, fmt.Errorf("rest: failed to create request: %w", err)
	}

	maps.Copy(req.Header, c.headers)

	for _, mod := range mods {
		mod(req)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rest: request failed: %w", err)
	}

	return resp, nil
}

// GetJSON performs a GET request and decodes the JSON response body into a new instance of Resp.
// Returns an *APIError if the response status is not 2xx.
func GetJSON[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	query any,
	mods ...RequestModifier,
) (*Resp, error) {
	resp, err := c.Request(ctx, http.MethodGet, path, nil, query, mods...)
	if err != nil {
		return nil, err
	}

	result := new(Resp)
	if err := handleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// PostForm marshals the payload to URL values, performs a POST request with
// application/x-www-form-urlencoded content type, and decodes the response body into Resp.
func PostForm[Req, Resp any](
	ctx context.Context,
	c Requester,
	path string,
	payload Req,
	query any,
	mods ...RequestModifier,
) (*Resp, error) {
	if err := Validate(payload); err != nil {
		return nil, err
	}

	formValues, err := StructToValues(payload)
	if err != nil {
		return nil, err
	}

	formMod := func(req *http.Request) {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	mods = append([]RequestModifier{formMod}, mods...)

	resp, err := c.Request(ctx, http.MethodPost, path, []byte(formValues.Encode()), query, mods...)
	if err != nil {
		return nil, err
	}

	result := new(Resp)
	if err := handleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// PostJSON marshals the payload to JSON, performs a POST request, and decodes the
// response body into a new instance of Resp.
// It automatically sets the Content-Type and Accept headers to application/json.
func PostJSON[Req, Resp any](
	ctx context.Context,
	c Requester,
	path string,
	payload Req,
	query any,
	mods ...RequestModifier,
) (*Resp, error) {
	if err := Validate(payload); err != nil {
		return nil, err
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("rest: failed to marshal payload: %w", err)
	}

	jsonMod := func(req *http.Request) {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
	}
	// Prepend JSON headers so they can be overridden by user mods if needed
	mods = append([]RequestModifier{jsonMod}, mods...)

	resp, err := c.Request(ctx, http.MethodPost, path, bodyBytes, query, mods...)
	if err != nil {
		return nil, err
	}

	result := new(Resp)
	if err := handleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// PatchJSON marshals the payload to JSON, performs a PATCH request, and decodes the
// response body into a new instance of Resp.
// It automatically sets the Content-Type and Accept headers to application/json.
func PatchJSON[Req, Resp any](
	ctx context.Context,
	c Requester,
	path string,
	payload Req,
	query any,
	mods ...RequestModifier,
) (*Resp, error) {
	if err := Validate(payload); err != nil {
		return nil, err
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("rest: failed to marshal payload: %w", err)
	}

	jsonMod := func(req *http.Request) {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
	}
	// Prepend JSON headers so they can be overridden by user mods if needed
	mods = append([]RequestModifier{jsonMod}, mods...)

	resp, err := c.Request(ctx, http.MethodPatch, path, bodyBytes, query, mods...)
	if err != nil {
		return nil, err
	}

	result := new(Resp)
	if err := handleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// DeleteJSON marshals the payload to JSON (if not nil), performs a DELETE request, and decodes the
// response body into a new instance of Resp.
func DeleteJSON[Req, Resp any](
	ctx context.Context,
	c Requester,
	path string,
	payload Req,
	query any,
	mods ...RequestModifier,
) (*Resp, error) {
	if err := Validate(payload); err != nil {
		return nil, err
	}

	var (
		bodyBytes []byte
		err       error
	)

	bodyBytes, err = json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("rest: failed to marshal payload: %w", err)
	}

	if string(bodyBytes) == "null" {
		bodyBytes = nil
	}

	jsonMod := func(req *http.Request) {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
	}
	mods = append([]RequestModifier{jsonMod}, mods...)

	resp, err := c.Request(ctx, http.MethodDelete, path, bodyBytes, query, mods...)
	if err != nil {
		return nil, err
	}

	result := new(Resp)
	if err := handleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

func handleResponse(resp *http.Response, target any, requester Requester) error {
	if resp == nil {
		return errors.New("rest: response is nil")
	}

	if resp.Request != nil && resp.Request.Context().Value(debugCtxKey{}) != nil {
		reqDump, _ := httputil.DumpRequestOut(resp.Request, true)
		respDump, _ := httputil.DumpResponse(resp, true)

		fmt.Fprintf(os.Stderr, "\n--- HTTP DEBUG ---\n%s\n%s\n------------------\n", string(reqDump), string(respDump))
	}

	if resp.Request != nil {
		if targetPtr, ok := resp.Request.Context().Value(capturerCtxKey{}).(**http.Response); ok {
			*targetPtr = resp
		} else {
			defer resp.Body.Close()
		}
	} else {
		defer resp.Body.Close()
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
		return &APIError{StatusCode: resp.StatusCode, Body: bodyBytes}
	}

	// If target is nil or status is 204 No Content, discard body and return
	if target == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	// Determine which decoder to use
	decoder := JSONDecoder
	if resp.Request != nil {
		if d, ok := resp.Request.Context().Value(decoderCtxKey{}).(Decoder); ok {
			decoder = d
		}
	}

	if provider, ok := requester.(BaseResponseProvider); ok {
		if br := provider.BaseResponse(); br != nil {
			br.SetData(target)

			if err := decoder.Decode(resp.Body, br); err != nil {
				return err
			}

			if !br.IsSuccess() {
				return br.Error()
			}

			return nil
		}
	}

	err := decoder.Decode(resp.Body, target)
	if errors.Is(err, io.EOF) {
		return nil // Success with empty body
	}

	return err
}
