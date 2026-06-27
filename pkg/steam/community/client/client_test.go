// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client_test

import (
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

type mockSession struct{}

func (m *mockSession) SessionID(s string) string {
	return "test_session_id"
}

// newMockedClient is a helper to construct a client with a mocked REST service.
func newMockedClient(t *testing.T, mock *mock.ServiceMock) *client.Client {
	t.Helper()

	return client.New(nil, mock).WithREST(mock)
}

func TestNew_InitializesCorrectly(t *testing.T) {
	t.Parallel()

	mockHTTP := &http.Client{}
	logger := log.New(log.DefaultConfig(log.LevelDebug))
	rc := aoni.NewClient(mockHTTP)

	t.Run("Default Initialization", func(t *testing.T) {
		t.Parallel()

		c := client.New(mockHTTP, &mockSession{})
		require.NotNil(t, c)
		assert.Equal(t, "test_session_id", c.SessionID(client.BaseURL))
	})

	t.Run("With Logger Option", func(t *testing.T) {
		t.Parallel()

		c := client.New(mockHTTP, &mockSession{})
		require.NotNil(t, c)

		updated := c.WithLogger(logger)

		require.NotNil(t, updated)
		assert.NotSame(t, c, updated)
	})

	t.Run("With REST Option", func(t *testing.T) {
		t.Parallel()

		c := client.New(mockHTTP, &mockSession{})
		require.NotNil(t, c)

		updated := c.WithREST(rc)

		require.NotNil(t, updated)
		assert.Equal(t, rc, updated.Unwrap())
		assert.NotSame(t, c, updated)
	})
}

func TestClient_SessionID_VariousSessions_ReturnsExpectedID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		session client.SessionProvider
		want    string
	}{
		{
			name:    "with_session_func",
			session: &mockSession{},
			want:    "test_session_id",
		},
		{
			name:    "without_session_func",
			session: nil,
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := client.New(&http.Client{}, tt.session)
			assert.Equal(t, tt.want, c.SessionID("any_url"))
		})
	}
}

func TestClient_Request_VariousResponses_ReturnsExpected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		mockSetup    func(m *mock.ServiceMock)
		expectedBody string
		wantErr      bool
		expectedErr  error
		errorContent string
	}{
		{
			name: "success",
			mockSetup: func(m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"status": "ok"}`)),
					}, nil
				}
			},
			expectedBody: `{"status": "ok"}`,
		},
		{
			name: "underlying_client_error",
			mockSetup: func(m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return nil, errors.New("network failure")
				}
			},
			wantErr:      true,
			errorContent: "network failure",
		},
		{
			name: "response_body_read_error",
			mockSetup: func(m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(faultyReader{}),
					}, nil
				}
			},
			wantErr:      true,
			errorContent: "read error",
		},
		{
			name: "rate_limited",
			mockSetup: func(m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusTooManyRequests,
						Body:       io.NopCloser(strings.NewReader("")),
					}, nil
				}
			},
			wantErr:     true,
			expectedErr: client.ErrRateLimited,
		},
		{
			name: "internal_server_error",
			mockSetup: func(m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusInternalServerError,
						Body:       io.NopCloser(strings.NewReader("")),
					}, nil
				}
			},
			wantErr:      true,
			errorContent: "steam API error: message=Steam is down or in maintenance, status=500",
		},
		{
			name: "auth_redirect",
			mockSetup: func(m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusFound,
						Header:     http.Header{"Location": {"https://steamcom/login/rendercapcha"}},
						Body:       io.NopCloser(strings.NewReader("")),
					}, nil
				}
			},
			wantErr:     true,
			expectedErr: service.ErrSessionExpired,
		},
		{
			name: "family_view_restricted",
			mockSetup: func(m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusForbidden,
						Body: io.NopCloser(
							strings.NewReader(
								`<div id="parental_notice_instructions">Enter your PIN below to exit Family View.</div>`,
							),
						),
					}, nil
				}
			},
			wantErr:     true,
			expectedErr: client.ErrFamilyViewRestricted,
		},
		{
			name: "soft_auth_fail_g_steamID_false",
			mockSetup: func(m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader("var g_steamID = false;")),
					}, nil
				}
			},
			wantErr:     true,
			expectedErr: service.ErrSessionExpired,
		},
		{
			name: "soft_auth_fail_g_steamID_0",
			mockSetup: func(m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`var g_steamID = "0";`)),
					}, nil
				}
			},
			wantErr:     true,
			expectedErr: service.ErrSessionExpired,
		},
		{
			name: "soft_auth_fail_sign_in_title",
			mockSetup: func(m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader("<title>Sign In</title>")),
					}, nil
				}
			},
			wantErr:     true,
			expectedErr: service.ErrSessionExpired,
		},
		{
			name: "sorry_page_with_reason",
			mockSetup: func(m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body: io.NopCloser(
							strings.NewReader(`<h1>Sorry!</h1><h3>   You've made too many requests.   </h3>`),
						),
					}, nil
				}
			},
			wantErr:      true,
			errorContent: "steam API error: message=You've made too many requests., status=200",
		},
		{
			name: "sorry_page_without_reason",
			mockSetup: func(m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`<h1>Sorry!</h1><p>Other text</p>`)),
					}, nil
				}
			},
			wantErr:      true,
			errorContent: "steam API error: message=unknown steam community error (Sorry page), status=200",
		},
		{
			name: "trade_error_message",
			mockSetup: func(m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`<div id="error_msg">  Error (15)  </div>`)),
					}, nil
				}
			},
			wantErr:      true,
			errorContent: "steam API error: message=Error (15), status=200",
		},
		{
			name: "generic_bad_request",
			mockSetup: func(m *mock.ServiceMock) {
				m.OnRest = func(method, path string, body any) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusBadRequest,
						Body:       io.NopCloser(strings.NewReader("bad data")),
					}, nil
				}
			},
			wantErr:     true,
			expectedErr: service.NewSteamAPIError("bad data", http.StatusBadRequest, nil),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			mock := mock.NewServiceMock()
			if tt.mockSetup != nil {
				tt.mockSetup(mock)
			}

			client := newMockedClient(t, mock)

			resp, err := client.Request(ctx, http.MethodGet, "/test")
			if tt.wantErr {
				require.Error(t, err)

				if tt.errorContent != "" {
					assert.Contains(t, err.Error(), tt.errorContent)
				} else {
					assert.ErrorIs(t, err, tt.expectedErr)
				}

				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			t.Cleanup(func() {
				_ = resp.Body.Close()
			})

			respBody, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.JSONEq(t, tt.expectedBody, string(respBody))
		})
	}
}

