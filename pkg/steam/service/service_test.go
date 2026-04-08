// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/lemon4ksan/g-man/pkg/steam/api"
	pb "github.com/lemon4ksan/g-man/pkg/steam/protobuf"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"

	"google.golang.org/protobuf/proto"
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

type CPlayer_GetNickname_Request struct {
	pb.CMsgClientHeartBeat
}

type CPlayer_GetNickname_Response struct {
	proto.Message
	Nickname string `json:"nickname"`
}

func TestInferUnifiedMethod(t *testing.T) {
	tests := []struct {
		msg     proto.Message
		iface   string
		method  string
		wantErr bool
	}{
		{&CPlayer_GetNickname_Request{}, "Player", "GetNickname", false},
		{&pb.CMsgClientLogon{}, "", "", true},
		{nil, "", "", true},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%T", tt.msg), func(t *testing.T) {
			iface, method, err := inferUnifiedMethod(tt.msg)
			if (err != nil) != tt.wantErr {
				t.Errorf("inferUnifiedMethod() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if iface != tt.iface || method != tt.method {
				t.Errorf("got (%s, %s), want (%s, %s)", iface, method, tt.iface, tt.method)
			}
		})
	}
}

func TestClient_Immutability(t *testing.T) {
	base := New(&MockTransport{})
	derived := base.WithAPIKey("key1")

	if base.apiKey != "" {
		t.Error("Base client was mutated after WithAPIKey")
	}
	if derived.apiKey != "key1" {
		t.Error("Derived client doesn't have the API key")
	}

	if base == derived {
		t.Error("WithAPIKey returned the same pointer, expected a clone")
	}
}

func TestUnified_Success(t *testing.T) {
	expectedNickname := "GabeN"

	respData, _ := json.Marshal(map[string]any{
		"response": map[string]string{"nickname": expectedNickname},
	})

	transport := &MockTransport{
		OnDo: func(req *tr.Request) (*tr.Response, error) {
			target := req.Target().(*UnifiedTarget)
			if target.Interface != "Player" || target.Method != "GetNickname" {
				t.Errorf("Wrong target inferred: %s", target.String())
			}
			return tr.NewResponse(respData, tr.HTTPMetadata{StatusCode: http.StatusOK}), nil
		},
	}

	res, err := Unified[CPlayer_GetNickname_Response](t.Context(), transport, &CPlayer_GetNickname_Request{}, api.WithFormat(api.FormatJSON))
	if err != nil {
		t.Fatalf("Unified call failed: %v", err)
	}

	if res.Nickname != expectedNickname {
		t.Errorf("Expected nickname %s, got %s", expectedNickname, res.Nickname)
	}
}

func TestExecute_NoResponse(t *testing.T) {
	ctx := context.Background()
	transport := &MockTransport{
		OnDo: func(req *tr.Request) (*tr.Response, error) {
			return tr.NewResponse([]byte("ignored content"), tr.HTTPMetadata{StatusCode: http.StatusOK}), nil
		},
	}

	res, err := execute[NoResponse](ctx, transport, tr.NewRequest(mockTarget("test"), nil), api.FormatJSON)
	if err != nil {
		t.Errorf("execute with NoResponse failed: %v", err)
	}
	if res != nil {
		t.Error("expected nil response for NoResponse type")
	}
}

func TestClient_SocketMetadataValidation(t *testing.T) {
	ctx := context.Background()

	t.Run("Socket Success", func(t *testing.T) {
		transport := &MockTransport{
			OnDo: func(req *tr.Request) (*tr.Response, error) {
				return tr.NewResponse(nil, tr.SocketMetadata{Result: protocol.EResult_OK}), nil
			},
		}
		client := New(transport)
		_, err := client.Do(ctx, tr.NewRequest(mockTarget("test"), nil))
		if err != nil {
			t.Errorf("Expected OK for socket EResult_OK, got %v", err)
		}
	})

	t.Run("Socket EResult Fail", func(t *testing.T) {
		transport := &MockTransport{
			OnDo: func(req *tr.Request) (*tr.Response, error) {
				return tr.NewResponse(nil, tr.SocketMetadata{Result: protocol.EResult_AccessDenied}), nil
			},
		}
		client := New(transport)
		_, err := client.Do(ctx, tr.NewRequest(mockTarget("test"), nil))

		var resErr api.EResultError
		if !errors.As(err, &resErr) || resErr.EResult != protocol.EResult_AccessDenied {
			t.Errorf("expected EResult_AccessDenied, got %v", err)
		}
	})
}

func TestWebAPI_WithParams(t *testing.T) {
	ctx := context.Background()
	type TestParams struct {
		SteamID uint64 `url:"steamid"`
	}

	transport := &MockTransport{
		OnDo: func(req *tr.Request) (*tr.Response, error) {
			if req.Params().Get("steamid") != "76561197960287930" {
				t.Errorf("Param not found or wrong value: %s", req.Params().Get("steamid"))
			}
			return tr.NewResponse([]byte(`{"response": {"status": "ok"}}`), tr.HTTPMetadata{StatusCode: http.StatusOK}), nil
		},
	}

	_, err := WebAPI[any](ctx, transport, "GET", "ISteamUser", "GetPlayerSummaries", 2, &TestParams{SteamID: 76561197960287930})
	if err != nil {
		t.Errorf("WebAPI failed: %v", err)
	}
}
