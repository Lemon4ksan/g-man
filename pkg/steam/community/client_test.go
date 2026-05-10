// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package community_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/rest"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/test/requester"
)

// faultyReader is a reader that always returns an error.
type faultyReader struct{}

func (fr faultyReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("read error")
}

// mockHTTPDoer is a mock implementation of rest.HTTPDoer for isolated client tests.
type mockHTTPDoer struct {
	doFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	return m.doFunc(req)
}

// A simple struct for testing JSON responses.
type genericResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// A simple struct for testing request bodies/queries.
type genericRequest struct {
	Param1 string `url:"param1"`
	Param2 int    `url:"param2"`
}

func TestNewClient(t *testing.T) {
	mockHTTP := &http.Client{}
	sessionFunc := func(s string) string { return "session" }

	t.Run("Default Initialization", func(t *testing.T) {
		c := community.NewClient(mockHTTP, sessionFunc)
		require.NotNil(t, c)
		assert.Equal(t, "session", c.SessionID(community.BaseURL))
	})

	t.Run("WithLogger Option", func(t *testing.T) {
		logger := log.New(log.DefaultConfig(log.DebugLevel))
		// This test ensures the option can be applied without panicking.
		c := community.NewClient(mockHTTP, sessionFunc, community.WithLogger(logger))
		require.NotNil(t, c)
	})

	t.Run("WithREST Option", func(t *testing.T) {
		rc := rest.NewClient(mockHTTP)
		// This test ensures the option can be applied without panicking.
		c := community.NewClient(mockHTTP, sessionFunc, community.WithREST(rc))
		require.NotNil(t, c)
	})
}

func TestClient_WithRegistry(t *testing.T) {
	c1 := community.NewClient(&http.Client{}, nil)
	r1 := c1.Registry()
	require.NotNil(t, r1)

	r2 := api.NewUnmarshalRegistry()
	c2 := c1.WithRegistry(r2)

	// Assert the original client is unchanged
	assert.Same(t, r1, c1.Registry(), "Original client's registry should not change")
	// Assert the new client has the new registry
	assert.Same(t, r2, c2.Registry(), "New client should have the new registry")
}

func TestClient_SessionID(t *testing.T) {
	t.Run("With Session Func", func(t *testing.T) {
		c := community.NewClient(&http.Client{}, func(s string) string { return "test_session_id" })
		assert.Equal(t, "test_session_id", c.SessionID("any_url"))
	})

	t.Run("Without Session Func", func(t *testing.T) {
		c := community.NewClient(&http.Client{}, nil)
		assert.Empty(t, c.SessionID("any_url"))
	})
}

