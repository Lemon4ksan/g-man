// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package websession

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	log "github.com/lemon4ksan/foundation/async/logkit"
	"github.com/lemon4ksan/foundation/codec/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
	pb "github.com/lemon4ksan/g-man/protobuf/steam"
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

func (m *mockTransport) Do(req *http.Request) (*http.Response, error) {
	return m.RoundTrip(req)
}

func setupMockTransport() *mockTransport {
	return &mockTransport{handlers: make(map[string]func(r *http.Request) (*http.Response, error))}
}

func newMockedSession(transport *mockTransport) *WebSession {
	return New(testSteamID, log.Discard, transport)
}

func TestNew(t *testing.T) {
	t.Parallel()

	ws := New(testCustomSteamID, log.Discard, nil)

	require.NotNil(t, ws)
	assert.Equal(t, testCustomSteamID, ws.steamID)
	assert.NotNil(t, ws.httpClient)
	assert.NotNil(t, ws.jar)
	assert.False(t, ws.isAuth)
	assert.Len(t, ws.domains, len(DefaultDomains))
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

		var count atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if count.Add(1) < 2 {
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
		assert.Equal(t, int32(2), count.Load())
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
		assert.True(t, ws.IsAuthenticated())
		assert.ErrorContains(t, err, "network error")
	})

	t.Run("verify_unauthorized_clears_session", func(t *testing.T) {
		t.Parallel()

		mt := setupMockTransport()
		mt.handlers[urlVerify] = func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    r,
			}, nil
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

func TestRefreshAndAutoRefresh(t *testing.T) {
	t.Parallel()

	t.Run("refresh_without_prior_authenticate", func(t *testing.T) {
		t.Parallel()

		ws := newMockedSession(setupMockTransport())
		err := ws.Refresh(t.Context())
		assert.ErrorIs(t, err, ErrRefreshTokenRequired)
	})

	t.Run("refresh_success_fast_path", func(t *testing.T) {
		t.Parallel()

		ws := newMockedSession(setupMockTransport())
		ws.WithTokenRefresher(func(ctx context.Context, refreshToken string) (string, error) {
			return "access_token", nil
		})
		err := ws.Authenticate(
			t.Context(),
			pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient,
			"my_refresh_token",
			"access_token",
		)
		require.NoError(t, err)
		assert.True(t, ws.IsAuthenticated())

		err = ws.Refresh(t.Context())
		assert.NoError(t, err)
		assert.True(t, ws.IsAuthenticated())
	})

	t.Run("auto_refresh_lifecycle", func(t *testing.T) {
		t.Parallel()

		ws := newMockedSession(setupMockTransport())
		err := ws.Authenticate(
			t.Context(),
			pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient,
			"my_refresh_token",
			"access_token",
		)
		require.NoError(t, err)

		ws.StartAutoRefresh(t.Context(), 50*time.Millisecond)
		time.Sleep(120 * time.Millisecond)
		assert.True(t, ws.IsAuthenticated())
		ws.StopAutoRefresh()
	})
}

func TestWebSession_Refresh_FinalizeUnauthorized_NoInfiniteRecursion(t *testing.T) {
	t.Parallel()

	mt := setupMockTransport()
	mt.handlers[urlFinalize] = func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Body:       io.NopCloser(strings.NewReader(`{"error": "Unauthorized"}`)),
			Header:     http.Header{"Content-Type": {"application/json"}},
			Request:    r,
		}, nil
	}

	ws := newMockedSession(mt)
	ws.retryBackoff = time.Millisecond

	// Must fail cleanly with error and NOT crash the test process with stack overflow
	err := ws.Authenticate(
		t.Context(),
		pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_WebBrowser,
		"invalid_nonce",
		"",
	)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "finalize login failed")
	assert.False(t, ws.IsAuthenticated())
}

func TestWebSession_Refresh_RecursionCircuitBreaker(t *testing.T) {
	t.Parallel()

	ws := newMockedSession(setupMockTransport())
	ctx := context.WithValue(t.Context(), inRefreshKey, struct{}{})

	err := ws.Refresh(ctx)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "refresh recursion detected")
}

func TestAuthenticate_FastPath_WithTokenRefresher(t *testing.T) {
	t.Parallel()

	ws := newMockedSession(setupMockTransport())
	refresherCalled := false
	ws.WithTokenRefresher(func(ctx context.Context, refreshToken string) (string, error) {
		assert.Equal(t, "test_rt", refreshToken)

		refresherCalled = true

		return "acquired_at", nil
	})

	err := ws.Authenticate(t.Context(), pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient, "test_rt", "")
	require.NoError(t, err)
	assert.True(t, refresherCalled)
	assert.True(t, ws.IsAuthenticated())

	// Verify steamLoginSecure cookie was injected
	u, err := url.Parse("https://steamcommunity.com")
	require.NoError(t, err)

	var loginCookie *http.Cookie
	for _, c := range ws.Cookies(u) {
		if c.Name == cookieSteamLoginSecure {
			loginCookie = c
			break
		}
	}

	require.NotNil(t, loginCookie)
	assert.Contains(t, loginCookie.Value, "acquired_at")
}

