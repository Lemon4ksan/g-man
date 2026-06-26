// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/lemon4ksan/aoni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/community/client"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/test/mock"
)

// faultyReader is a reader that always returns an error.
type faultyReader struct{}

func (fr faultyReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("read error")
}

func TestNewClient(t *testing.T) {
	mockHTTP := &http.Client{}
	sessionFunc := func(s string) string { return "session" }

	t.Run("Default Initialization", func(t *testing.T) {
		c := client.New(mockHTTP, sessionFunc)
		require.NotNil(t, c)
		assert.Equal(t, "session", c.SessionID(client.BaseURL))
	})

	t.Run("WithLogger Option", func(t *testing.T) {
		logger := log.New(log.DefaultConfig(log.LevelDebug))
		// This test ensures the option can be applied without panicking.
		c := client.New(mockHTTP, sessionFunc, client.WithLogger(logger))
		require.NotNil(t, c)
	})

	t.Run("WithREST Option", func(t *testing.T) {
		rc := aoni.NewClient(mockHTTP)
		// This test ensures the option can be applied without panicking.
		c := client.New(mockHTTP, sessionFunc, client.WithREST(rc))
		require.NotNil(t, c)
	})
}

func TestClient_SessionID(t *testing.T) {
	t.Run("With Session Func", func(t *testing.T) {
		c := client.New(&http.Client{}, func(s string) string { return "test_session_id" })
		assert.Equal(t, "test_session_id", c.SessionID("any_url"))
	})

	t.Run("Without Session Func", func(t *testing.T) {
		c := client.New(&http.Client{}, nil)
		assert.Empty(t, c.SessionID("any_url"))
	})
}