func TestClient_Request(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		mock := requester.New()
		mock.OnRest = func(method, path string, body []byte) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"status": "ok"}`)),
			}, nil
		}
		client := community.NewClient(nil, nil, community.WithREST(mock))

		resp, err := client.Request(ctx, http.MethodGet, "/test", nil, nil)
		require.NoError(t, err)

		require.NotNil(t, resp)
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(resp.Body)
		assert.JSONEq(t, `{"status": "ok"}`, string(respBody))
	})

	t.Run("Underlying Client Error", func(t *testing.T) {
		mock := requester.New()
		expectedErr := errors.New("network failure")
		mock.OnRest = func(method, path string, body []byte) (*http.Response, error) {
			return nil, expectedErr
		}
		client := community.NewClient(nil, nil, community.WithREST(mock))

		_, err := client.Request(ctx, http.MethodGet, "/test", nil, nil)
		require.Error(t, err)
		assert.Equal(t, expectedErr, err)
	})

	t.Run("Response Body Read Error", func(t *testing.T) {
		mock := requester.New()
		mock.OnRest = func(method, path string, body []byte) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(faultyReader{}),
			}, nil
		}
		client := community.NewClient(nil, nil, community.WithREST(mock))

		_, err := client.Request(ctx, http.MethodGet, "/test", nil, nil)
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
			expectedErr: community.ErrRateLimited,
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
			expectedErr: api.ErrSessionExpired,
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
			expectedErr: community.ErrFamilyViewRestricted,
		},
		{
			name: "Soft Auth Fail (g_steamID = false)",
			response: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("var g_steamID = false;")),
			},
			expectedErr: api.ErrSessionExpired,
		},
		{
			name: "Soft Auth Fail (g_steamID = \"0\")",
			response: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`var g_steamID = "0";`)),
			},
			expectedErr: api.ErrSessionExpired,
		},
		{
			name: "Soft Auth Fail (Sign In Title)",
			response: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("<title>Sign In</title>")),
			},
			expectedErr: api.ErrSessionExpired,
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
			expectedErr: api.NewSteamAPIError("bad data", http.StatusBadRequest, nil),
		},
	}

	for _, tt := range errorTests {
		t.Run(tt.name, func(t *testing.T) {
			mock := requester.New()
			// The SUT reads and closes the body, so we need to ensure the mock can provide it again if needed.
			bodyBytes, _ := io.ReadAll(tt.response.Body)
			mock.OnRest = func(method, path string, body []byte) (*http.Response, error) {
				tt.response.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				return tt.response, nil
			}
			client := community.NewClient(nil, nil, community.WithREST(mock))

			_, err := client.Request(ctx, http.MethodGet, "/test", nil, nil)
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
		mock := requester.New()
		client := community.NewClient(nil, nil, community.WithREST(mock))
		htmlWithKey := `<div><p>Key: 1234567890ABCDEF1234567890ABCDEF</p></div>`

		mock.OnRest = func(method, path string, body []byte) (*http.Response, error) {
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
		mock := requester.New()
		client := community.NewClient(nil, mock.SessionID, community.WithREST(mock), community.WithLogger(log.Discard))

		htmlWithForm := `<div><form id="register_form"></form></div>`
		htmlWithKey := `<div>Key: FEDCBA0987654321FEDCBA0987654321</div>`
		callCount := 0

		mock.OnRest = func(method, path string, body []byte) (*http.Response, error) {
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

				vals, _ := url.ParseQuery(string(body))
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
		mock := requester.New()
		client := community.NewClient(nil, mock.SessionID, community.WithREST(mock), community.WithLogger(log.Discard))

		callCount := 0
		mock.OnRest = func(method, path string, body []byte) (*http.Response, error) {
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
				vals, _ := url.ParseQuery(string(body))
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
		_, err := client.GetOrRegisterAPIKey(ctx, "")
		require.ErrorIs(t, err, community.ErrAPITokenNotFound)
	})

	t.Run("Initial Page Fetch Fails", func(t *testing.T) {
		mock := requester.New()
		client := community.NewClient(nil, nil, community.WithREST(mock))
		expectedErr := errors.New("network error")
		mock.OnRest = func(method, path string, body []byte) (*http.Response, error) {
			return nil, expectedErr
		}

		_, err := client.GetOrRegisterAPIKey(ctx, "test.com")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch apikey page")
	})

	t.Run("Registration Request Fails", func(t *testing.T) {
		mock := requester.New()
		client := community.NewClient(nil, nil, community.WithREST(mock), community.WithLogger(log.Discard))
		expectedErr := errors.New("post failed")

		mock.OnRest = func(method, path string, body []byte) (*http.Response, error) {
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
		mock := requester.New()
		client := community.NewClient(nil, nil, community.WithREST(mock))
		htmlWithoutKeyOrForm := `<html><body><p>Your account is limited.</p></body></html>`

		mock.OnRest = func(method, path string, body []byte) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(htmlWithoutKeyOrForm)),
			}, nil
		}

		_, err := client.GetOrRegisterAPIKey(ctx, "test.com")
		require.Error(t, err)
		assert.Equal(t, community.ErrAPITokenNotFound, err)
	})
}

func TestGet(t *testing.T) {
	ctx := context.Background()
	mock := requester.New()
	client := community.NewClient(nil, nil, community.WithREST(mock))
	respBody, _ := json.Marshal(genericResponse{Success: true, Message: "OK"})

	t.Run("Success", func(t *testing.T) {
		mock.OnRest = func(method, path string, body []byte) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(respBody))}, nil
		}
		resp, err := community.Get[genericResponse](ctx, client, "/test/get", genericRequest{Param1: "hi", Param2: 1})
		require.NoError(t, err)
		assert.Equal(t, true, resp.Success)
	})

	t.Run("Request Struct Conversion Error", func(t *testing.T) {
		_, err := community.Get[genericResponse](ctx, client, "/test/get", make(chan int))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported type")
	})

	t.Run("Unmarshal Error", func(t *testing.T) {
		mock.OnRest = func(method, path string, body []byte) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"success": "not-a-bool"}`)),
			}, nil
		}
		_, err := community.Get[genericResponse](ctx, client, "/test/get", nil)
		require.Error(t, err)
		assert.IsType(t, &json.UnmarshalTypeError{}, err)
	})
}

