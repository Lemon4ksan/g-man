// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package community_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/lemon4ksan/aoni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/test/mock"
)

// mockHTTPDoer is a mock implementation of aoni.HTTPDoer for isolated client tests.
type mockHTTPDoer struct {
	doFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	return m.doFunc(req)
}

type customRequester struct{ aoni.Requester }

func (cr customRequester) SessionID(baseURL string) string { return "" }

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

// faultyReader is a reader that always returns an error.
type faultyReader struct{}

func (fr faultyReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("read error")
}

func TestGet(t *testing.T) {
	ctx := context.Background()
	mock := mock.NewServiceMock()
	client := community.NewClient(nil, nil, community.WithREST(mock))
	respBody, _ := json.Marshal(genericResponse{Success: true, Message: "OK"})

	t.Run("Success", func(t *testing.T) {
		mock.OnRest = func(method, path string, body any) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(respBody))}, nil
		}
		resp, err := community.Get[genericResponse](ctx, client, "/test/get", genericRequest{Param1: "hi", Param2: 1})
		require.NoError(t, err)
		assert.Equal(t, true, resp.Success)
	})

	t.Run("Request Struct Conversion Error", func(t *testing.T) {
		resp, err := community.Get[genericResponse](ctx, client, "/test/get", make(chan int))
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("Unmarshal Error", func(t *testing.T) {
		mock.OnRest = func(method, path string, body any) (*http.Response, error) {
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
	mock := mock.NewServiceMock()
	client := community.NewClient(nil, nil, community.WithREST(mock))

	t.Run("Success", func(t *testing.T) {
		html := "<html><body>Test</body></html>"
		mock.OnRest = func(method, path string, body any) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(html))}, nil
		}
		resp, err := community.GetHTML(ctx, client, "/test/html")
		require.NoError(t, err)

		bodyBytes, _ := io.ReadAll(resp)
		assert.Equal(t, html, string(bodyBytes))
	})

	t.Run("Request Fails", func(t *testing.T) {
		expectedErr := errors.New("network error")
		mock.OnRest = func(method, path string, body any) (*http.Response, error) {
			return nil, expectedErr
		}
		_, err := community.GetHTML(ctx, client, "/test/html")
		require.Error(t, err)
		assert.Equal(t, expectedErr, err)
	})

	t.Run("Body Read Fails", func(t *testing.T) {
		mock.OnRest = func(method, path string, body any) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(faultyReader{})}, nil
		}
		_, err := community.GetHTML(ctx, client, "/test/html")
		require.Error(t, err)
		assert.EqualError(t, err, "read error")
	})
}

func TestPostForm(t *testing.T) {
	ctx := context.Background()
	mock := mock.NewServiceMock()
	client := community.NewClient(nil, mock.SessionID, community.WithREST(mock))
	respBody, _ := json.Marshal(genericResponse{Success: true})

	t.Run("Success with Request Struct", func(t *testing.T) {
		mock.OnRest = func(method, path string, body any) (*http.Response, error) {
			var bodyStr string
			if b, ok := body.([]byte); ok {
				bodyStr = string(b)
			} else {
				bodyStr = body.(string)
			}

			vals, err := url.ParseQuery(bodyStr)
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
		mock.OnRest = func(method, path string, body any) (*http.Response, error) {
			var bodyStr string
			if b, ok := body.([]byte); ok {
				bodyStr = string(b)
			} else {
				bodyStr = body.(string)
			}

			vals, err := url.ParseQuery(bodyStr)
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
	mock := mock.NewServiceMock()
	client := community.NewClient(nil, mock.SessionID, community.WithREST(mock))
	respBody, _ := json.Marshal(genericResponse{Success: true})

	t.Run("Success", func(t *testing.T) {
		mock.OnRest = func(method, path string, body any) (*http.Response, error) {
			var reqData genericRequest

			err := json.Unmarshal(body.([]byte), &reqData)
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
		_, err := community.Get[genericResponse](
			ctx,
			client,
			"/test",
			nil,
			aoni.WithHeader("X-Test-Header", "Value123"),
		)
		require.NoError(t, err)
		assert.Equal(t, "Value123", receivedHeaders.Get("X-Test-Header"))
	})

	t.Run("Registry Fallback", func(t *testing.T) {
		// Define a requester that doesn't implement the registryProvider interface
		mock := mock.NewServiceMock()
		mock.OnRest = func(method, path string, body any) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"success": true}`))}, nil
		}
		req := customRequester{Requester: mock}

		// This call should not panic, indicating the registry fallback worked.
		_, err := community.Get[genericResponse](ctx, req, "/test", nil)
		require.NoError(t, err)
	})
}