func TestClient_Request(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		mock := mock.NewServiceMock()
		mock.OnRest = func(method, path string, body any) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"status": "ok"}`)),
			}, nil
		}
		client := client.New(nil, nil, client.WithREST(mock))

		resp, err := client.Request(ctx, http.MethodGet, "/test")
		require.NoError(t, err)

		require.NotNil(t, resp)
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(resp.Body)
		assert.JSONEq(t, `{"status": "ok"}`, string(respBody))
	})

	t.Run("Underlying Client Error", func(t *testing.T) {
		mock := mock.NewServiceMock()
		expectedErr := errors.New("network failure")
		mock.OnRest = func(method, path string, body any) (*http.Response, error) {
			return nil, expectedErr
		}
		client := client.New(nil, nil, client.WithREST(mock))

		_, err := client.Request(ctx, http.MethodGet, "/test")
		require.Error(t, err)
		assert.Equal(t, expectedErr, err)
	})

	t.Run("Response Body Read Error", func(t *testing.T) {
		mock := mock.NewServiceMock()
		mock.OnRest = func(method, path string, body any) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(faultyReader{}),
			}, nil
		}
		client := client.New(nil, nil, client.WithREST(mock))

		_, err := client.Request(ctx, http.MethodGet, "/test")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read error")
	})

	// Table-driven test for all Steam-specific error conditions
	errorTests := []struct {
		name         string
		response     *http.Response
		expectedErr  error
		errorContent string
	}{
		{
			name: "Rate Limited",
			response: &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Body:       io.NopCloser(strings.NewReader("")),
			},
			expectedErr: client.ErrRateLimited,
		},
		{
			name: "Internal Server Error",
			response: &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader("")),
			},
			errorContent: "steam API error: message=Steam is down or in maintenance, status=500",
		},
		{
			name: "Auth Redirect",
			response: &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": {"https://steamcom/login/rendercapcha"}},
				Body:       io.NopCloser(strings.NewReader("")),
			},
			expectedErr: service.ErrSessionExpired,
		},
		{
			name: "Family View Restricted",
			response: &http.Response{
				StatusCode: http.StatusForbidden,
				Body: io.NopCloser(
					strings.NewReader(
						`<div id="parental_notice_instructions">Enter your PIN below to exit Family View.</div>`,
					),
				),
			},
			expectedErr: client.ErrFamilyViewRestricted,
		},
		{
			name: "Soft Auth Fail (g_steamID = false)",
			response: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("var g_steamID = false;")),
			},
			expectedErr: service.ErrSessionExpired,
		},
		{
			name: "Soft Auth Fail (g_steamID = \"0\")",
			response: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`var g_steamID = "0";`)),
			},
			expectedErr: service.ErrSessionExpired,
		},
		{
			name: "Soft Auth Fail (Sign In Title)",
			response: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("<title>Sign In</title>")),
			},
			expectedErr: service.ErrSessionExpired,
		},
		{
			name: "Sorry Page with Reason",
			response: &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(
					strings.NewReader(`<h1>Sorry!</h1><h3>   You've made too many requests.   </h3>`),
				),
			},
			errorContent: "steam API error: message=You've made too many requests., status=200",
		},
		{
			name: "Sorry Page without Reason",
			response: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`<h1>Sorry!</h1><p>Other text</p>`)),
			},
			errorContent: "steam API error: message=unknown steam community error (Sorry page), status=200",
		},
		{
			name: "Trade Error Message",
			response: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`<div id="error_msg">  Error (15)  </div>`)),
			},
			errorContent: "steam API error: message=Error (15), status=200",
		},
		{
			name: "Generic Bad Request",
			response: &http.Response{
				StatusCode: http.StatusBadRequest,
				Body:       io.NopCloser(strings.NewReader("bad data")),
			},
			expectedErr: service.NewSteamAPIError("bad data", http.StatusBadRequest, nil),
		},
	}

	for _, tt := range errorTests {
		t.Run(tt.name, func(t *testing.T) {
			mock := mock.NewServiceMock()
			// The SUT reads and closes the body, so we need to ensure the mock can provide it again if needed.
			bodyBytes, _ := io.ReadAll(tt.response.Body)
			mock.OnRest = func(method, path string, body any) (*http.Response, error) {
				tt.response.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				return tt.response, nil
			}
			client := client.New(nil, nil, client.WithREST(mock))

			_, err := client.Request(ctx, http.MethodGet, "/test")
			require.Error(t, err)

			if tt.errorContent != "" {
				assert.EqualError(t, err, tt.errorContent)
			} else {
				assert.ErrorIs(t, err, tt.expectedErr)
			}
		})
	}
}

