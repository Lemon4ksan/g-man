// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package api

import (
	"errors"
	"net/url"
	"testing"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

type mockTarget struct {
	URL        string
	HttpMethod string
	Version    int
}

func (m *mockTarget) String() string         { return m.URL }
func (m *mockTarget) SetHTTPMethod(s string) { m.HttpMethod = s }
func (m *mockTarget) SetVersion(v int)       { m.Version = v }

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

func TestUnmarshalResponse(t *testing.T) {
	t.Run("Wrapped JSON", func(t *testing.T) {
		data := []byte(`{"response": {"name": "G-Man"}}`)
		target := make(map[string]string)
		err := UnmarshalResponse(data, &target, FormatJSON)
		if err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}
		if target["name"] != "G-Man" {
			t.Errorf("expected G-Man, got %s", target["name"])
		}
	})

	t.Run("Direct JSON", func(t *testing.T) {
		data := []byte(`{"name": "Gordon"}`)
		target := make(map[string]string)
		err := UnmarshalResponse(data, &target, FormatJSON)
		if err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}
		if target["name"] != "Gordon" {
			t.Errorf("expected Gordon, got %s", target["name"])
		}
	})

	t.Run("Protobuf", func(t *testing.T) {
		msg := &emptypb.Empty{}
		data, _ := proto.Marshal(msg)

		target := &emptypb.Empty{}
		err := UnmarshalResponse(data, &target, FormatProtobuf)
		if err != nil {
			t.Fatalf("unmarshal protobuf failed: %v", err)
		}
	})

	t.Run("VDF Text", func(t *testing.T) {
		data := []byte(`"Player" { "Health" "100" }`)
		var target struct {
			Player struct {
				Health string `mapstructure:"Health"`
			} `mapstructure:"Player"`
		}
		err := UnmarshalResponse(data, &target, FormatVDF)
		if err != nil {
			t.Fatalf("unmarshal VDF failed: %v", err)
		}
		if target.Player.Health != "100" {
			t.Errorf("expected 100, got %s", target.Player.Health)
		}
	})

	t.Run("FormatRaw", func(t *testing.T) {
		data := []byte("raw_binary_data")
		var target []byte
		err := UnmarshalResponse(data, &target, FormatRaw)
		if err != nil {
			t.Fatal(err)
		}
		if string(target) != "raw_binary_data" {
			t.Errorf("expected raw_binary_data, got %s", string(target))
		}

		// Test error when target is not *[]byte
		var wrongTarget string
		err = UnmarshalResponse(data, &wrongTarget, FormatRaw)
		if err == nil {
			t.Error("expected error for non-slice target in FormatRaw")
		}
	})

	t.Run("Protobuf JSON Detection", func(t *testing.T) {
		data := []byte(`{}`)
		target := &emptypb.Empty{}
		err := UnmarshalResponse(data, target, FormatProtobuf)
		if err != nil {
			t.Fatalf("failed to unmarshal JSON-encoded protobuf: %v", err)
		}
	})

	t.Run("VDF Wrapped", func(t *testing.T) {
		data := []byte(`"response" { "success" "1" }`)
		var target struct {
			Success string `mapstructure:"success"`
		}
		err := UnmarshalResponse(data, &target, FormatVDF)
		if err != nil {
			t.Fatal(err)
		}
		if target.Success != "1" {
			t.Error("failed to unwrap response in VDF")
		}
	})

	t.Run("Unsupported Format", func(t *testing.T) {
		err := UnmarshalResponse([]byte("data"), nil, FormatUnknown)
		if err == nil {
			t.Error("expected error for unknown format")
		}
	})
}

func TestEmptyResponse(t *testing.T) {
	target := make(map[string]any)

	cases := [][]byte{
		nil,
		{},
		[]byte(`{"response": {}}`),
		[]byte(`{"response": null}`),
	}

	for _, tc := range cases {
		err := UnmarshalResponse(tc, &target, FormatJSON)
		if err != nil {
			t.Errorf("expected no error for %v, got %v", tc, err)
		}
	}
}

func TestCallOptions(t *testing.T) {
	target := &mockTarget{URL: "test"}
	req := tr.NewRequest(target, nil)
	cfg := &CallConfig{}

	t.Run("WithHTTPMethod", func(t *testing.T) {
		WithHTTPMethod("PUT")(req, cfg)
		if target.HttpMethod != "PUT" {
			t.Error("WithHTTPMethod failed")
		}
	})

	t.Run("WithVersion", func(t *testing.T) {
		WithVersion(5)(req, cfg)
		if target.Version != 5 {
			t.Error("WithVersion failed")
		}
	})

	t.Run("WithFormat", func(t *testing.T) {
		WithFormat(FormatVDF)(req, cfg)
		if cfg.Format != FormatVDF {
			t.Error("WithFormat failed")
		}
	})

	t.Run("QueryParams", func(t *testing.T) {
		WithQueryParam("a", "1")(req, cfg)
		WithQueryParams(url.Values{"b": {"2"}})(req, cfg)
		WithOverrideAPIKey("secret")(req, cfg)

		params := req.Params()
		if params.Get("a") != "1" || params.Get("b") != "2" || params.Get("key") != "secret" {
			t.Errorf("query params injection failed: %v", params)
		}
	})
}

func TestAuthErrors(t *testing.T) {
	authErrors := []enums.EResult{
		enums.EResult_NotLoggedOn,
		enums.EResult_Expired,
		enums.EResult_InvalidPassword,
	}

	for _, res := range authErrors {
		if !IsAuthError(res) {
			t.Errorf("expected %v to be an auth error", res)
		}
	}

	if IsAuthError(enums.EResult_OK) {
		t.Error("EResult_OK should not be an auth error")
	}
}

func TestErrorStructures(t *testing.T) {
	t.Run("EResultError", func(t *testing.T) {
		baseErr := errors.New("underlying")
		err := EResultError{EResult: enums.EResult_Busy, Err: baseErr}

		if !errors.Is(err, baseErr) {
			t.Error("EResultError unwrap failed")
		}
		if err.Error() == "" {
			t.Error("empty error string")
		}
	})

	t.Run("SteamAPIError", func(t *testing.T) {
		baseErr := errors.New("network_fail")
		err := SteamAPIError{Message: "fail", StatusCode: 500, Err: baseErr}

		if !errors.Is(err, baseErr) {
			t.Error("SteamAPIError unwrap failed")
		}
		expected := "steam API error: message=fail, status=500"
		if err.Error() != expected {
			t.Errorf("expected %s, got %s", expected, err.Error())
		}
	})
}

func TestNewHttpRequest(t *testing.T) {
	req := NewHttpRequest("POST", "http://example.com/api", []byte("body"))
	target, ok := req.Target().(HttpTarget)
	if !ok {
		t.Fatal("target is not HttpTarget")
	}
	if target.HTTPMethod() != "POST" || target.HTTPPath() != "api" {
		t.Errorf("NewHttpRequest created invalid target: %+v", target)
	}
}