func TestClient_GetOrRegisterAPIKey_VariousScenarios_ReturnsExpected(t *testing.T) {
	t.Parallel()

	t.Run("key_already_exists", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()

		mock := mock.NewServiceMock()
		client := newMockedClient(t, mock)
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

	t.Run("register_new_key_success", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()

		mock := mock.NewServiceMock()
		client := newMockedClient(t, mock)

		htmlWithForm := `<div><form id="register_form"></form></div>`
		htmlWithKey := `<div>Key: FEDCBA0987654321FEDCBA0987654321</div>`
		callCount := 0

		mock.OnRest = func(method, path string, body any) (*http.Response, error) {
			callCount++
			switch callCount {
			case 1:
				assert.Equal(t, http.MethodGet, method)
				assert.Equal(t, "dev/apikey", path)

				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(htmlWithForm)),
				}, nil

			case 2:
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

			case 3:
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

	t.Run("register_new_key_with_default_domain", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()

		mock := mock.NewServiceMock()
		c := newMockedClient(t, mock)

		callCount := 0
		mock.OnRest = func(method, path string, body any) (*http.Response, error) {
			callCount++

			if method == http.MethodGet && callCount == 1 {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`<form id="register_form"></form>`)),
				}, nil
			}

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

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`<html>No key here and no form</html>`)),
			}, nil
		}

		_, err := c.GetOrRegisterAPIKey(ctx, "")
		require.ErrorIs(t, err, client.ErrAPITokenNotFound)
	})

	t.Run("initial_page_fetch_fails", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()

		mock := mock.NewServiceMock()
		client := newMockedClient(t, mock)
		expectedErr := errors.New("network error")
		mock.OnRest = func(method, path string, body any) (*http.Response, error) {
			return nil, expectedErr
		}

		_, err := client.GetOrRegisterAPIKey(ctx, "test.com")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch apikey page")
	})

	t.Run("registration_request_fails", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()

		mock := mock.NewServiceMock()
		client := newMockedClient(t, mock)
		expectedErr := errors.New("post failed")

		mock.OnRest = func(method, path string, body any) (*http.Response, error) {
			if method == http.MethodGet {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`<form id="register_form"></form>`)),
				}, nil
			}

			return nil, expectedErr
		}

		_, err := client.GetOrRegisterAPIKey(ctx, "test.com")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "registration request failed")
	})

	t.Run("no_key_and_no_registration_form", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()

		mock := mock.NewServiceMock()
		c := newMockedClient(t, mock)
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

type mockHTTPDoer struct {
	doFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	return m.doFunc(req)
}

func TestClient_Request_ReplayableBody_Success(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	htmlContent := "var g_steamID = false;"
	httpClient := &mockHTTPDoer{
		doFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       aoni.AsReplayable(io.NopCloser(strings.NewReader(htmlContent))),
			}, nil
		},
	}

	c := client.New(httpClient, &mockSession{})

	httpClient.doFunc = func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       aoni.AsReplayable(io.NopCloser(strings.NewReader("var g_steamID = false;"))),
		}, nil
	}

	_, err := c.Request(ctx, http.MethodGet, "/test")
	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrSessionExpired)
}

func TestClient_Request_ReplayableBody_ReadError(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	httpClient := &mockHTTPDoer{
		doFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(faultyReader{}),
			}, nil
		},
	}

	c := client.New(httpClient, &customSessionProvider{})
	_, err := c.Request(ctx, http.MethodGet, "/test")
	require.Error(t, err)
	assert.EqualError(t, err, "read error")
}

