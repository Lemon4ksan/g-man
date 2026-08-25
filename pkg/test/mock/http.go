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

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/foundation/codec/json"

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

	rawURL := req.URL.String()
	rawPlus := strings.ReplaceAll(rawURL, "%20", "+")
	rawPercent := strings.ReplaceAll(rawURL, "+", "%20")
	key, _ := url.PathUnescape(rawURL)
	keyPlus := strings.ReplaceAll(key, "%20", "+")
	path, _ := url.PathUnescape(strings.TrimPrefix(req.URL.Path, "/"))

	matchErrKey := key
	if _, exists := s.ResponseErrs[matchErrKey]; !exists {
		if _, exists := s.ResponseErrs[rawURL]; exists {
			matchErrKey = rawURL
		} else if _, exists := s.ResponseErrs[rawPlus]; exists {
			matchErrKey = rawPlus
		} else if _, exists := s.ResponseErrs[rawPercent]; exists {
			matchErrKey = rawPercent
		} else if _, exists := s.ResponseErrs[keyPlus]; exists {
			matchErrKey = keyPlus
		} else if qUnescaped, err := url.QueryUnescape(rawURL); err == nil && s.ResponseErrs[qUnescaped] != nil {
			matchErrKey = qUnescaped
		} else if _, exists := s.ResponseErrs[path]; exists {
			matchErrKey = path
		} else {
			bestMatch := ""
			for k, err := range s.ResponseErrs {
				if err == nil {
					continue
				}
				if strings.Contains(k, "{") {
					prefix, _, _ := strings.Cut(k, "{")
					suffix := k[strings.LastIndex(k, "}")+1:]
					if strings.Contains(rawURL, strings.Trim(prefix, "/")) && strings.HasSuffix(rawURL, suffix) {
						if len(k) > len(bestMatch) {
							bestMatch = k
						}
					}
				} else if k != "" && (strings.Contains(rawURL, k) || strings.Contains(path, k)) {
					if len(k) > len(bestMatch) {
						bestMatch = k
					}
				}
			}
			if bestMatch != "" {
				matchErrKey = bestMatch
			} else if _, exists := s.ResponseErrs[""]; exists {
				matchErrKey = ""
			}
		}
	}

	if err, exists := s.ResponseErrs[matchErrKey]; exists && err != nil {
		return nil, err
	}

	matchKey := key
	if _, exists := s.responses[matchKey]; !exists {
		if _, exists := s.responses[rawURL]; exists {
			matchKey = rawURL
		} else if _, exists := s.responses[rawPlus]; exists {
			matchKey = rawPlus
		} else if _, exists := s.responses[rawPercent]; exists {
			matchKey = rawPercent
		} else if _, exists := s.responses[keyPlus]; exists {
			matchKey = keyPlus
		} else if qUnescaped, err := url.QueryUnescape(rawURL); err == nil && s.responses[qUnescaped] != nil {
			matchKey = qUnescaped
		} else if _, exists := s.responses[path]; exists {
			matchKey = path
		} else {
			bestMatch := ""
			for k := range s.responses {
				if strings.Contains(k, "{") {
					prefix, _, _ := strings.Cut(k, "{")
					suffix := k[strings.LastIndex(k, "}")+1:]
					if strings.Contains(rawURL, strings.Trim(prefix, "/")) && strings.HasSuffix(rawURL, suffix) {
						if len(k) > len(bestMatch) {
							bestMatch = k
						}
					}
				} else if k != "" && (strings.Contains(rawURL, k) || strings.Contains(path, k)) {
					if len(k) > len(bestMatch) {
						bestMatch = k
					}
				}
			}
			if bestMatch != "" {
				matchKey = bestMatch
			} else if _, exists := s.responses[""]; exists {
				matchKey = ""
			}
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

	for _, m := range mods {
		m.Apply(stdReq)
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
	if _, exists := s.ResponseErrs[resolvedURL]; !exists {
		if err, ok := s.ResponseErrs[urlStr]; ok {
			s.ResponseErrs[resolvedURL] = err
		}
	}

	if _, exists := s.responses[resolvedPath]; !exists {
		if data, ok := s.responses[path]; ok {
			s.responses[resolvedPath] = data
			s.statusCodes[resolvedPath] = s.statusCodes[path]
			s.headers[resolvedPath] = s.headers[path]
		}
	}
	if _, exists := s.ResponseErrs[resolvedPath]; !exists {
		if err, ok := s.ResponseErrs[path]; ok {
			s.ResponseErrs[resolvedPath] = err
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
