// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rest

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testPayload struct {
	Message string `json:"message"`
	Status  int    `json:"status"`
}

func TestClient_Request_URLConstruction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/test" {
			t.Errorf("expected path /api/v1/test, got %s", r.URL.Path)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)

	r, err := client.Request(context.Background(), http.MethodGet, "/api/v1/test", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	_ = r.Body.Close()
}

func TestClient_Request_GetParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("foo") != "bar" || query.Get("baz") != "123" {
			t.Errorf("unexpected query params: %v", query)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)
	params := url.Values{}
	params.Set("foo", "bar")
	params.Set("baz", "123")

	r, err := client.Request(context.Background(), http.MethodGet, "/test", nil, params)
	if err != nil {
		t.Fatal(err)
	}

	_ = r.Body.Close()
}

func TestClient_Headers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Default") != "default-val" {
			t.Error("default header missing")
		}

		if r.Header.Get("X-Custom") != "custom-val" {
			t.Error("custom modifier header missing")
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL).WithHeader("X-Default", "default-val")

	mod := func(req *http.Request) {
		req.Header.Set("X-Custom", "custom-val")
	}

	r, err := client.Request(context.Background(), http.MethodGet, "/", nil, nil, mod)
	if err != nil {
		t.Fatal(err)
	}

	_ = r.Body.Close()
}

func TestClient_GetJSON(t *testing.T) {
	expected := testPayload{Message: "hello", Status: http.StatusOK}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)

	result, err := GetJSON[testPayload](context.Background(), client, "/json", nil)
	if err != nil {
		t.Fatalf("GetJSON failed: %v", err)
	}

	if result.Message != expected.Message || result.Status != expected.Status {
		t.Errorf("decoded struct mismatch. got %+v, want %+v", result, expected)
	}
}

func TestClient_PostJSON(t *testing.T) {
	input := testPayload{Message: "sending", Status: 1}
	response := testPayload{Message: "received", Status: 2}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("expected application/json content type")
		}

		var body testPayload
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}

		if body.Message != input.Message {
			t.Errorf("request body mismatch: got %s, want %s", body.Message, input.Message)
		}

		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)

	result, err := PostJSON[testPayload, testPayload](context.Background(), client, "/post", input, nil)
	if err != nil {
		t.Fatalf("PostJSON failed: %v", err)
	}

	if result.Message != response.Message {
		t.Errorf("response mismatch: got %s, want %s", result.Message, response.Message)
	}
}

func TestClient_ErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": "not found"}`))
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)

	_, err := GetJSON[any](context.Background(), client, "/404", nil)
	if err == nil {
		t.Fatal("expected error on 404 status code, got nil")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Errorf("expected APIError, got %v", err)
	}

	if !contains(string(apiErr.Body), "not found") {
		t.Errorf("unexpected error message: %v", err)
	}

	if !contains(apiErr.Error(), "404") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestClient_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(100 * time.Millisecond):
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)
	ctx, cancel := context.WithCancel(context.Background())

	cancel()

	r, err := client.Request(ctx, http.MethodGet, "/", nil, nil)
	if err == nil {
		_ = r.Body.Close()

		t.Fatal("expected error for canceled context, got nil")
	}
}

func TestClient_DeleteJSON(t *testing.T) {
	input := testPayload{Message: "deleting", Status: 1}
	response := testPayload{Message: "deleted", Status: 2}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}

		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("expected application/json content type")
		}

		var body testPayload
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}

		if body.Message != input.Message {
			t.Errorf("request body mismatch: got %s, want %s", body.Message, input.Message)
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)

	result, err := DeleteJSON[testPayload, testPayload](context.Background(), client, "/delete", input, nil)
	if err != nil {
		t.Fatalf("DeleteJSON failed: %v", err)
	}

	if result.Message != response.Message {
		t.Errorf("response mismatch: got %s, want %s", result.Message, response.Message)
	}
}

func TestClient_DeleteJSON_NilPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}

		// Check that body is empty
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != io.EOF && err != nil {
			t.Errorf("expected empty body, got error: %v", err)
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)

	_, err := DeleteJSON[*testPayload, any](context.Background(), client, "/delete-nil", nil, nil)
	if err != nil {
		t.Fatalf("DeleteJSON failed: %v", err)
	}
}

// Improved generic response for testing
type apiResponse struct {
	Status   string `json:"status"`
	Data     any    `json:"data"`
	ErrorMsg string `json:"error,omitempty"`
}

func (a *apiResponse) IsSuccess() bool  { return a.Status == "success" }
func (a *apiResponse) Error() error     { return errors.New(a.ErrorMsg) }
func (a *apiResponse) SetData(data any) { a.Data = data }

