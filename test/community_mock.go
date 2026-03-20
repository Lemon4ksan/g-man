// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/rest"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
)

type MockCommunityRequester struct {
	mu sync.Mutex

	Calls []*http.Request

	Responses    map[string][]byte
	ResponseErrs map[string]error
	StatusCodes  map[string]int
	Headers      map[string]http.Header

	MockSessionID string
}

func NewMockCommunityRequester() *MockCommunityRequester {
	return &MockCommunityRequester{
		Responses:     make(map[string][]byte),
		ResponseErrs:  make(map[string]error),
		StatusCodes:   make(map[string]int),
		Headers:       make(map[string]http.Header),
		MockSessionID: "mock_session_12345",
	}
}

func (m *MockCommunityRequester) SessionID(baseURL string) string {
	return m.MockSessionID
}

func (m *MockCommunityRequester) Request(ctx context.Context, method, path string, body []byte, query url.Values, mods ...rest.RequestModifier) (*http.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	u, _ := url.Parse(community.BaseURL + path)
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}
	urlStr := u.String()

	req, _ := http.NewRequestWithContext(ctx, method, urlStr, bytes.NewReader(body))

	for _, mod := range mods {
		mod(req)
	}
	m.Calls = append(m.Calls, req)

	if err, ok := m.ResponseErrs[urlStr]; ok && err != nil {
		return nil, err
	}

	statusCode := http.StatusOK
	if code, ok := m.StatusCodes[urlStr]; ok {
		statusCode = code
	}

	resp := &http.Response{
		StatusCode: statusCode,
		Header:     m.Headers[urlStr],
		Body:       io.NopCloser(bytes.NewReader(m.Responses[urlStr])),
		Request:    req,
	}

	return resp, nil
}

func (m *MockCommunityRequester) SetJSONResponse(url string, statusCode int, obj any) {
	b, _ := json.Marshal(obj)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Responses[url] = b
	m.StatusCodes[url] = statusCode
}

func (m *MockCommunityRequester) SetHTMLResponse(url string, statusCode int, html string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Responses[url] = []byte(html)
	m.StatusCodes[url] = statusCode
}

func (m *MockCommunityRequester) SetRedirect(url string, location string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.StatusCodes[url] = http.StatusFound
	h := make(http.Header)
	h.Set("Location", location)
	m.Headers[url] = h
}

func (m *MockCommunityRequester) GetLastCall() *http.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.Calls) == 0 {
		return nil
	}
	return m.Calls[len(m.Calls)-1]
}

func (m *MockCommunityRequester) ClearCalls() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = nil
}

func (m *MockCommunityRequester) CallsCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Calls)
}
