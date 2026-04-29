// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package websession

import (
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
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
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

	return nil, fmt.Errorf("mockTransport: нет обработчика для URL: %s", urlStr)
}

func setupMockTransport(t *testing.T) *mockTransport {
	mt := &mockTransport{handlers: make(map[string]func(r *http.Request) (*http.Response, error))}
	oldDefault := http.DefaultTransport
	http.DefaultTransport = mt
	t.Cleanup(func() {
		http.DefaultTransport = oldDefault
	})

	return mt
}

func newMockedSession(transport *mockTransport) *WebSession {
	ws := newTestSession()
	mockHTTPClient := &http.Client{Transport: transport}
	ws.client = rest.NewClient(mockHTTPClient)

	return ws
}

func newTestSession() *WebSession {
	steamID := id.ID(76561197960265728)
	return New(steamID, log.Discard)
}

func TestNew(t *testing.T) {
	steamID := id.ID(76561197960265729)

	ws := New(steamID, log.Discard)

	require.NotNil(t, ws, "New() should not return nil")
	assert.Equal(t, steamID, ws.steamID, "SteamID must be set correctly")
	assert.Equal(t, log.Discard, ws.logger, "Logger must be set correctly")
	assert.NotNil(t, ws.client, "HTTP client must not be nil")
	assert.NotNil(t, ws.jar, "Cookie jar must not be nil")
	assert.False(t, ws.isAuth, "isAuth must be false by default")
	assert.Equal(t, defaultDomains, ws.domains, "Domains must match default values")

	u, _ := url.Parse("https://steamcommunity.com")
	assert.Empty(t, ws.jar.Cookies(u), "Cookie jar must be empty after initialization")
}

func TestAddDomains(t *testing.T) {
	ws := newTestSession()
	initialDomainCount := len(ws.domains)
	newDomains := []string{"https://example.com", "https://test.com"}

	ws.AddDomains(newDomains...)

	assert.Len(t, ws.domains, initialDomainCount+len(newDomains), "The number of domains should increase")
	assert.Contains(t, ws.domains, "https://example.com", "A new domain should be added")
	assert.Contains(t, ws.domains, "https://test.com", "A new domain should be added")
}

func TestREST(t *testing.T) {
	ws := newTestSession()
	client := ws.REST()
	assert.NotNil(t, client, "Client() must not return nil")
	assert.Equal(t, ws.client, client, "Client() must return an internal HTTP client")
}

func TestHTTP(t *testing.T) {
	ws := newTestSession()
	httpClient := ws.HTTP()

	assert.NotNil(t, httpClient, "HTTP() не должен возвращать nil")
	assert.NotNil(t, httpClient.Jar, "Клиент должен иметь CookieJar")
	assert.Equal(t, ws.client.HTTP(), httpClient)
}

func TestIsAuthenticated(t *testing.T) {
	ws := newTestSession()
	assert.False(t, ws.IsAuthenticated(), "The session must not be initially authenticated")

	ws.mu.Lock()
	ws.isAuth = true
	ws.mu.Unlock()

	assert.True(t, ws.IsAuthenticated(), "The session must be authenticated after setting the flag")
}

func TestClear(t *testing.T) {
	ws := newTestSession()
	ws.isAuth = true
	oldJar := ws.jar

	u, _ := url.Parse("https://steamcommunity.com")
	ws.jar.SetCookies(u, []*http.Cookie{{Name: "test", Value: "value"}})

	ws.Clear()

	assert.False(t, ws.isAuth, "isAuth must be reset to false")
	assert.NotEqual(t, oldJar, ws.jar, "A new cookie jar must be created")
	assert.Empty(t, ws.jar.Cookies(u), "The cookie jar must be cleared")
}

func TestSessionID(t *testing.T) {
	ws := newTestSession()
	targetURL := "https://steamcommunity.com"

	assert.Empty(t, ws.SessionID(targetURL), "SessionID must be empty if the cookie is not set")

	assert.Empty(t, ws.SessionID(":%"), "SessionID must be empty for an invalid URL")

	sessionIDValue := "testsessionid123"
	u, _ := url.Parse(targetURL)
	ws.jar.SetCookies(u, []*http.Cookie{{Name: "sessionid", Value: sessionIDValue}})

	assert.Equal(t, sessionIDValue, ws.SessionID(targetURL), "SessionID must be retrieved correctly")

	ws.jar.SetCookies(u, []*http.Cookie{{Name: "othercookie", Value: "othervalue"}})
	assert.Equal(t, sessionIDValue, ws.SessionID(targetURL), "SessionID must be found among other cookies")
}