func TestClient_BaseResponse(t *testing.T) {
	t.Run("Success response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"status": "success", "data": {"message": "unwrapped"}}`))
		}))
		defer server.Close()

		client := NewClient(nil).
			WithBaseURL(server.URL).
			WithBaseResponse(func() BaseResponse { return &apiResponse{} })

		result, err := GetJSON[testPayload](context.Background(), client, "/wrapped", nil)
		require.NoError(t, err)
		assert.Equal(t, "unwrapped", result.Message)
	})

	t.Run("Error response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"status": "fail", "error": "something went wrong"}`))
		}))
		defer server.Close()

		client := NewClient(nil).
			WithBaseURL(server.URL).
			WithBaseResponse(func() BaseResponse { return &apiResponse{} })

		_, err := GetJSON[testPayload](context.Background(), client, "/error", nil)
		assert.ErrorContains(t, err, "something went wrong")
	})
}

func TestClient_PathTemplates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Path", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)

	t.Run("WithVar single replacement", func(t *testing.T) {
		resp, err := client.Request(
			context.Background(),
			http.MethodGet,
			"/user/{id}/profile",
			nil,
			nil,
			WithVar("id", 123),
		)
		require.NoError(t, err)
		assert.Equal(t, "/user/123/profile", resp.Header.Get("X-Path"))
	})

	t.Run("WithVars multiple replacements", func(t *testing.T) {
		resp, err := client.Request(
			context.Background(),
			http.MethodGet,
			"/{group}/{member}",
			nil,
			nil,
			WithVars("group", "admins", "member", "bob"),
		)
		require.NoError(t, err)
		assert.Equal(t, "/admins/bob", resp.Header.Get("X-Path"))
	})

	t.Run("Escaping", func(t *testing.T) {
		resp, err := client.Request(
			context.Background(),
			http.MethodGet,
			"/search/{query}",
			nil,
			nil,
			WithVar("query", "hello world"),
		)
		require.NoError(t, err)
		assert.Equal(t, "/search/hello%20world", resp.Header.Get("X-Path"))
	})
}

func TestClient_Validation(t *testing.T) {
	type RequiredParams struct {
		ID   int    `url:"id"   validate:"required"`
		Name string `url:"name"`
	}

	type RequiredPayload struct {
		Key string `json:"key" validate:"required"`
	}

	client := NewClient(nil).WithBaseURL("http://localhost")

	t.Run("Missing query param", func(t *testing.T) {
		params := RequiredParams{Name: "test"} // ID is 0 (zero value)
		_, err := GetJSON[any](context.Background(), client, "/test", params)
		assert.Error(t, err)

		var valErr *ValidationError
		if assert.ErrorAs(t, err, &valErr) {
			assert.Equal(t, "ID", valErr.Field)
		}
	})

	t.Run("Missing payload field", func(t *testing.T) {
		payload := RequiredPayload{} // Key is empty
		_, err := PostJSON[RequiredPayload, any](context.Background(), client, "/test", payload, nil)
		assert.Error(t, err)

		var valErr *ValidationError
		if assert.ErrorAs(t, err, &valErr) {
			assert.Equal(t, "Key", valErr.Field)
		}
	})

	t.Run("Validation success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewClient(nil).WithBaseURL(server.URL)
		params := RequiredParams{ID: 1}
		_, err := GetJSON[any](context.Background(), client, "/test", params)
		assert.NoError(t, err)
	})
}

