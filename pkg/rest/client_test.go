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

func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr || (len(substr) > 0 && (s[:len(substr)] == substr || contains(s[1:], substr))))
}
