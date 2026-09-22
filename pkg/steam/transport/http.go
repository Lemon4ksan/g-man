// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

const HTTPUserAgent = "Valve/Steam HTTP Client 1.0"

var ErrTargetNotHTTP = errors.New("http: target does not support HTTP transport")

type HTTPMetadata struct {
	Result     enums.EResult
	StatusCode int
	Header     http.Header
}

// HTTPTransport executes transport requests over HTTPS WebAPI using standard or fast.Client engines.
type HTTPTransport struct {
	client  *aoni.Client
	baseURL string
}

type HTTPTarget interface {
	Target
	HTTPPath() string
	HTTPMethod() string
}

// NewHTTPTransport constructs an HTTPTransport instance.
func NewHTTPTransport(doer any, baseURL string) *HTTPTransport {
	client := aoni.NewClient(doer,
		option.WithBaseURL(baseURL),
		option.WithUserAgent(HTTPUserAgent),
	)

	return &HTTPTransport{
		client:  client,
		baseURL: baseURL,
	}
}

func (t *HTTPTransport) Do(ctx context.Context, req *Request) (*Response, error) {
	target, ok := req.Target().(HTTPTarget)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrTargetNotHTTP, req.Target())
	}

	params := req.Params()

	bodyBytes, err := extractBodyBytes(req.Body)
	if err != nil {
		return nil, fmt.Errorf("http: failed to read request body: %w", err)
	}

	if len(bodyBytes) > 0 {
		encBuf := make([]byte, base64.StdEncoding.EncodedLen(len(bodyBytes)))
		base64.StdEncoding.Encode(encBuf, bodyBytes)
		params.Set("input_protobuf_encoded", bytesconv.B2S(encBuf))
	}

	mods := make([]aoni.RequestModifier, 0, len(req.Modifiers())+2)
	if len(params) > 0 {
		mods = append(mods, mod.WithQuery(params))
	}

	mods = append(mods, mod.Custom(func(r aoni.Request) {
		for key, values := range req.Header() {
			for _, val := range values {
				r.AddHeader(key, val)
			}
		}

		if r.Header("User-Agent") == "" {
			r.SetHeader("User-Agent", HTTPUserAgent)
		}

		r.SetHeader("Accept", "text/html,*/*;q=0.9")
	}))
	mods = append(mods, req.Modifiers()...)

	//nolint:bodyclose // body is wrapped into Response (bodyRC) and closed by caller
	resp, err := t.client.Request(ctx, target.HTTPMethod(), target.HTTPPath(), mods...)
	if err != nil {
		return nil, err
	}

	var bodyRC io.ReadCloser
	if resp != nil {
		bodyRC = resp.Body
	}

	return NewResponse(bodyRC, HTTPMetadata{
		Result:     t.parseEResult(resp),
		Header:     resp.Header,
		StatusCode: resp.StatusCode,
	}), nil
}

// parseEResult extracts the Valve EResult code from HTTP response headers.
//
// Invariant: Steam WebAPI returns result codes via the "x-eresult" response header.
// Note on Valve quirk: In certain edge cases, Steam servers intermittently return "x-eresult: 2"
// (EResult_Fail) despite HTTP 200 OK and valid JSON body payloads.
//
// Parity: matches node-steam-tradeoffer-manager and steamcommunity error handling.
func (t *HTTPTransport) parseEResult(r *http.Response) enums.EResult {
	if r == nil || r.Header == nil {
		return enums.EResult_OK
	}

	resHeader := r.Header.Get("x-eresult")
	if resHeader != "" {
		if val, ok := bytesconv.ParseUintFast(bytesconv.S2B(resHeader)); ok {
			return enums.EResult(val)
		}
	}

	return enums.EResult_OK
}

func extractBodyBytes(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, nil
	}

	if buf, ok := r.(*bytes.Buffer); ok {
		return buf.Bytes(), nil
	}

	if br, ok := r.(*bytes.Reader); ok {
		b := make([]byte, br.Len())
		_, err := br.ReadAt(b, 0)

		return b, err
	}

	return io.ReadAll(r)
}
