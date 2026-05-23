// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package websession

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/rest"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
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
	steamID := id.ID(76561197960265728)
	return New(steamID, log.Discard, rest.DoerFunc(transport.RoundTrip))
}

func TestNew(t *testing.T) {
	steamID := id.ID(76561197960265729)
	ws := New(steamID, log.Discard, nil)

	require.NotNil(t, ws)
	assert.Equal(t, steamID, ws.steamID)
	assert.NotNil(t, ws.httpClient)
	assert.NotNil(t, ws.jar)
	assert.False(t, ws.isAuth)
	assert.Equal(t, defaultDomains, ws.domains)
}

func TestAddDomains(t *testing.T) {
	ws := newMockedSession(setupMockTransport())
	initialCount := len(ws.domains)
	ws.AddDomains("https://example.com")
	assert.Len(t, ws.domains, initialCount+1)
}

func TestREST(t *testing.T) {
	ws := newMockedSession(setupMockTransport())
	assert.NotNil(t, ws.REST())
}

func TestHTTP(t *testing.T) {
	ws := newMockedSession(setupMockTransport())
	assert.NotNil(t, ws.HTTP())
	assert.NotNil(t, ws.HTTP().Jar)
}

func TestIsAuthenticated(t *testing.T) {
	ws := newMockedSession(setupMockTransport())
	assert.False(t, ws.IsAuthenticated())
	ws.mu.Lock()
	ws.isAuth = true
	ws.mu.Unlock()
	assert.True(t, ws.IsAuthenticated())
}

func TestClear(t *testing.T) {
	ws := newMockedSession(setupMockTransport())
	ws.isAuth = true
	ws.Clear()
	assert.False(t, ws.isAuth)
}

func TestAuthenticate(t *testing.T) {
	ctx := context.Background()

	t.Run("Empty refresh token", func(t *testing.T) {
		ws := newMockedSession(setupMockTransport())
		err := ws.Authenticate(ctx, pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_WebBrowser, "", "")
		assert.Error(t, err)
	})

	t.Run("Fast Path", func(t *testing.T) {
		ws := newMockedSession(setupMockTransport())
		err := ws.Authenticate(ctx, pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient, "rt", "at")
		assert.NoError(t, err)
		assert.True(t, ws.IsAuthenticated())
	})

	t.Run("Slow Path Success", func(t *testing.T) {
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
		err := ws.Authenticate(ctx, pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_WebBrowser, "rt", "")
		assert.NoError(t, err)
		assert.True(t, ws.IsAuthenticated())
	})
}

func TestExecuteTransferWithRetry(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"result": 1})
		}))
		defer server.Close()

		ws := New(id.ID(0), log.Discard, server.Client())
		err := ws.executeTransferWithRetry(ctx, server.URL, nil)
		assert.NoError(t, err)
	})

	t.Run("Retries", func(t *testing.T) {
		var count int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if atomic.AddInt32(&count, 1) < 2 {
				http.Error(w, "fail", 500)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"result": 1})
		}))
		defer server.Close()

		ws := New(id.ID(0), log.Discard, server.Client())
		err := ws.executeTransferWithRetry(ctx, server.URL, nil)
		assert.NoError(t, err)
		assert.Equal(t, int32(2), atomic.LoadInt32(&count))
	})
}

func TestVerify(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		mt := setupMockTransport()
		mt.handlers[urlVerify] = func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		}
		ws := newMockedSession(mt)
		ws.isAuth = true
		ok, err := ws.Verify(ctx)
		assert.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("Redirect to login", func(t *testing.T) {
		mt := setupMockTransport()
		mt.handlers[urlVerify] = func(r *http.Request) (*http.Response, error) {
			u, _ := url.Parse("https://steamcommunity.com/login/home/")

			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    &http.Request{URL: u},
			}, nil
		}
		ws := newMockedSession(mt)
		ws.isAuth = true
		ok, err := ws.Verify(ctx)
		assert.NoError(t, err)
		assert.False(t, ok)
		assert.False(t, ws.IsAuthenticated())
	})

	t.Run("Verify failure request", func(t *testing.T) {
		mt := setupMockTransport()
		mt.handlers[urlVerify] = func(r *http.Request) (*http.Response, error) {
			return nil, errors.New("network error")
		}
		ws := newMockedSession(mt)
		ws.isAuth = true
		ok, err := ws.Verify(ctx)
		assert.Error(t, err)
		assert.False(t, ok)
	})

	t.Run("Verify not authenticated", func(t *testing.T) {
		ws := newMockedSession(setupMockTransport())
		ok, err := ws.Verify(ctx)
		assert.NoError(t, err)
		assert.False(t, ok)
	})
}

func TestSessionID(t *testing.T) {
	t.Run("SessionID invalid URL", func(t *testing.T) {
		ws := newMockedSession(setupMockTransport())
		assert.Empty(t, ws.SessionID(":%:invalid"))
	})

	t.Run("SessionID absent cookie", func(t *testing.T) {
		ws := newMockedSession(setupMockTransport())
		assert.Empty(t, ws.SessionID("https://steamcommunity.com"))
	})

	t.Run("SessionID present cookie", func(t *testing.T) {
		ws := newMockedSession(setupMockTransport())
		ws.seedCookies("mysessionid", "mysecure")
		assert.Equal(t, "mysessionid", ws.SessionID("https://steamcommunity.com"))
	})
}

func TestAuthenticate_SlowPath_Errors(t *testing.T) {
	ctx := context.Background()

	t.Run("Slow Path Error Code", func(t *testing.T) {
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
		err := ws.Authenticate(ctx, pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_WebBrowser, "rt", "")
		assert.ErrorContains(t, err, "finalize login error code: 42")
	})

	t.Run("Slow Path Network Failure", func(t *testing.T) {
		mt := setupMockTransport()
		mt.handlers[urlFinalize] = func(r *http.Request) (*http.Response, error) {
			return nil, errors.New("network failure")
		}
		ws := newMockedSession(mt)
		err := ws.Authenticate(ctx, pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_WebBrowser, "rt", "")
		assert.ErrorContains(t, err, "finalize login failed")
	})

	t.Run("Slow Path Transfer Exhausted Failure", func(t *testing.T) {
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
		err := ws.Authenticate(ctx, pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_WebBrowser, "rt", "")
		assert.ErrorContains(t, err, "perma-fail")
	})

	t.Run("Transfer Non-OK Result", func(t *testing.T) {
		ctx := context.Background()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"result": 2}) // EResult_Fail
		}))
		defer server.Close()

		ws := New(id.ID(0), log.Discard, server.Client())
		err := ws.executeTransferWithRetry(ctx, server.URL, nil)
		assert.ErrorContains(t, err, "steam error: Fail")
	})
}
