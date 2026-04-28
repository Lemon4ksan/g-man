// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package api

import (
	"testing"

	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
)

func TestCallOptions_Detailed(t *testing.T) {
	target := &mockTarget{URL: "http://api.steampowered.com"}
	req := tr.NewRequest(target, nil)
	cfg := &CallConfig{}

	t.Run("WithHeader", func(t *testing.T) {
		WithHeader("X-Custom-Header", "G-Man-Secret")(req, cfg)

		if req.Header().Get("X-Custom-Header") != "G-Man-Secret" {
			t.Error("WithHeader failed to set header")
		}
	})

	t.Run("WithCustomRegistry", func(t *testing.T) {
		customReg := &UnmarshalRegistry{}
		WithCustomRegistry(customReg)(req, cfg)

		if cfg.Registry != customReg {
			t.Error("WithCustomRegistry failed to set registry")
		}
	})

	t.Run("WithHTTPMethod - Compatibility check", func(t *testing.T) {
		reqNoSetter := tr.NewRequest(nil, nil)
		WithHTTPMethod("POST")(reqNoSetter, cfg)
	})

	t.Run("WithVersion - Compatibility check", func(t *testing.T) {
		reqNoSetter := tr.NewRequest(nil, nil)
		WithVersion(2)(reqNoSetter, cfg)
	})
}

func TestHttpTarget(t *testing.T) {
	target := HttpTarget{
		HttpMethod: "POST",
		URL:        "https://steamcommunity.com/tradeoffer/new/",
	}

	if target.HTTPMethod() != "POST" {
		t.Errorf("expected POST, got %s", target.HTTPMethod())
	}

	expectedPath := "tradeoffer/new/"
	if target.HTTPPath() != expectedPath {
		t.Errorf("expected path %s, got %s", expectedPath, target.HTTPPath())
	}

	// Test default method
	targetDefault := HttpTarget{URL: "http://test.com/path"}
	if targetDefault.HTTPMethod() != "GET" {
		t.Error("expected default method GET")
	}
}

func TestHttpTarget_Methods(t *testing.T) {
	t.Run("Default Method", func(t *testing.T) {
		target := HttpTarget{URL: "http://test.com"}
		if target.HTTPMethod() != "GET" {
			t.Errorf("expected default GET, got %s", target.HTTPMethod())
		}
	})

	t.Run("Custom Method", func(t *testing.T) {
		target := HttpTarget{URL: "http://test.com", HttpMethod: "PATCH"}
		if target.HTTPMethod() != "PATCH" {
			t.Errorf("expected PATCH, got %s", target.HTTPMethod())
		}
	})

	t.Run("String representation", func(t *testing.T) {
		u := "http://example.com/api"

		target := HttpTarget{URL: u}
		if target.String() != u {
			t.Errorf("expected %s, got %s", u, target.String())
		}
	})

	t.Run("HTTPPath extraction", func(t *testing.T) {
		tests := []struct {
			url      string
			expected string
		}{
			{"http://api.com/ISteamUser/GetPlayerSummaries/v0002/", "ISteamUser/GetPlayerSummaries/v0002/"},
			{"/v1/internal/call", "v1/internal/call"},
			{"api/no-slash", "api/no-slash"},
			{"", ""},
		}

		for _, tc := range tests {
			target := HttpTarget{URL: tc.url}
			if target.HTTPPath() != tc.expected {
				t.Errorf("url: %s, expected path: %s, got: %s", tc.url, tc.expected, target.HTTPPath())
			}
		}
	})
}

func TestNewHttpRequest_Creation(t *testing.T) {
	method := "DELETE"
	url := "http://steam.com/delete"
	body := []byte("payload")

	req := NewHttpRequest(method, url, body)

	if req == nil {
		t.Fatal("request is nil")
	}

	target, ok := req.Target().(HttpTarget)
	if !ok {
		t.Fatal("target should be HttpTarget")
	}

	if target.HttpMethod != method {
		t.Errorf("expected %s, got %s", method, target.HttpMethod)
	}

	if string(req.Body()) != "payload" {
		t.Errorf("expected payload, got %s", string(req.Body()))
	}
}

func TestResponseFormat_Values(t *testing.T) {
	formats := []ResponseFormat{
		FormatUnknown, FormatRaw, FormatProtobuf,
		FormatJSON, FormatVDF, FormatBinaryKV,
	}

	for i, f := range formats {
		if int(f) != i {
			t.Errorf("Format enum mismatch at index %d", i)
		}
	}
}