func TestAuthenticate(t *testing.T) {
	ctx := context.Background()

	t.Run("Error: Empty refresh token", func(t *testing.T) {
		ws := newTestSession()
		err := ws.Authenticate(ctx, pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_WebBrowser, "", "")
		require.Error(t, err)
		assert.Equal(t, "websession: refresh token is required", err.Error())
	})

	t.Run("Fast Path - SteamClient", func(t *testing.T) {
		ws := newTestSession()
		accessToken := "client_access_token_123"
		err := ws.Authenticate(
			ctx,
			pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_SteamClient,
			"some_refresh_token",
			accessToken,
		)

		require.NoError(t, err)
		assert.True(t, ws.IsAuthenticated())

		secureCookieValue := fmt.Sprintf("%d||%s", ws.steamID, accessToken)
		u, _ := url.Parse("https://store.steampowered.com")
		cookies := ws.jar.Cookies(u)
		assert.True(
			t,
			findCookie(cookies, "steamLoginSecure", secureCookieValue),
			"Cookie steamLoginSecure должен быть установлен",
		)
		assert.True(t, findCookie(cookies, "sessionid", ""), "The sessionid cookie must be set.")
	})

	t.Run("Fast Path - MobileApp", func(t *testing.T) {
		ws := newTestSession()
		accessToken := "mobile_access_token_456"
		err := ws.Authenticate(
			ctx,
			pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_MobileApp,
			"some_refresh_token",
			accessToken,
		)

		require.NoError(t, err)
		assert.True(t, ws.IsAuthenticated())

		secureCookieValue := fmt.Sprintf("%d||%s", ws.steamID, accessToken)
		u, _ := url.Parse("https://steamcommunity.com")
		cookies := ws.jar.Cookies(u)
		assert.True(
			t,
			findCookie(cookies, "steamLoginSecure", secureCookieValue),
			"The steamLoginSecure cookie must be set.",
		)
	})

	t.Run("Slow Path - Success", func(t *testing.T) {
		mt := setupMockTransport(t)

		const transferURL = "https://community.steam-mock.com/transfer"

		mt.handlers["https://login.steampowered.com/jwt/finalizelogin"] = func(r *http.Request) (*http.Response, error) {
			resp := map[string]any{
				"error": 0,
				"transfer_info": []map[string]any{
					{"url": transferURL, "params": map[string]string{"nonce": "123"}},
				},
			}
			b, _ := json.Marshal(resp)

			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(string(b))),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			}, nil
		}

		mt.handlers[transferURL] = func(r *http.Request) (*http.Response, error) {
			b, _ := json.Marshal(map[string]any{"result": enums.EResult_OK})

			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(string(b))),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			}, nil
		}

		ws := newTestSession()
		err := ws.Authenticate(ctx, pb.EAuthTokenPlatformType_k_EAuthTokenPlatformType_WebBrowser, "token", "")

		require.NoError(t, err)
		assert.True(t, ws.IsAuthenticated())
	})

	t.Run("Slow Path - API finalize error", func(t *testing.T) {
		transport := &mockTransport{
			handlers: map[string]func(r *http.Request) (*http.Response, error){
				"https://login.steampowered.com/jwt/finalizelogin": func(r *http.Request) (*http.Response, error) {
					response := map[string]any{"error": 1, "transfer_info": []any{}}
					body, _ := json.Marshal(response)

					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(string(body))),
						Header:     http.Header{"Content-Type": []string{"application/json"}},
					}, nil
				},
			},
		}

		ws := newMockedSession(transport)
		err := ws.authSlowPath(ctx, "token", "sessionid")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "finalize login error code: 1")
		assert.False(t, ws.IsAuthenticated())
	})

	t.Run("Slow Path - finalize HTTP error", func(t *testing.T) {
		transport := &mockTransport{
			handlers: map[string]func(r *http.Request) (*http.Response, error){
				"https://login.steampowered.com/jwt/finalizelogin": func(r *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusInternalServerError,
						Body:       io.NopCloser(strings.NewReader("Internal Server Error")),
					}, nil
				},
			},
		}

		ws := newMockedSession(transport)
		err := ws.authSlowPath(ctx, "token", "sessionid")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "finalize login failed")
		assert.False(t, ws.IsAuthenticated())
	})

	t.Run("Slow Path - Transfer failure", func(t *testing.T) {
		mt := setupMockTransport(t)

		mt.handlers["https://login.steampowered.com/jwt/finalizelogin"] = func(r *http.Request) (*http.Response, error) {
			resp := map[string]any{
				"error": 0,
				"transfer_info": []map[string]any{
					{"url": "https://fail-transfer.com", "params": map[string]string{}},
				},
			}
			b, _ := json.Marshal(resp)

			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(string(b))),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			}, nil
		}

		mt.handlers["https://fail-transfer.com"] = func(r *http.Request) (*http.Response, error) {
			b, _ := json.Marshal(map[string]any{"result": enums.EResult_InvalidPassword})

			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(string(b))),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			}, nil
		}

		ws := newTestSession()
		err := ws.authSlowPath(context.Background(), "token", "sessionid")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "transfer failed for")
		assert.Contains(t, err.Error(), "steam error: InvalidPassword")
		assert.False(t, ws.IsAuthenticated())
	})
}

