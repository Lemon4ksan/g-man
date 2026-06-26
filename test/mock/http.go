// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// test/mock/http.go
package mock

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
)

// HTTPStub implements aoni.HTTPDoer, aoni.Requester, and community.Requester.
// It unifies all HTTP and Steam Community mocking under a single, thread-safe stub.
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

// NewHTTPStub instantiates a new HTTPStub with production-ready defaults.
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

// SessionID fulfills the community.SessionProvider interface.
func (s *HTTPStub) SessionID(baseURL string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.MockSessionID
}

// SetSessionID updates the stubbed session ID.
func (s *HTTPStub) SetSessionID(sid string) {
	s.mu.Lock()
	s.MockSessionID = sid
	s.mu.Unlock()
}

// GetOrRegisterAPIKey fulfills the community.Requester interface.
func (s *HTTPStub) GetOrRegisterAPIKey(ctx context.Context, domain string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.apiKey, nil
}

// SetAPIKey updates the stubbed WebAPI key.
func (s *HTTPStub) SetAPIKey(key string) {
	s.mu.Lock()
	s.apiKey = key
	s.mu.Unlock()
}

// Do fulfills the aoni.HTTPDoer interface.
func (s *HTTPStub) Do(req *http.Request) (*http.Response, error) {
	s.mu.Lock()
	s.Calls = append(s.Calls, req)
	s.mu.Unlock()

	s.mu.RLock()
	defer s.mu.RUnlock()

	key, _ := url.PathUnescape(req.URL.String())
	path, _ := url.PathUnescape(strings.TrimPrefix(req.URL.Path, "/"))

	// Fallback lookup: Full URL -> Path suffix -> Default fallback
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

// Request fulfills the aoni.Requester interface.
func (s *HTTPStub) Request(
	ctx context.Context,
	method, path string,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	urlStr := community.BaseURL + path
	req, _ := http.NewRequestWithContext(ctx, method, urlStr, nil)
	for _, mod := range mods {
		mod(req)
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

// SetRawResponse registers a raw byte slice response for a path/URL.
func (s *HTTPStub) SetRawResponse(key string, statusCode int, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.responses[key] = data
	s.statusCodes[key] = statusCode
}

// SetJSONResponse serializes and registers an object response for a path/URL.
func (s *HTTPStub) SetJSONResponse(key string, statusCode int, obj any) {
	b, _ := json.Marshal(obj)
	s.SetRawResponse(key, statusCode, b)
}

// SetHTMLResponse registers an HTML string response for a path/URL.
func (s *HTTPStub) SetHTMLResponse(key string, statusCode int, html string) {
	s.SetRawResponse(key, statusCode, []byte(html))
}

// SetRedirect registers a 302 redirect response for a path/URL.
func (s *HTTPStub) SetRedirect(key, location string) {
	s.mu.Lock()
	s.statusCodes[key] = http.StatusFound
	h := make(http.Header)
	h.Set("Location", location)
	s.headers[key] = h
	s.mu.Unlock()
}

// GetLastCall returns the last captured HTTP request.
func (s *HTTPStub) GetLastCall() *http.Request {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.Calls) == 0 {
		return nil
	}
	return s.Calls[len(s.Calls)-1]
}

// GetLastCallParams returns the parameters (Query or Form) of the last call.
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

// ClearCalls clears the captured calls history.
func (s *HTTPStub) ClearCalls() {
	s.mu.Lock()
	s.Calls = nil
	s.mu.Unlock()
}

// CallsCount returns the number of captured calls.
func (s *HTTPStub) CallsCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.Calls)
}