func TestClient_PostForm(t *testing.T) {
	type Params struct {
		ID   int    `url:"id"`
		Name string `url:"name"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		_ = r.ParseForm()
		assert.Equal(t, "123", r.Form.Get("id"))
		assert.Equal(t, "bob", r.Form.Get("name"))

		_, _ = w.Write([]byte(`{"status": 200}`))
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)
	_, err := PostForm[Params, any](context.Background(), client, "/form", Params{ID: 123, Name: "bob"}, nil)
	assert.NoError(t, err)
}

func TestClient_CaptureResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Header", "captured")
		_, _ = w.Write([]byte(`{"message": "ok"}`))
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)

	var rawResp *http.Response

	result, err := GetJSON[testPayload](context.Background(), client, "/capture", nil, CaptureResponse(&rawResp))
	require.NoError(t, err)
	assert.Equal(t, "ok", result.Message)
	require.NotNil(t, rawResp)
	assert.Equal(t, "captured", rawResp.Header.Get("X-Custom-Header"))
	_ = rawResp.Body.Close()
}

func TestClient_DX_Helpers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check Auth
		if r.Header.Get("Authorization") == "Bearer my-token" {
			w.Header().Set("X-Auth", "bearer")
		} else if u, p, ok := r.BasicAuth(); ok && u == "user" && p == "pass" {
			w.Header().Set("X-Auth", "basic")
		}

		// Check UA
		if r.Header.Get("User-Agent") == "G-MAN-BOT" {
			w.Header().Set("X-UA", "ok")
		}

		_, _ = w.Write([]byte(`{"message": "ok"}`))
	}))
	defer server.Close()

	// Update DefaultClient for global helpers test
	DefaultClient = DefaultClient.WithBaseURL(server.URL)

	t.Run("Global Get with Bearer", func(t *testing.T) {
		var raw *http.Response

		res, err := Get[testPayload](context.Background(), "/get", nil, WithBearer("my-token"), CaptureResponse(&raw))
		require.NoError(t, err)
		assert.Equal(t, "ok", res.Message)
		assert.Equal(t, "bearer", raw.Header.Get("X-Auth"))
	})

	t.Run("Basic Auth and User Agent", func(t *testing.T) {
		var raw *http.Response

		_, err := Get[testPayload](
			context.Background(),
			"/auth",
			nil,
			WithBasicAuth("user", "pass"),
			WithUserAgent("G-MAN-BOT"),
			CaptureResponse(&raw),
		)
		require.NoError(t, err)
		assert.Equal(t, "basic", raw.Header.Get("X-Auth"))
		assert.Equal(t, "ok", raw.Header.Get("X-UA"))
	})

	t.Run("Debug Mode (manual verification)", func(t *testing.T) {
		// Just ensure it doesn't panic
		_, err := Get[testPayload](context.Background(), "/debug", nil, Debug())
		require.NoError(t, err)
	})
}

func TestClient_AdvancedFeatures(t *testing.T) {
	t.Run("Streaming body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			assert.Equal(t, "streamed data", string(body))
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewClient(nil).WithBaseURL(server.URL)
		reader := strings.NewReader("streamed data")

		_, err := client.Request(context.Background(), http.MethodPost, "/", reader, nil)
		require.NoError(t, err)
	})

	t.Run("Cookies", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie("test-cookie")
			if err == nil {
				w.Header().Set("X-Cookie", c.Value)
			}

			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewClient(nil).WithBaseURL(server.URL)

		t.Run("WithCookie modifier", func(t *testing.T) {
			resp, err := client.Request(
				context.Background(),
				http.MethodGet,
				"/",
				nil,
				nil,
				WithCookie(&http.Cookie{Name: "test-cookie", Value: "yum"}),
			)
			require.NoError(t, err)
			assert.Equal(t, "yum", resp.Header.Get("X-Cookie"))
		})

		t.Run("WithCookies map modifier", func(t *testing.T) {
			resp, err := client.Request(
				context.Background(),
				http.MethodGet,
				"/",
				nil,
				nil,
				WithCookies(map[string]string{"test-cookie": "yum-yum"}),
			)
			require.NoError(t, err)
			assert.Equal(t, "yum-yum", resp.Header.Get("X-Cookie"))
		})
	})

	t.Run("Redirect policy", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/start" {
				http.Redirect(w, r, "/end", http.StatusFound)
			} else {
				w.WriteHeader(http.StatusOK)
			}
		}))
		defer server.Close()

		t.Run("Disable redirects", func(t *testing.T) {
			client := NewClient(nil).WithBaseURL(server.URL).WithRedirectLimit(0)
			resp, err := client.Request(context.Background(), http.MethodGet, "/start", nil, nil)
			require.NoError(t, err)
			assert.Equal(t, http.StatusFound, resp.StatusCode) // Should not follow
		})

		t.Run("Limit redirects", func(t *testing.T) {
			// With max 2, it should allow one jump (start -> end)
			client := NewClient(nil).WithBaseURL(server.URL).WithRedirectLimit(2)
			resp, err := client.Request(context.Background(), http.MethodGet, "/start", nil, nil)
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})
	})

	t.Run("Timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewClient(nil).WithBaseURL(server.URL).WithTimeout(10 * time.Millisecond)
		_, err := client.Request(context.Background(), http.MethodGet, "/", nil, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Client.Timeout exceeded")
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr || (len(substr) > 0 && (s[:len(substr)] == substr || contains(s[1:], substr))))
}

func TestClient_WithMultipart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.Header.Get("Content-Type"), "multipart/form-data")
		err := r.ParseMultipartForm(10 * 1024 * 1024)
		require.NoError(t, err)

		assert.Equal(t, "val1", r.FormValue("field1"))
		assert.Equal(t, "val2", r.FormValue("field2"))

		file, _, err := r.FormFile("file1")
		require.NoError(t, err)
		data, err := io.ReadAll(file)
		require.NoError(t, err)
		assert.Equal(t, "file content", string(data))

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)
	fields := map[string]string{
		"field1": "val1",
		"field2": "val2",
	}
	files := map[string]io.Reader{
		"file1": strings.NewReader("file content"),
	}

	resp, err := client.Request(context.Background(), http.MethodPost, "/", nil, nil, WithMultipart(fields, files))
	require.NoError(t, err)

	_ = resp.Body.Close()
}

func TestClient_TransportMethod(t *testing.T) {
	client := NewClient(nil)
	tr := client.Transport()
	require.NotNil(t, tr)

	nonStandardClient := NewClient(DoerFunc(func(r *http.Request) (*http.Response, error) {
		return nil, nil
	}))
	assert.Nil(t, nonStandardClient.Transport())
}
