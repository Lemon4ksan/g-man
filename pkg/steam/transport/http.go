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
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"

	"github.com/lemon4ksan/g-man/internal/bytesconv"
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
	client     *aoni.Client
	fastClient *fast.Client
	doer       aoni.HTTPDoer
	baseURL    string
}

type HTTPTarget interface {
	Target
	HTTPPath() string
	HTTPMethod() string
}

// NewHTTPTransport constructs an HTTPTransport instance.
func NewHTTPTransport(doer any, baseURL string) *HTTPTransport {
	tr := &HTTPTransport{
		baseURL: baseURL,
	}

	if doer == nil {
		tr.fastClient = fast.NewClient(option.WithBaseURL(baseURL), option.WithUserAgent(HTTPUserAgent))
		return tr
	}

	if fc, ok := doer.(*fast.Client); ok {
		tr.fastClient = fc.With(option.WithBaseURL(baseURL), option.WithUserAgent(HTTPUserAgent))
		return tr
	}

	if ac, ok := doer.(*aoni.Client); ok {
		tr.client = ac.With(option.WithBaseURL(baseURL), option.WithUserAgent(HTTPUserAgent))
		return tr
	}

	if hd, ok := doer.(aoni.HTTPDoer); ok {
		tr.doer = hd
		tr.client = aoni.NewClient(hd, option.WithBaseURL(baseURL), option.WithUserAgent(HTTPUserAgent))

		return tr
	}

	if rd, ok := doer.(aoni.RequestDoer); ok {
		tr.client = aoni.NewClient(
			aoni.NewRequestDoerAdapter(rd),
			option.WithBaseURL(baseURL),
			option.WithUserAgent(HTTPUserAgent),
		)

		return tr
	}

	return tr
}

type fastUnsafeReadCloser struct {
	*bytes.Reader
	resp aoni.Response
}

func (f *fastUnsafeReadCloser) Close() error {
	if f.resp != nil {
		return f.resp.Close()
	}

	return nil
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

	if t.fastClient != nil {
		fastReq := fast.NewRequest(nil)
		defer fastReq.Release()

		fastReq.SetContext(ctx)
		fastReq.SetMethod(target.HTTPMethod())
		fastReq.SetURL(t.baseURL + target.HTTPPath())

		if len(params) > 0 {
			fastReq.SetRawQuery(params.Encode())
		}

		for key, values := range req.Header() {
			for _, val := range values {
				fastReq.AddHeader(key, val)
			}
		}

		fastReq.SetHeader("Accept", "text/html,*/*;q=0.9")

		for _, m := range req.Modifiers() {
			if m != nil {
				m(fastReq)
			}
		}

		resp, err := t.fastClient.Do(fastReq)
		if err != nil {
			return nil, err
		}

		var bodyRC io.ReadCloser
		if unsafeResp, ok := resp.(interface{ UnsafeBodyBytes() []byte }); ok {
			bodyRC = &fastUnsafeReadCloser{
				Reader: bytes.NewReader(unsafeResp.UnsafeBodyBytes()),
				resp:   resp,
			}
		} else {
			bodyRC = resp.BodyStream()
		}

		return NewResponse(bodyRC, HTTPMetadata{
			Result:     t.parseEResult(resp),
			Header:     http.Header(resp.Headers()),
			StatusCode: resp.StatusCode(),
		}), nil
	}

	mods := append([]aoni.RequestModifier{
		mod.WithQuery(params),
		func(r aoni.Request) {
			for key, values := range req.Header() {
				for _, val := range values {
					r.AddHeader(key, val)
				}
			}

			r.SetHeader("Accept", "text/html,*/*;q=0.9")
		},
	}, req.Modifiers()...)

	resp, err := t.client.Request(ctx, target.HTTPMethod(), target.HTTPPath(), mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	return NewResponse(resp.Body, HTTPMetadata{
		Result:     t.parseEResult(resp),
		Header:     resp.Header,
		StatusCode: resp.StatusCode,
	}), nil
}

func (t *HTTPTransport) parseEResult(v any) enums.EResult {
	var resHeader string

	switch r := v.(type) {
	case *http.Response:
		if r != nil && r.Header != nil {
			resHeader = r.Header.Get("x-eresult")
		}

	case aoni.Response:
		if r != nil {
			resHeader = r.Header("x-eresult")
		}
	}

	if resHeader != "" {
		if val, ok := bytesconv.ParseInt64(bytesconv.S2B(resHeader)); ok {
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
