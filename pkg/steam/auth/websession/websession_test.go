// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package websession

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
)

const (
	testSteamID       = id.ID(76561197960265728)
	testCustomSteamID = id.ID(76561197960265729)
)

type mockTransport struct {
	handlers map[string]func(r *http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	urlStr := req.URL.String()
	if handler, ok := m.handlers[urlStr]; ok {
		return handler(req)
	}

	for prefix, handler := range m.handlers {
		if strings.HasPrefix(urlStr, prefix) {
			return handler(req)
		}
	}

	return nil, fmt.Errorf("mockTransport: no handler for URL: %s", urlStr)
}

func setupMockTransport() *mockTransport {
	return &mockTransport{handlers: make(map[string]func(r *http.Request) (*http.Response, error))}
}

func newMockedSession(transport *mockTransport) *WebSession {
	return New(testSteamID, log.Discard, aoni.DoerFunc(transport.RoundTrip))
}

func TestNew(t *testing.T) {
	t.Parallel()

	ws := New(testCustomSteamID, log.Discard, nil)

	require.NotNil(t, ws)
	assert.Equal(t, testCustomSteamID, ws.steamID)
	assert.NotNil(t, ws.httpClient)
	assert.NotNil(t, ws.jar)
	assert.False(t, ws.isAuth)
	assert.Len(t, ws.domains, len(defaultDomains))
}

func TestAddDomains(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		ws := newMockedSession(setupMockTransport())
		initialCount := len(ws.domains)
		ws.AddDomains("https://example.com")
		assert.Len(t, ws.domains, initialCount+1)
	})

	t.Run("add_invalid_domain", func(t *testing.T) {
		t.Parallel()

		ws := newMockedSession(setupMockTransport())
		initialCount := len(ws.domains)
		ws.AddDomains(":%:invalid")
		assert.Len(t, ws.domains, initialCount)
	})
}

func TestREST(t *testing.T) {
	t.Parallel()

	ws := newMockedSession(setupMockTransport())
	assert.NotNil(t, ws.REST())
}

func TestHTTP(t *testing.T) {
	t.Parallel()

	ws := newMockedSession(setupMockTransport())
	assert.NotNil(t, ws.HTTP())
	assert.NotNil(t, ws.HTTP().Jar)
}

func TestIsAuthenticated(t *testing.T) {
	t.Parallel()

	ws := newMockedSession(setupMockTransport())
	assert.False(t, ws.IsAuthenticated())
	ws.mu.Lock()
	ws.isAuth = true
	ws.mu.Unlock()
	assert.True(t, ws.IsAuthenticated())
}

func TestClear(t *testing.T) {
	t.Parallel()

	ws := newMockedSession(setupMockTransport())
	ws.isAuth = true
	ws.Clear()
	assert.False(t, ws.isAuth)
}

func TestAuthenticate(t *testing.T) {
	t.Parallel()

	t.Run("empty_refresh_token", func(t *testing.T) {
		t.Parallel()

		ws := newMockedSession(setupMockTransport())
		err := ws.Authenticate(t.Context(), pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_WebBrowser, "", "")
		assert.Error(t, err)
	})

	t.Run("fast_path", func(t *testing.T) {
		t.Parallel()

		ws := newMockedSession(setupMockTransport())
		err := ws.Authenticate(t.Context(), pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient, "rt", "at")
		assert.NoError(t, err)
		assert.True(t, ws.IsAuthenticated())
	})

	t.Run("slow_path_success", func(t *testing.T) {
		t.Parallel()

		mt := setupMockTransport()
		mt.handlers[urlFinalize] = func(r *http.Request) (*http.Response, error) {
			resp, _ := json.Marshal(map[string]any{
				"error": 0,
				"transfer_info": []map[string]any{
					{"url": "https://t.com", "params": map[string]string{"a": "b"}},
				},
			})

			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader(resp)),
				Header:     http.Header{"Content-Type": {"application/json"}},
			}, nil
		}
		mt.handlers["https://t.com"] = func(r *http.Request) (*http.Response, error) {
			resp, _ := json.Marshal(map[string]any{"result": 1})

			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader(resp)),
				Header:     http.Header{"Content-Type": {"application/json"}},
			}, nil
		}

		ws := newMockedSession(mt)
		err := ws.Authenticate(t.Context(), pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_WebBrowser, "rt", "")
		assert.NoError(t, err)
		assert.True(t, ws.IsAuthenticated())
	})
}

func TestExecuteTransferWithRetry(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"result": 1})
		}))
		defer server.Close()

		ws := New(id.ID(0), log.Discard, server.Client())
		err := ws.executeTransfer(t.Context(), server.URL, nil)
		assert.NoError(t, err)
	})

	t.Run("retries", func(t *testing.T) {
		t.Parallel()

		var count int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if atomic.AddInt32(&count, 1) < 2 {
				hj, ok := w.(http.Hijacker)
				if ok {
					conn, _, _ := hj.Hijack()
					_ = conn.Close()
				}

				return
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"result": 1})
		}))
		defer server.Close()

		ws := New(id.ID(0), log.Discard, server.Client())
		ws.retryBackoff = time.Millisecond
		err := ws.executeTransfer(t.Context(), server.URL, nil)
		assert.NoError(t, err)
		assert.Equal(t, int32(2), atomic.LoadInt32(&count))
	})
}