func TestAuthenticate_FastPath_RefresherNil_ReturnsMissingTokenRefresherError(t *testing.T) {
	t.Parallel()

	ws := newMockedSession(setupMockTransport()) // tokenRefresher is nil

	err := ws.Authenticate(t.Context(), pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient, "test_rt", "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMissingTokenRefresher))
	assert.ErrorContains(t, err, "missing TokenRefresher for platform")
	assert.ErrorContains(t, err, "cannot use /jwt/finalizelogin")
	assert.False(t, ws.IsAuthenticated())
}

func TestAuthenticate_FastPath_MobileApp_RefresherNil_ReturnsMissingTokenRefresherError(t *testing.T) {
	t.Parallel()

	ws := newMockedSession(setupMockTransport())

	err := ws.Authenticate(t.Context(), pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_MobileApp, "test_rt", "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMissingTokenRefresher))
	assert.False(t, ws.IsAuthenticated())
}

func TestAuthenticate_FastPath_RefresherFails_ReturnsError(t *testing.T) {
	t.Parallel()

	ws := newMockedSession(setupMockTransport())
	ws.WithTokenRefresher(func(ctx context.Context, refreshToken string) (string, error) {
		return "", errors.New("cm socket rpc failure")
	})

	err := ws.Authenticate(t.Context(), pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient, "test_rt", "")
	require.Error(t, err)
	assert.ErrorContains(t, err, "cm socket rpc failure")
	assert.False(t, ws.IsAuthenticated())
}

func TestAuthenticate_FastPath_RefresherEmptyToken_ReturnsError(t *testing.T) {
	t.Parallel()

	ws := newMockedSession(setupMockTransport())
	ws.WithTokenRefresher(func(ctx context.Context, refreshToken string) (string, error) {
		return "", nil
	})

	err := ws.Authenticate(t.Context(), pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient, "test_rt", "")
	require.Error(t, err)
	assert.ErrorContains(t, err, "empty access token")
	assert.False(t, ws.IsAuthenticated())
}

func TestCookie_SessionID_HttpOnlyFalse(t *testing.T) {
	t.Parallel()

	u, err := url.Parse("https://steamcommunity.com")
	require.NoError(t, err)

	built := buildCookiesForDomain(u, "sess12345", "client12345", "secure12345")

	var sessCookie, clientCookie, secureCookie *http.Cookie
	for _, c := range built {
		switch c.Name {
		case cookieSessionID:
			sessCookie = c
		case cookieClientSessionID:
			clientCookie = c
		case cookieSteamLoginSecure:
			secureCookie = c
		}
	}

	require.NotNil(t, sessCookie, "sessionid cookie must be present")
	assert.False(t, sessCookie.HttpOnly, "sessionid must have HttpOnly: false for CSRF parity")
	assert.True(t, sessCookie.Secure, "sessionid must have Secure: true")
	assert.Equal(t, "/", sessCookie.Path)
	assert.Equal(t, "steamcommunity.com", sessCookie.Domain)

	require.NotNil(t, clientCookie, "clientsessionid cookie must be present")
	assert.False(t, clientCookie.HttpOnly, "clientsessionid must have HttpOnly: false")
	assert.True(t, clientCookie.Secure)
	assert.Equal(t, "/", clientCookie.Path)
	assert.Equal(t, "steamcommunity.com", clientCookie.Domain)

	require.NotNil(t, secureCookie, "steamLoginSecure cookie must be present")
	assert.True(t, secureCookie.HttpOnly, "steamLoginSecure must be HttpOnly: true")
	assert.True(t, secureCookie.Secure)
	assert.Equal(t, "/", secureCookie.Path)
	assert.Equal(t, http.SameSiteNoneMode, secureCookie.SameSite)
	assert.Equal(t, "steamcommunity.com", secureCookie.Domain)

	// Verify jar integration and session ID length
	ws := newMockedSession(setupMockTransport())
	err = ws.Authenticate(t.Context(), pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient, "rt", "at")
	require.NoError(t, err)

	sessID := ws.SessionID("https://steamcommunity.com")
	assert.Equal(t, 24, len(sessID), "sessionid must be 24 hex characters")

	// Verify Secure flag prevents cookies over plain HTTP
	httpURL, err := url.Parse("http://steamcommunity.com")
	require.NoError(t, err)
	assert.Empty(t, ws.Cookies(httpURL), "secure cookies must not be returned over insecure http://")
}

func TestCookie_ClientSessionID_FormattingAndAccessor(t *testing.T) {
	t.Parallel()

	ws := newMockedSession(setupMockTransport())
	err := ws.Authenticate(t.Context(), pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient, "rt", "at")
	require.NoError(t, err)

	u, err := url.Parse("https://store.steampowered.com")
	require.NoError(t, err)

	clientSessID := ws.ClientSessionID("https://store.steampowered.com")
	assert.Len(t, clientSessID, 16, "clientsessionid must be exactly 16 hex characters")

	cookies := ws.Cookies(u)

	var clientCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "clientsessionid" {
			clientCookie = c
			break
		}
	}

	require.NotNil(t, clientCookie)
	assert.Equal(t, clientSessID, clientCookie.Value)

	// Verify randomness across instances
	ws2 := newMockedSession(setupMockTransport())
	err = ws2.Authenticate(t.Context(), pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient, "rt", "at")
	require.NoError(t, err)
	assert.NotEqual(
		t,
		ws.ClientSessionID("https://store.steampowered.com"),
		ws2.ClientSessionID("https://store.steampowered.com"),
	)
}

func TestSteamLoginSecure_Format_EncodeURIComponentParity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		steamID     id.ID
		accessToken string
		expected    string
	}{
		{
			name:        "standard_jwt",
			steamID:     id.ID(76561197960265728),
			accessToken: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.payload.signature",
			expected:    "76561197960265728%7C%7CeyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.payload.signature",
		},
		{
			name:        "token_with_base64_padding",
			steamID:     id.ID(76561197960265728),
			accessToken: "abc==",
			expected:    "76561197960265728%7C%7Cabc%3D%3D",
		},
		{
			name:        "token_with_slashes_and_plus",
			steamID:     id.ID(76561197960265728),
			accessToken: "a+b/c",
			expected:    "76561197960265728%7C%7Ca%2Bb%2Fc",
		},
		{
			name:        "token_with_spaces_and_special_chars",
			steamID:     id.ID(76561197960265728),
			accessToken: "token space!~*()'",
			expected:    "76561197960265728%7C%7Ctoken%20space!~*()'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := FormatSteamLoginSecure(tt.steamID, tt.accessToken)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestCookie_MultiDomainSeeding_All5Domains(t *testing.T) {
	t.Parallel()

	ws := newMockedSession(setupMockTransport())
	err := ws.Authenticate(
		t.Context(),
		pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient,
		"rt",
		"my_token",
	)
	require.NoError(t, err)

	expectedDomains := []string{
		"https://steamcommunity.com",
		"https://store.steampowered.com",
		"https://help.steampowered.com",
		"https://login.steampowered.com",
		"https://s.team",
	}

	for _, raw := range expectedDomains {
		u, err := url.Parse(raw)
		require.NoError(t, err)

		cookies := ws.Cookies(u)
		require.NotEmpty(t, cookies, "cookies must be populated for %s", raw)

		names := make(map[string]*http.Cookie)
		for _, c := range cookies {
			names[c.Name] = c
		}

		assert.Contains(t, names, "sessionid", "sessionid missing on %s", raw)
		assert.Contains(t, names, "clientsessionid", "clientsessionid missing on %s", raw)
		assert.Contains(t, names, "steamLoginSecure", "steamLoginSecure missing on %s", raw)
	}
}

func TestCookie_SubdomainAccessibility(t *testing.T) {
	t.Parallel()

	ws := newMockedSession(setupMockTransport())
	err := ws.Authenticate(
		t.Context(),
		pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient,
		"rt",
		"my_token",
	)
	require.NoError(t, err)

	subdomains := []string{
		"https://api.steamcommunity.com/ISteamUser/v1",
		"https://broadcast.steamcommunity.com/broadcast",
		"https://checkout.steampowered.com/cart",
		"https://media.steampowered.com/steamcommunity/public",
	}

	for _, raw := range subdomains {
		u, err := url.Parse(raw)
		require.NoError(t, err)

		cookies := ws.Cookies(u)
		assert.NotEmpty(t, cookies, "subdomain %s must receive cookies", raw)

		names := make(map[string]bool)
		for _, c := range cookies {
			names[c.Name] = true
		}

		assert.True(t, names["sessionid"], "sessionid missing on subdomain %s", raw)
		assert.True(t, names["clientsessionid"], "clientsessionid missing on subdomain %s", raw)
		assert.True(t, names["steamLoginSecure"], "steamLoginSecure missing on subdomain %s", raw)
	}

	// Non-Steam domain must NOT receive cookies
	nonSteam, _ := url.Parse("https://example.com/test")
	assert.Empty(t, ws.Cookies(nonSteam), "external domains must not receive Steam cookies")
}

func TestWebSession_Refresh_SingleFlight_PreventsConcurrentThrashing(t *testing.T) {
	t.Parallel()

	var refresherCalls atomic.Int32

	ws := newMockedSession(setupMockTransport())
	ws.WithTokenRefresher(func(ctx context.Context, refreshToken string) (string, error) {
		refresherCalls.Add(1)
		time.Sleep(50 * time.Millisecond) // simulate CM socket round-trip
		return "fresh_token", nil
	})

	err := ws.Authenticate(
		t.Context(),
		pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient,
		"rt",
		"initial_token",
	)
	require.NoError(t, err)

	var wg sync.WaitGroup

	concurrentCount := 10
	wg.Add(concurrentCount)

	for range concurrentCount {
		go func() {
			defer wg.Done()

			err := ws.Refresh(t.Context())
			assert.NoError(t, err)
		}()
	}

	wg.Wait()
	assert.Equal(t, int32(1), refresherCalls.Load(), "TokenRefresher must be invoked exactly once via singleflight")
}

func TestWebSession_REST_ReAuth_SoftLogoutRedirectAndHTML(t *testing.T) {
	t.Parallel()

	t.Run("reauth_on_login_redirect", func(t *testing.T) {
		t.Parallel()

		mt := setupMockTransport()

		var callCount atomic.Int32

		targetURL := "https://steamcommunity.com/tradeoffer/1"
		mt.handlers[targetURL] = func(r *http.Request) (*http.Response, error) {
			if callCount.Add(1) == 1 {
				return &http.Response{
					StatusCode: http.StatusFound,
					Header:     http.Header{"Location": []string{"https://steamcommunity.com/login/home/"}},
					Body:       io.NopCloser(strings.NewReader("")),
				}, nil
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"status":"success"}`)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			}, nil
		}

		ws := newMockedSession(mt)
		ws.retryBackoff = time.Millisecond
		ws.WithTokenRefresher(func(ctx context.Context, refreshToken string) (string, error) {
			return "tok", nil
		})
		_ = ws.Authenticate(t.Context(), pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient, "rt", "tok")

		type okResp struct {
			Status string `json:"status"`
		}

		res, err := ws.REST().GetTo[okResp](t.Context(), targetURL)
		require.NoError(t, err)
		assert.Equal(t, "success", res.Status)
		assert.Equal(t, int32(2), callCount.Load())
	})

	t.Run("reauth_on_logged_in_false_body", func(t *testing.T) {
		t.Parallel()

		mt := setupMockTransport()

		var callCount atomic.Int32

		targetURL := "https://steamcommunity.com/market/priceoverview"
		mt.handlers[targetURL] = func(r *http.Request) (*http.Response, error) {
			if callCount.Add(1) == 1 {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"success":false,"Logged In":false}`)),
					Header:     http.Header{"Content-Type": []string{"application/json"}},
				}, nil
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"success":true,"lowest_price":"$2.50"}`)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			}, nil
		}

		ws := newMockedSession(mt)
		ws.retryBackoff = time.Millisecond
		ws.WithTokenRefresher(func(ctx context.Context, refreshToken string) (string, error) {
			return "tok", nil
		})
		_ = ws.Authenticate(t.Context(), pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient, "rt", "tok")

		type marketResp struct {
			Success     bool   `json:"success"`
			LowestPrice string `json:"lowest_price"`
		}

		res, err := ws.REST().GetTo[marketResp](t.Context(), targetURL)
		require.NoError(t, err)
		assert.True(t, res.Success)
		assert.Equal(t, "$2.50", res.LowestPrice)
		assert.Equal(t, int32(2), callCount.Load())
	})
}

func TestWebSession_Verify_SoftLogout(t *testing.T) {
	t.Parallel()

	t.Run("verify_returns_false_on_sign_in_title", func(t *testing.T) {
		t.Parallel()

		mt := setupMockTransport()
		mt.handlers[urlVerify] = func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("<html><head><title>Sign In</title></head></html>")),
				Header:     http.Header{"Content-Type": []string{"text/html"}},
			}, nil
		}

		ws := newMockedSession(mt)
		ws.isAuth = true
		ok, err := ws.Verify(t.Context())
		require.NoError(t, err)
		assert.False(t, ok)
		assert.False(t, ws.IsAuthenticated())
	})

	t.Run("verify_returns_false_on_login_redirect", func(t *testing.T) {
		t.Parallel()

		mt := setupMockTransport()
		mt.handlers[urlVerify] = func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": []string{"https://steamcommunity.com/login/home/?goto="}},
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		}

		ws := newMockedSession(mt)
		ws.isAuth = true
		ok, err := ws.Verify(t.Context())
		require.NoError(t, err)
		assert.False(t, ok)
		assert.False(t, ws.IsAuthenticated())
	})
}
