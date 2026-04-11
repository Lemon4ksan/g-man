// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
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

	if !contains(err.Error(), "not found") {
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

func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr || (len(substr) > 0 && (s[:len(substr)] == substr || contains(s[1:], substr))))
}