func TestVerify(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		mt := setupMockTransport()
		mt.handlers[urlVerify] = func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		}
		ws := newMockedSession(mt)
		ws.isAuth = true
		ok, err := ws.Verify(t.Context())
		assert.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("redirect_to_login", func(t *testing.T) {
		t.Parallel()

		mt := setupMockTransport()
		mt.handlers[urlVerify] = func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 302,
				Header:     http.Header{"Location": []string{"https://steamcommunity.com/login/home/"}},
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    r,
			}, nil
		}
		ws := newMockedSession(mt)
		ws.isAuth = true
		ok, err := ws.Verify(t.Context())
		assert.NoError(t, err)
		assert.False(t, ok)
		assert.False(t, ws.IsAuthenticated())
	})

	t.Run("redirect_loop_error", func(t *testing.T) {
		t.Parallel()

		mt := setupMockTransport()
		redirectURL := "https://steamcommunity.com/chat/clientinterfaces"
		mt.handlers[redirectURL] = func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 302,
				Header:     http.Header{"Location": []string{redirectURL}},
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    r,
			}, nil
		}

		ws := newMockedSession(mt)
		req, err := http.NewRequestWithContext(t.Context(), "GET", redirectURL, nil)
		require.NoError(t, err)

		_, err = ws.Do(req)
		assert.ErrorContains(t, err, "stopped after 10 redirects")
	})

	t.Run("verify_failure_request", func(t *testing.T) {
		t.Parallel()

		mt := setupMockTransport()
		mt.handlers[urlVerify] = func(r *http.Request) (*http.Response, error) {
			return nil, errors.New("network error")
		}
		ws := newMockedSession(mt)
		ws.isAuth = true
		ok, err := ws.Verify(t.Context())
		assert.False(t, ok)
		assert.False(t, ws.IsAuthenticated())
		assert.NoError(t, err)
	})

	t.Run("verify_not_authenticated", func(t *testing.T) {
		t.Parallel()

		ws := newMockedSession(setupMockTransport())
		ok, err := ws.Verify(t.Context())
		assert.NoError(t, err)
		assert.False(t, ok)
	})
}

func TestSessionID(t *testing.T) {
	t.Parallel()

	t.Run("session_id_invalid_url", func(t *testing.T) {
		t.Parallel()

		ws := newMockedSession(setupMockTransport())
		assert.Empty(t, ws.SessionID(":%:invalid"))
	})

	t.Run("session_id_absent_cookie", func(t *testing.T) {
		t.Parallel()

		ws := newMockedSession(setupMockTransport())
		assert.Empty(t, ws.SessionID("https://steamcommunity.com"))
	})

	t.Run("session_id_present_cookie", func(t *testing.T) {
		t.Parallel()

		ws := newMockedSession(setupMockTransport())
		ws.seedCookies("mysessionid", "mysecure")
		assert.Equal(t, "mysessionid", ws.SessionID("https://steamcommunity.com"))
	})
}

func TestAuthenticate_SlowPath_Errors(t *testing.T) {
	t.Parallel()

	t.Run("slow_path_error_code", func(t *testing.T) {
		t.Parallel()

		mt := setupMockTransport()
		mt.handlers[urlFinalize] = func(r *http.Request) (*http.Response, error) {
			resp, _ := json.Marshal(map[string]any{
				"error": 42,
			})

			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader(resp)),
				Header:     http.Header{"Content-Type": {"application/json"}},
			}, nil
		}
		ws := newMockedSession(mt)
		ws.retryBackoff = time.Millisecond

		err := ws.Authenticate(t.Context(), pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_WebBrowser, "rt", "")
		assert.ErrorContains(t, err, "finalize login error code: 42")
	})

	t.Run("slow_path_network_failure", func(t *testing.T) {
		t.Parallel()

		mt := setupMockTransport()
		mt.handlers[urlFinalize] = func(r *http.Request) (*http.Response, error) {
			return nil, errors.New("network failure")
		}
		ws := newMockedSession(mt)
		ws.retryBackoff = time.Millisecond

		err := ws.Authenticate(t.Context(), pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_WebBrowser, "rt", "")
		assert.ErrorContains(t, err, "finalize login failed")
	})

	t.Run("slow_path_transfer_exhausted_failure", func(t *testing.T) {
		t.Parallel()

		mt := setupMockTransport()
		mt.handlers[urlFinalize] = func(r *http.Request) (*http.Response, error) {
			resp, _ := json.Marshal(map[string]any{
				"error": 0,
				"transfer_info": []map[string]any{
					{"url": "https://fail-transfer.com", "params": map[string]string{"a": "b"}},
				},
			})

			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader(resp)),
				Header:     http.Header{"Content-Type": {"application/json"}},
			}, nil
		}
		mt.handlers["https://fail-transfer.com"] = func(r *http.Request) (*http.Response, error) {
			return nil, errors.New("perma-fail")
		}
		ws := newMockedSession(mt)
		ws.retryBackoff = time.Millisecond

		err := ws.Authenticate(t.Context(), pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_WebBrowser, "rt", "")
		assert.ErrorContains(t, err, "perma-fail")
	})

	t.Run("transfer_non_ok_result", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"result": 2}) // EResult_Fail
		}))
		defer server.Close()

		ws := New(id.ID(0), log.Discard, server.Client())
		ws.retryBackoff = time.Millisecond

		err := ws.executeTransfer(t.Context(), server.URL, nil)
		assert.ErrorContains(t, err, "steam error: Fail")
	})
}
