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

func (cr customRequester) GetOrRegisterAPIKey(ctx context.Context, domain string) (string, error) {
	return "", nil
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

// faultyReader is a reader that always returns an error.
type faultyReader struct{}

func (fr faultyReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("read error")
}

func TestDecorate(t *testing.T) {
	t.Parallel()

	t.Run("empty_modifiers", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		dec := community.Decorate(mockSvc)
		assert.Equal(t, mockSvc, dec)
	})

	t.Run("applies_default_and_runtime_modifiers", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()

		var defaultModCalled bool

		defaultMod := func(req *http.Request) {
			defaultModCalled = true
		}

		dec := community.Decorate(mockSvc, defaultMod)

		var runtimeModCalled bool

		runtimeMod := func(req *http.Request) {
			runtimeModCalled = true
		}

		mockSvc.OnRest = func(method, path string, body any) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil
		}

		_, err := dec.Request(t.Context(), "GET", "/test/decorate", runtimeMod)
		require.NoError(t, err)
		assert.True(t, defaultModCalled)
		assert.True(t, runtimeModCalled)
	})
}

func TestGet(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		client := community.NewClient(nil, nil).WithREST(mockSvc)
		respBody, _ := json.Marshal(genericResponse{Success: true, Message: "OK"})

		mockSvc.OnRest = func(method, path string, body any) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(respBody))}, nil
		}
		resp, err := community.Get[genericResponse](
			t.Context(),
			client,
			"/test/get",
			genericRequest{Param1: "hi", Param2: 1},
		)
		require.NoError(t, err)
		assert.Equal(t, true, resp.Success)
	})

	t.Run("request_struct_conversion_error", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		client := community.NewClient(nil, nil).WithREST(mockSvc)

		resp, err := community.Get[genericResponse](t.Context(), client, "/test/get", make(chan int))
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("request_failure", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		client := community.NewClient(nil, nil).WithREST(mockSvc)

		mockSvc.OnRest = func(method, path string, body any) (*http.Response, error) {
			return nil, errors.New("get request failed")
		}
		_, err := community.Get[genericResponse](t.Context(), client, "/test/get", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "get request failed")
	})

	t.Run("unmarshal_error", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		client := community.NewClient(nil, nil).WithREST(mockSvc)

		mockSvc.OnRest = func(method, path string, body any) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"success": "not-a-bool"}`)),
			}, nil
		}
		_, err := community.Get[genericResponse](t.Context(), client, "/test/get", nil)
		require.Error(t, err)
		assert.IsType(t, &json.UnmarshalTypeError{}, err)
	})
}

func TestGetHTML(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		client := community.NewClient(nil, nil).WithREST(mockSvc)

		html := "<html><body>Test</body></html>"
		mockSvc.OnRest = func(method, path string, body any) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(html))}, nil
		}
		resp, err := community.GetHTML(t.Context(), client, "/test/html")
		require.NoError(t, err)

		bodyBytes, _ := io.ReadAll(resp)
		assert.Equal(t, html, string(bodyBytes))
	})

	t.Run("request_fails", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		client := community.NewClient(nil, nil).WithREST(mockSvc)

		expectedErr := errors.New("network error")
		mockSvc.OnRest = func(method, path string, body any) (*http.Response, error) {
			return nil, expectedErr
		}
		_, err := community.GetHTML(t.Context(), client, "/test/html")
		require.Error(t, err)
		assert.Equal(t, expectedErr, err)
	})

	t.Run("body_read_fails", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		mockSvc.OnRest = func(method, path string, body any) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(faultyReader{})}, nil
		}
		client := community.NewClient(nil, nil).WithREST(mockSvc)

		_, err := community.GetHTML(t.Context(), client, "/test/html")
		require.Error(t, err)
		assert.EqualError(t, err, "read error")
	})
}

func TestPostForm(t *testing.T) {
	t.Parallel()

	respBody, _ := json.Marshal(genericResponse{Success: true})

	t.Run("success_with_request_struct", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		client := community.NewClient(nil, mockSvc).WithREST(mockSvc)

		mockSvc.OnRest = func(method, path string, body any) (*http.Response, error) {
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
		_, err := community.PostForm[genericResponse](t.Context(), client, "/test/post", reqMsg)
		require.NoError(t, err)
	})

	t.Run("success_with_session_id_already_set", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		client := community.NewClient(nil, mockSvc).WithREST(mockSvc)

		mockSvc.OnRest = func(method, path string, body any) (*http.Response, error) {
			var bodyStr string
			if b, ok := body.([]byte); ok {
				bodyStr = string(b)
			} else {
				bodyStr = body.(string)
			}

			vals, err := url.ParseQuery(bodyStr)
			require.NoError(t, err)
			assert.Equal(t, "custom_session_id", vals.Get("sessionid"))

			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(respBody))}, nil
		}

		type requestWithSession struct {
			SessionID string `url:"sessionid"`
			Param     string `url:"param"`
		}

		reqMsg := requestWithSession{SessionID: "custom_session_id", Param: "val"}
		_, err := community.PostForm[genericResponse](t.Context(), client, "/test/post", reqMsg)
		require.NoError(t, err)
	})

	t.Run("success_without_request_struct", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		client := community.NewClient(nil, mockSvc).WithREST(mockSvc)

		mockSvc.OnRest = func(method, path string, body any) (*http.Response, error) {
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
		_, err := community.PostForm[genericResponse](t.Context(), client, "/test/post", nil)
		require.NoError(t, err)
	})

	t.Run("request_error", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		client := community.NewClient(nil, mockSvc).WithREST(mockSvc)

		mockSvc.OnRest = func(method, path string, body any) (*http.Response, error) {
			return nil, errors.New("post form failed")
		}
		_, err := community.PostForm[genericResponse](t.Context(), client, "/test/post", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "post form failed")
	})

	t.Run("struct_conversion_error", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		client := community.NewClient(nil, mockSvc).WithREST(mockSvc)

		_, err := community.PostForm[genericResponse](t.Context(), client, "/test/post", make(chan int))
		require.Error(t, err)
	})
}

func TestPostJSON(t *testing.T) {
	t.Parallel()

	respBody, _ := json.Marshal(genericResponse{Success: true})

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		client := community.NewClient(nil, mockSvc).WithREST(mockSvc)

		mockSvc.OnRest = func(method, path string, body any) (*http.Response, error) {
			var reqData genericRequest

			err := json.Unmarshal(body.([]byte), &reqData)
			require.NoError(t, err)
			assert.Equal(t, "data", reqData.Param1)
			assert.Equal(t, 42, reqData.Param2)

			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(respBody))}, nil
		}
		reqMsg := genericRequest{Param1: "data", Param2: 42}
		_, err := community.PostJSON[genericResponse](t.Context(), client, "/test/post", reqMsg)
		require.NoError(t, err)
	})

	t.Run("success_without_request_body", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		client := community.NewClient(nil, mockSvc).WithREST(mockSvc)

		mockSvc.OnRest = func(method, path string, body any) (*http.Response, error) {
			assert.Nil(t, body)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(respBody))}, nil
		}
		_, err := community.PostJSON[genericResponse](t.Context(), client, "/test/post", nil)
		require.NoError(t, err)
	})

	t.Run("success_with_empty_session_id", func(t *testing.T) {
		t.Parallel()

		mockEmptySess := mock.NewServiceMock()
		mockEmptySess.OnSessionID = func(target string) string {
			return ""
		}
		mockEmptySess.OnRest = func(method, path string, body any) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(respBody))}, nil
		}

		clientEmptySession := community.NewClient(nil, mockEmptySess).WithREST(mockEmptySess)
		_, err := community.PostJSON[genericResponse](t.Context(), clientEmptySession, "/test/post", nil)
		require.NoError(t, err)
	})

	t.Run("request_error", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		client := community.NewClient(nil, mockSvc).WithREST(mockSvc)

		mockSvc.OnRest = func(method, path string, body any) (*http.Response, error) {
			return nil, errors.New("post json failed")
		}
		_, err := community.PostJSON[genericResponse](t.Context(), client, "/test/post", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "post json failed")
	})

	t.Run("json_marshal_error", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		client := community.NewClient(nil, mockSvc).WithREST(mockSvc)

		type badJSON struct{ F func() }

		_, err := community.PostJSON[genericResponse](t.Context(), client, "/test/post", badJSON{})
		require.Error(t, err)
		assert.IsType(t, &json.UnsupportedTypeError{}, err)
	})
}

func TestPerformRequest(t *testing.T) {
	t.Parallel()

	t.Run("applies_call_options", func(t *testing.T) {
		t.Parallel()

		var receivedHeaders http.Header

		httpClient := &mockHTTPDoer{
			doFunc: func(req *http.Request) (*http.Response, error) {
				receivedHeaders = req.Header
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
			},
		}
		client := community.NewClient(httpClient, nil)
		_, err := community.Get[genericResponse](
			t.Context(),
			client,
			"/test",
			nil,
			aoni.WithHeader("X-Test-Header", "Value123"),
		)
		require.NoError(t, err)
		assert.Equal(t, "Value123", receivedHeaders.Get("X-Test-Header"))
	})

	t.Run("registry_fallback", func(t *testing.T) {
		t.Parallel()

		mockSvc := mock.NewServiceMock()
		mockSvc.OnRest = func(method, path string, body any) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"success": true}`))}, nil
		}
		req := customRequester{Requester: mockSvc}

		require.NotPanics(t, func() {
			_, err := community.Get[genericResponse](t.Context(), req, "/test", nil)
			require.NoError(t, err)
		})
	})
}