func TestClient_GetOrRegisterAPIKey(t *testing.T) {
	ctx := context.Background()

	t.Run("Key Already Exists", func(t *testing.T) {
		mock := mock.NewServiceMock()
		client := client.New(nil, nil, client.WithREST(mock))
		htmlWithKey := `<div><p>Key: 1234567890ABCDEF1234567890ABCDEF</p></div>`

		mock.OnRest = func(method, path string, body any) (*http.Response, error) {
			require.Equal(t, http.MethodGet, method)
			require.Equal(t, "dev/apikey", path)

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(htmlWithKey)),
			}, nil
		}

		key, err := client.GetOrRegisterAPIKey(ctx, "test.com")
		require.NoError(t, err)
		assert.Equal(t, "1234567890ABCDEF1234567890ABCDEF", key)
	})

	t.Run("Register New Key Success", func(t *testing.T) {
		mock := mock.NewServiceMock()
		client := client.New(nil, mock.SessionID, client.WithREST(mock), client.WithLogger(log.Discard))

		htmlWithForm := `<div><form id="register_form"></form></div>`
		htmlWithKey := `<div>Key: FEDCBA0987654321FEDCBA0987654321</div>`
		callCount := 0

		mock.OnRest = func(method, path string, body any) (*http.Response, error) {
			callCount++
			switch callCount {
			case 1: // First GET to fetch the form
				assert.Equal(t, http.MethodGet, method)
				assert.Equal(t, "dev/apikey", path)

				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(htmlWithForm)),
				}, nil

			case 2: // POST to register the key
				assert.Equal(t, http.MethodPost, method)
				assert.Equal(t, "dev/registerkey", path)

				var bodyStr string
				if body != nil {
					if b, ok := body.([]byte); ok {
						bodyStr = string(b)
					} else if s, ok := body.(string); ok {
						bodyStr = s
					}
				}

				vals, _ := url.ParseQuery(bodyStr)
				assert.Equal(t, "custom.com", vals.Get("domain"))
				assert.Equal(t, "agreed", vals.Get("agreeToTerms"))
				assert.Equal(t, "mock_session_id", vals.Get("sessionid"))

				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil

			case 3: // Second GET to retrieve the new key
				assert.Equal(t, http.MethodGet, method)
				assert.Equal(t, "dev/apikey", path)

				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(htmlWithKey)),
				}, nil

			default:
				return nil, fmt.Errorf("unexpected call count: %d", callCount)
			}
		}

		key, err := client.GetOrRegisterAPIKey(ctx, "custom.com")
		require.NoError(t, err)
		assert.Equal(t, "FEDCBA0987654321FEDCBA0987654321", key)
	})

	t.Run("Register New Key with Default Domain", func(t *testing.T) {
		mock := mock.NewServiceMock()
		c := client.New(nil, mock.SessionID, client.WithREST(mock), client.WithLogger(log.Discard))

		callCount := 0
		mock.OnRest = func(method, path string, body any) (*http.Response, error) {
			callCount++

			// 1. First GET: Return form to trigger registration
			if method == http.MethodGet && callCount == 1 {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`<form id="register_form"></form>`)),
				}, nil
			}

			// 2. The POST: Validate domain and return success
			if method == http.MethodPost {
				var bodyStr string
				if b, ok := body.([]byte); ok {
					bodyStr = string(b)
				} else {
					bodyStr = body.(string)
				}

				vals, _ := url.ParseQuery(bodyStr)
				assert.Equal(t, "localhost", vals.Get("domain"))

				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
			}

			// 3. Second GET (triggered by tail call): Return something WITHOUT the form
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`<html>No key here and no form</html>`)),
			}, nil
		}

		// Now it will return ErrAPITokenNotFound instead of looping forever
		_, err := c.GetOrRegisterAPIKey(ctx, "")
		require.ErrorIs(t, err, client.ErrAPITokenNotFound)
	})

	t.Run("Initial Page Fetch Fails", func(t *testing.T) {
		mock := mock.NewServiceMock()
		client := client.New(nil, nil, client.WithREST(mock))
		expectedErr := errors.New("network error")
		mock.OnRest = func(method, path string, body any) (*http.Response, error) {
			return nil, expectedErr
		}

		_, err := client.GetOrRegisterAPIKey(ctx, "test.com")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch apikey page")
	})

	t.Run("Registration Request Fails", func(t *testing.T) {
		mock := mock.NewServiceMock()
		client := client.New(nil, nil, client.WithREST(mock), client.WithLogger(log.Discard))
		expectedErr := errors.New("post failed")

		mock.OnRest = func(method, path string, body any) (*http.Response, error) {
			if method == http.MethodGet {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`<form id="register_form"></form>`)),
				}, nil
			}

			return nil, expectedErr // Fail on POST
		}

		_, err := client.GetOrRegisterAPIKey(ctx, "test.com")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "registration request failed")
	})

	t.Run("No Key and No Registration Form", func(t *testing.T) {
		mock := mock.NewServiceMock()
		c := client.New(nil, nil, client.WithREST(mock))
		htmlWithoutKeyOrForm := `<html><body><p>Your account is limited.</p></body></html>`

		mock.OnRest = func(method, path string, body any) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(htmlWithoutKeyOrForm)),
			}, nil
		}

		_, err := c.GetOrRegisterAPIKey(ctx, "test.com")
		require.Error(t, err)
		assert.Equal(t, client.ErrAPITokenNotFound, err)
	})
}