func TestClient_Request_RedirectError_Intercept(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	mockService := mock.NewServiceMock()
	c := newMockedClient(t, mockService)

	mockService.OnRest = func(method, path string, body any) (*http.Response, error) {
		return nil, errors.New("stopped after 10 redirects (redirect loop detected)")
	}

	_, err := c.Request(ctx, http.MethodGet, "/test")
	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrSessionExpired)
	assert.Contains(t, err.Error(), "session expired during redirect loop")
}

func TestClient_Request_SessionExpired_Intercept(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	mockService := mock.NewServiceMock()
	c := newMockedClient(t, mockService)

	mockService.OnRest = func(method, path string, body any) (*http.Response, error) {
		return nil, errors.New("unauthorized: session expired")
	}

	_, err := c.Request(ctx, http.MethodGet, "/test")
	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrSessionExpired)
	assert.Contains(t, err.Error(), "session expired during redirect loop")
}

type customSessionProvider struct{}

func (c *customSessionProvider) SessionID(baseURL string) string {
	return "test_session"
}

func TestTruncateBody_VariousLengths_TruncatesCorrectly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		body     []byte
		maxLen   int
		expected string
	}{
		{
			name:     "short_body_not_truncated",
			body:     []byte("hello"),
			maxLen:   500,
			expected: "hello",
		},
		{
			name:     "exact_length_not_truncated",
			body:     []byte("12345"),
			maxLen:   5,
			expected: "12345",
		},
		{
			name:     "long_body_truncated",
			body:     []byte("this is a very long body that should be truncated"),
			maxLen:   10,
			expected: "this is a ...[truncated]",
		},
		{
			name:     "empty_body",
			body:     []byte{},
			maxLen:   500,
			expected: "",
		},
		{
			name:     "nil_body",
			body:     nil,
			maxLen:   500,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := client.TruncateBody(tt.body, tt.maxLen)
			if result != tt.expected {
				t.Errorf("truncateBody() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestCheckSteamErrors_VariousResponses_DetectsErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		header     http.Header
		body       []byte
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "rate_limit",
			statusCode: http.StatusTooManyRequests,
			header:     http.Header{},
			body:       []byte(""),
			wantErr:    true,
			errMsg:     "Rate limit exceeded",
		},
		{
			name:       "server_error",
			statusCode: http.StatusInternalServerError,
			header:     http.Header{},
			body:       []byte(""),
			wantErr:    true,
			errMsg:     "Steam is down or in maintenance",
		},
		{
			name:       "auth_redirect",
			statusCode: http.StatusFound,
			header:     http.Header{"Location": []string{"https://store.steampowered.com/login"}},
			body:       []byte(""),
			wantErr:    true,
			errMsg:     "Session expired",
		},
		{
			name:       "family_view",
			statusCode: http.StatusForbidden,
			header:     http.Header{},
			body: []byte(
				`<div id="parental_notice_instructions">Enter your PIN below to exit Family View.</div>`,
			),
			wantErr: true,
			errMsg:  "Family View enabled",
		},
		{
			name:       "soft_auth_failure_guest",
			statusCode: http.StatusOK,
			header:     http.Header{},
			body:       []byte(`g_steamID = false;`),
			wantErr:    true,
			errMsg:     "Session expired",
		},
		{
			name:       "sorry_page_with_reason",
			statusCode: http.StatusOK,
			header:     http.Header{},
			body:       []byte(`<h1>Sorry!</h1><div>Some content</div><h3>An error occurred</h3>`),
			wantErr:    true,
			errMsg:     "An error occurred",
		},
		{
			name:       "sorry_page_without_reason",
			statusCode: http.StatusOK,
			header:     http.Header{},
			body:       []byte(`<h1>Sorry!</h1><div>Some content without h3</div>`),
			wantErr:    true,
			errMsg:     "unknown steam community error (Sorry page)",
		},
		{
			name:       "bad_request_with_body_truncation",
			statusCode: http.StatusBadRequest,
			header:     http.Header{},
			body: []byte(
				"This is a very long error body that should definitely be truncated to prevent information leakage in logs and error messages. The body contains sensitive data that should not be exposed to users or logged in plain text. This repeated content ensures the body exceeds 500 characters for testing purposes. Adding more text here to make sure we cross the threshold and trigger the truncation behavior that we want to verify works correctly in production environments. This additional text is added to ensure we have enough characters to trigger the truncation logic.",
			),
			wantErr: true,
			errMsg:  "This additional text is added to en...[truncated]",
		},
		{
			name:       "success",
			statusCode: http.StatusOK,
			header:     http.Header{},
			body:       []byte(`{"success": true}`),
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := client.CheckSteamErrors(tt.statusCode, tt.header, tt.body)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkSteamErrors() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil {
				if tt.errMsg != "" {
					if !strings.Contains(err.Error(), tt.errMsg) {
						t.Errorf("checkSteamErrors() error message = %q, want to contain %q", err.Error(), tt.errMsg)
					}
				}
			}
		})
	}
}
