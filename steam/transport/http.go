// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport

import (
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lemon4ksan/g-man/steam/protocol"
)

const HTTPUserAgent = "Valve/Steam HTTP Client 1.0"

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type HTTPTarget interface {
	Target
	HTTPPath() string
	HTTPMethod() string
}

type HTTPTransport struct {
	client  HTTPDoer
	baseURL string
}

var _ Transport = (*HTTPTransport)(nil)

func NewHTTPTransport(client HTTPDoer, baseURL string) *HTTPTransport {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &HTTPTransport{
		client:  client,
		baseURL: baseURL,
	}
}

func (t *HTTPTransport) Do(req *Request) (*Response, error) {
	target, ok := req.Target().(HTTPTarget)
	if !ok {
		return nil, fmt.Errorf("http: target %T does not support HTTP transport", req.Target())
	}

	method := target.HTTPMethod()
	fullURL := t.baseURL + target.HTTPPath()
	v := req.Params()

	// Steam WebAPI trick: Wrap protobuf payload into base64 form parameter
	if len(req.Body()) > 0 {
		v.Set("input_protobuf_encoded", base64.StdEncoding.EncodeToString(req.Body()))
	}

	var bodyReader io.Reader
	queryString := v.Encode()

	if len(v) > 0 {
		if method == "GET" {
			fullURL += "?" + queryString
		} else {
			bodyReader = strings.NewReader(queryString)
		}
	}

	httpReq, err := http.NewRequestWithContext(req.Context(), method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("http request build: %w", err)
	}

	httpReq.Header.Set("User-Agent", HTTPUserAgent)
	httpReq.Header.Set("Accept", "text/html,*/*;q=0.9")
	httpReq.Header.Set("Accept-Encoding", "gzip,identity,*;q=0")

	if method == "POST" && httpReq.Header.Get("Content-Type") == "" {
		httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	}

	for key, values := range req.Header() {
		for _, val := range values {
			httpReq.Header.Add(key, val)
		}
	}

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := t.readBody(resp)
	if err != nil {
		return nil, err
	}

	return &Response{
		Header:     resp.Header,
		StatusCode: resp.StatusCode,
		Result:     t.parseEResult(resp),
		Body:       body,
	}, nil
}

func (t *HTTPTransport) parseEResult(resp *http.Response) protocol.EResult {
	if resHeader := resp.Header.Get("x-eresult"); resHeader != "" {
		if val, err := strconv.Atoi(resHeader); err == nil {
			return protocol.EResult(val)
		}
	}
	return protocol.EResult_OK
}

func (t *HTTPTransport) readBody(resp *http.Response) ([]byte, error) {
	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		reader = gz
	}
	return io.ReadAll(reader)
}

func (t *HTTPTransport) Close() error { return nil }
