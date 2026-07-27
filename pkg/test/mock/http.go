// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mock

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	json "github.com/goccy/go-json"
	"github.com/lemon4ksan/aoni"

	"github.com/lemon4ksan/g-man/pkg/steam/community"
)

type HTTPStub struct {
	mu sync.RWMutex

	Calls        []*http.Request
	ResponseErrs map[string]error

	responses   map[string][]byte
	statusCodes map[string]int
	headers     map[string]http.Header

	apiKey        string
	MockSessionID string
}

func NewHTTPStub() *HTTPStub {
	return &HTTPStub{
		ResponseErrs:  make(map[string]error),
		responses:     make(map[string][]byte),
		statusCodes:   make(map[string]int),
		headers:       make(map[string]http.Header),
		apiKey:        "key_123",
		MockSessionID: "mock_session_12345",
	}
}

func (s *HTTPStub) SessionID(baseURL string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.MockSessionID
}

func (s *HTTPStub) SetSessionID(sid string) {
	s.mu.Lock()
	s.MockSessionID = sid
	s.mu.Unlock()
}

func (s *HTTPStub) GetOrRegisterAPIKey(ctx context.Context, domain string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.apiKey, nil
}

func (s *HTTPStub) SetAPIKey(key string) {
	s.mu.Lock()
	s.apiKey = key
	s.mu.Unlock()
}

func (s *HTTPStub) Do(req *http.Request) (*http.Response, error) {
	s.mu.Lock()
	s.Calls = append(s.Calls, req)
	s.mu.Unlock()

	s.mu.RLock()
	defer s.mu.RUnlock()

	key, _ := url.PathUnescape(req.URL.String())
	path, _ := url.PathUnescape(strings.TrimPrefix(req.URL.Path, "/"))

	matchErrKey := key
	if _, exists := s.ResponseErrs[matchErrKey]; !exists {
		if _, exists := s.ResponseErrs[path]; exists {
			matchErrKey = path
		} else if _, exists := s.ResponseErrs[""]; exists {
			matchErrKey = ""
		}
	}

	if err, exists := s.ResponseErrs[matchErrKey]; exists && err != nil {
		return nil, err
	}

	matchKey := key
	if _, exists := s.responses[matchKey]; !exists {
		if _, exists := s.responses[path]; exists {
			matchKey = path
		} else if _, exists := s.responses[""]; exists {
			matchKey = ""
		}
	}

	statusCode := http.StatusOK
	if code, exists := s.statusCodes[matchKey]; exists {
		statusCode = code
	}

	return &http.Response{
		StatusCode: statusCode,
		Header:     s.headers[matchKey],
		Body:       io.NopCloser(bytes.NewReader(s.responses[matchKey])),
		Request:    req,
	}, nil
}

func (s *HTTPStub) Request(
	ctx context.Context,
	method, path string,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	urlStr := community.BaseURL + path
	req, _ := http.NewRequestWithContext(ctx, method, urlStr, nil)
	stdReq := aoni.NewStdRequest(req)

	for _, mod := range mods {
		mod(stdReq)
	}

	resolvedURL, _ := url.PathUnescape(req.URL.String())
	resolvedPath, _ := url.PathUnescape(strings.TrimPrefix(req.URL.Path, "/"))

	s.mu.Lock()

	if _, exists := s.responses[resolvedURL]; !exists {
		if data, ok := s.responses[urlStr]; ok {
			s.responses[resolvedURL] = data
			s.statusCodes[resolvedURL] = s.statusCodes[urlStr]
			s.headers[resolvedURL] = s.headers[urlStr]
		}
	}

	if _, exists := s.responses[resolvedPath]; !exists {
		if data, ok := s.responses[path]; ok {
			s.responses[resolvedPath] = data
			s.statusCodes[resolvedPath] = s.statusCodes[path]
			s.headers[resolvedPath] = s.headers[path]
		}
	}

	s.mu.Unlock()

	return s.Do(req)
}

func (s *HTTPStub) SetRawResponse(key string, statusCode int, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.responses[key] = data
	s.statusCodes[key] = statusCode
}

func (s *HTTPStub) SetJSONResponse(key string, statusCode int, obj any) {
	b, _ := json.Marshal(obj)
	s.SetRawResponse(key, statusCode, b)
}

func (s *HTTPStub) SetHTMLResponse(key string, statusCode int, html string) {
	s.SetRawResponse(key, statusCode, []byte(html))
}

func (s *HTTPStub) SetRedirect(key, location string) {
	s.mu.Lock()
	s.statusCodes[key] = http.StatusFound
	h := make(http.Header)
	h.Set("Location", location)
	s.headers[key] = h
	s.mu.Unlock()
}

func (s *HTTPStub) GetLastCall() *http.Request {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.Calls) == 0 {
		return nil
	}

	return s.Calls[len(s.Calls)-1]
}

func (s *HTTPStub) GetLastCallParams() url.Values {
	req := s.GetLastCall()
	if req == nil {
		return nil
	}

	if req.Method == http.MethodPost {
		_ = req.ParseForm()
		return req.PostForm
	}

	return req.URL.Query()
}

func (s *HTTPStub) ClearCalls() {
	s.mu.Lock()
	s.Calls = nil
	s.mu.Unlock()
}

func (s *HTTPStub) CallsCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.Calls)
}