func TestExecuteTransferWithRetry(t *testing.T) {
	ctx := context.Background()

	t.Run("Successful on the first try", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"result": enums.EResult_OK})
		}))
		defer server.Close()

		ws := newTestSession()
		err := ws.executeTransferWithRetry(ctx, rest.NewClient(server.Client()), server.URL, nil)
		require.NoError(t, err)
	})

	t.Run("Successful after several attempts", func(t *testing.T) {
		var attempt int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if atomic.AddInt32(&attempt, 1) < 3 {
				http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"result": enums.EResult_OK})
		}))
		defer server.Close()

		ws := newTestSession()
		err := ws.executeTransferWithRetry(ctx, rest.NewClient(server.Client()), server.URL, nil)
		require.NoError(t, err)
		assert.Equal(t, int32(3), atomic.LoadInt32(&attempt), "There must be 3 attempts")
	})

	t.Run("Failure after all attempts", func(t *testing.T) {
		var attempts int

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++

			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}))
		defer server.Close()

		ws := newTestSession()
		err := ws.executeTransferWithRetry(ctx, rest.NewClient(server.Client()), server.URL, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "after 3 retries")
		assert.Equal(t, 3, attempts, "Должно быть ровно 3 попытки")
	})

	t.Run("Result not ok", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"result": enums.EResult_AccessDenied})
		}))
		defer server.Close()

		ws := newTestSession()
		err := ws.executeTransferWithRetry(context.Background(), ws.client, server.URL, nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "steam error: AccessDenied")
	})
}

func TestVerify(t *testing.T) {
	ctx := context.Background()
	verifyURL := "https://steamcommunity.com/chat/clientinterfaces"

	t.Run("Not authenticated", func(t *testing.T) {
		ws := newTestSession()
		ok, err := ws.Verify(ctx)
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("Successful verification", func(t *testing.T) {
		mt := setupMockTransport(t)
		mt.handlers[verifyURL] = func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    r,
			}, nil
		}

		ws := newTestSession()
		ws.mu.Lock()
		ws.isAuth = true
		ws.mu.Unlock()

		ok, err := ws.Verify(ctx)
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("Verification failed - redirect", func(t *testing.T) {
		mt := setupMockTransport(t)
		mt.handlers[verifyURL] = func(r *http.Request) (*http.Response, error) {
			u, _ := url.Parse("https://steamcommunity.com/login/home/")

			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    &http.Request{URL: u},
			}, nil
		}

		ws := newTestSession()
		ws.isAuth = true

		ok, err := ws.Verify(ctx)
		require.NoError(t, err)
		assert.False(t, ok)
		assert.False(t, ws.IsAuthenticated())
	})

	t.Run("Verification failure - not status 200", func(t *testing.T) {
		transport := &mockTransport{
			handlers: map[string]func(r *http.Request) (*http.Response, error){
				verifyURL: func(r *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusForbidden,
						Body:       io.NopCloser(strings.NewReader("")),
						Request:    r,
					}, nil
				},
			},
		}
		ws := newMockedSession(transport)
		ws.isAuth = true

		ok, err := ws.Verify(ctx)
		require.NoError(t, err)
		assert.False(t, ok)
		assert.False(t, ws.IsAuthenticated(), "The session must be cleared after unsuccessful verification.")
	})

	t.Run("NetworkError", func(t *testing.T) {
		mt := setupMockTransport(t)
		mt.handlers["https://steamcommunity.com/chat/clientinterfaces"] = func(r *http.Request) (*http.Response, error) {
			return nil, errors.New("network unreachable")
		}

		ws := newTestSession()
		ws.mu.Lock()
		ws.isAuth = true
		ws.mu.Unlock()

		ok, err := ws.Verify(context.Background())
		assert.False(t, ok)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "verify request failed")
	})
}

func TestGenerateSessionID(t *testing.T) {
	sessionID := generateSessionID()
	assert.Len(t, sessionID, 24, "SessionID must be 24 characters long")
	assert.Regexp(t, `^[0-9a-f]{24}$`, sessionID, "SessionID must be a valid hex string")
}

func findCookie(cookies []*http.Cookie, name, value string) bool {
	for _, c := range cookies {
		if c.Name == name {
			if value == "" {
				return true
			}

			if strings.Contains(c.Value, value) {
				return true
			}
		}
	}

	return false
}