func TestGetHTML(t *testing.T) {
	ctx := context.Background()
	mock := requester.New()
	client := community.NewClient(nil, nil, community.WithREST(mock))

	t.Run("Success", func(t *testing.T) {
		html := "<html><body>Test</body></html>"
		mock.OnRest = func(method, path string, body []byte) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(html))}, nil
		}
		resp, err := community.GetHTML(ctx, client, "/test/html")
		require.NoError(t, err)
		assert.Equal(t, html, string(resp))
	})

	t.Run("Request Fails", func(t *testing.T) {
		expectedErr := errors.New("network error")
		mock.OnRest = func(method, path string, body []byte) (*http.Response, error) {
			return nil, expectedErr
		}
		_, err := community.GetHTML(ctx, client, "/test/html")
		require.Error(t, err)
		assert.Equal(t, expectedErr, err)
	})

	t.Run("Body Read Fails", func(t *testing.T) {
		mock.OnRest = func(method, path string, body []byte) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(faultyReader{})}, nil
		}
		_, err := community.GetHTML(ctx, client, "/test/html")
		require.Error(t, err)
		assert.EqualError(t, err, "read error")
	})
}

func TestPostForm(t *testing.T) {
	ctx := context.Background()
	mock := requester.New()
	client := community.NewClient(nil, mock.SessionID, community.WithREST(mock))
	respBody, _ := json.Marshal(genericResponse{Success: true})

	t.Run("Success with Request Struct", func(t *testing.T) {
		mock.OnRest = func(method, path string, body []byte) (*http.Response, error) {
			vals, err := url.ParseQuery(string(body))
			require.NoError(t, err)
			assert.Equal(t, "data", vals.Get("param1"))
			assert.Equal(t, "42", vals.Get("param2"))
			assert.Equal(t, "mock_session_id", vals.Get("sessionid"))

			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(respBody))}, nil
		}
		reqMsg := genericRequest{Param1: "data", Param2: 42}
		_, err := community.PostForm[genericResponse](ctx, client, "/test/post", reqMsg)
		require.NoError(t, err)
	})

	t.Run("Success without Request Struct", func(t *testing.T) {
		mock.OnRest = func(method, path string, body []byte) (*http.Response, error) {
			vals, err := url.ParseQuery(string(body))
			require.NoError(t, err)
			assert.Equal(t, "mock_session_id", vals.Get("sessionid"))

			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(respBody))}, nil
		}
		_, err := community.PostForm[genericResponse](ctx, client, "/test/post", nil)
		require.NoError(t, err)
	})

	t.Run("Struct Conversion Error", func(t *testing.T) {
		_, err := community.PostForm[genericResponse](ctx, client, "/test/post", make(chan int))
		require.Error(t, err)
	})
}

func TestPostJSON(t *testing.T) {
	ctx := context.Background()
	mock := requester.New()
	client := community.NewClient(nil, mock.SessionID, community.WithREST(mock))
	respBody, _ := json.Marshal(genericResponse{Success: true})

	t.Run("Success", func(t *testing.T) {
		mock.OnRest = func(method, path string, body []byte) (*http.Response, error) {
			var reqData genericRequest

			err := json.Unmarshal(body, &reqData)
			require.NoError(t, err)
			assert.Equal(t, "data", reqData.Param1)
			assert.Equal(t, 42, reqData.Param2)

			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(respBody))}, nil
		}
		reqMsg := genericRequest{Param1: "data", Param2: 42}
		_, err := community.PostJSON[genericResponse](ctx, client, "/test/post", reqMsg)
		require.NoError(t, err)
	})

	t.Run("JSON Marshal Error", func(t *testing.T) {
		type badJSON struct{ F func() }

		_, err := community.PostJSON[genericResponse](ctx, client, "/test/post", badJSON{})
		require.Error(t, err)
		assert.IsType(t, &json.UnsupportedTypeError{}, err)
	})
}

func TestPerformRequest(t *testing.T) {
	ctx := context.Background()

	t.Run("Applies Call Options", func(t *testing.T) {
		var receivedHeaders http.Header

		httpClient := &mockHTTPDoer{
			doFunc: func(req *http.Request) (*http.Response, error) {
				receivedHeaders = req.Header
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
			},
		}
		client := community.NewClient(httpClient, nil)
		_, err := community.Get[genericResponse](ctx, client, "/test", nil, api.WithHeader("X-Test-Header", "Value123"))
		require.NoError(t, err)
		assert.Equal(t, "Value123", receivedHeaders.Get("X-Test-Header"))
	})

	t.Run("Registry Fallback", func(t *testing.T) {
		// Define a requester that doesn't implement the registryProvider interface
		mock := requester.New()
		mock.OnRest = func(method, path string, body []byte) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"success": true}`))}, nil
		}
		req := customRequester{Requester: mock}

		// This call should not panic, indicating the registry fallback worked.
		_, err := community.Get[genericResponse](ctx, req, "/test", nil)
		require.NoError(t, err)
	})
}

type customRequester struct{ rest.Requester }

func (cr customRequester) SessionID(baseURL string) string { return "" }
