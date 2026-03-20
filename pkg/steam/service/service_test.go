// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package service

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
)

type MockTransport struct {
	OnDo func(req *tr.Request) (*tr.Response, error)
}

func (m *MockTransport) Do(ctx context.Context, req *tr.Request) (*tr.Response, error) {
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
			return tr.NewResponse([]byte("{}"), tr.HTTPMetadata{StatusCode: http.StatusOK}), nil
		},
	}

	client := New(transport).
		WithAPIKey(apiKey).
		WithAccessToken(accessToken)

	req := tr.NewRequest(mockTarget("test"), nil)
	_, err := client.Do(ctx, req)
	if err != nil {
		t.Fatalf("Do failed: %v", err)
	}
}

func TestUnifiedClient_Errors(t *testing.T) {
	ctx := context.Background()

	t.Run("HTTP Error", func(t *testing.T) {
		transport := &MockTransport{
			OnDo: func(req *tr.Request) (*tr.Response, error) {
				return tr.NewResponse(nil, tr.HTTPMetadata{StatusCode: 401}), nil
			},
		}
		client := New(transport)
		_, err := client.Do(ctx, tr.NewRequest(mockTarget("test"), nil))

		var apiErr api.SteamAPIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != 401 {
			t.Errorf("expected SteamAPIError 401, got %v", err)
		}
	})

	t.Run("EResult Error", func(t *testing.T) {
		transport := &MockTransport{
			OnDo: func(req *tr.Request) (*tr.Response, error) {
				return tr.NewResponse(nil, tr.HTTPMetadata{
					Result:     protocol.EResult_Fail,
					StatusCode: http.StatusOK,
				}), nil
			},
		}
		client := New(transport)
		_, err := client.Do(ctx, tr.NewRequest(mockTarget("test"), nil))

		var resErr api.EResultError
		if !errors.As(err, &resErr) || resErr.EResult != protocol.EResult_Fail {
			t.Errorf("expected EResultError Fail, got %v", err)
		}
	})
}
