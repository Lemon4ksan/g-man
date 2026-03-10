// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package api

import (
	"context"
	"errors"
	"testing"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

type MockTransport struct {
	OnDo func(req *tr.Request) (*tr.Response, error)
}

func (m *MockTransport) Do(req *tr.Request) (*tr.Response, error) {
	return m.OnDo(req)
}

func (m *MockTransport) Close() error { return nil }

type mockTarget string

func (m mockTarget) String() string { return string(m) }

func TestUnifiedClient_ParamInjection(t *testing.T) {
	ctx := context.Background()
	apiKey := "test-api-key"
	accessToken := "test-access-token"

	transport := &MockTransport{
		OnDo: func(req *tr.Request) (*tr.Response, error) {
			if req.Params().Get("key") != apiKey {
				t.Errorf("expected api key %s, got %s", apiKey, req.Params().Get("key"))
			}
			if req.Params().Get("access_token") != accessToken {
				t.Errorf("expected access token %s, got %s", accessToken, req.Params().Get("access_token"))
			}
			return &tr.Response{StatusCode: 200, Body: []byte("{}")}, nil
		},
	}

	client := NewUnifiedClient(transport).
		WithAPIKey(apiKey).
		WithAccessToken(accessToken)

	req := tr.NewRequest(ctx, mockTarget("test"), nil)
	_, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do failed: %v", err)
	}
}

func TestUnifiedClient_Errors(t *testing.T) {
	ctx := context.Background()

	t.Run("HTTP Error", func(t *testing.T) {
		transport := &MockTransport{
			OnDo: func(req *tr.Request) (*tr.Response, error) {
				return &tr.Response{StatusCode: 401}, nil
			},
		}
		client := NewUnifiedClient(transport)
		_, err := client.Do(tr.NewRequest(ctx, mockTarget("test"), nil))

		var apiErr SteamAPIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != 401 {
			t.Errorf("expected SteamAPIError 401, got %v", err)
		}
	})

	t.Run("EResult Error", func(t *testing.T) {
		transport := &MockTransport{
			OnDo: func(req *tr.Request) (*tr.Response, error) {
				return &tr.Response{
					StatusCode: 200,
					Result:     protocol.EResult_Fail,
				}, nil
			},
		}
		client := NewUnifiedClient(transport)
		_, err := client.Do(tr.NewRequest(ctx, mockTarget("test"), nil))

		var resErr EResultError
		if !errors.As(err, &resErr) || resErr.EResult != protocol.EResult_Fail {
			t.Errorf("expected EResultError Fail, got %v", err)
		}
	})
}

func TestUnifiedClient_UnmarshalResponse(t *testing.T) {
	client := NewUnifiedClient(nil)

	t.Run("Wrapped JSON", func(t *testing.T) {
		data := []byte(`{"response": {"name": "G-Man"}}`)
		target := make(map[string]string)
		err := client.unmarshalResponse(data, &target)
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
		err := client.unmarshalResponse(data, &target)
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
		err := client.unmarshalResponse(data, target)
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
		err := client.unmarshalResponse(data, &target)
		if err != nil {
			t.Fatalf("unmarshal VDF failed: %v", err)
		}
		if target.Player.Health != "100" {
			t.Errorf("expected 100, got %s", target.Player.Health)
		}
	})
}

func TestUnifiedClient_EmptyResponse(t *testing.T) {
	client := NewUnifiedClient(nil)
	target := make(map[string]any)

	cases := [][]byte{
		nil,
		{},
		{0x00},
		[]byte(`{"response": {}}`),
		[]byte(`{"response": null}`),
	}

	for _, tc := range cases {
		err := client.unmarshalResponse(tc, &target)
		if err != nil {
			t.Errorf("expected no error for %v, got %v", tc, err)
		}
	}
}
