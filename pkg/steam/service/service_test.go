// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/encoding"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
)

type mockTransport struct {
	onDo func(req *tr.Request) (*tr.Response, error)
}

func (m *mockTransport) Do(ctx context.Context, req *tr.Request) (*tr.Response, error) {
	return m.onDo(req)
}

type mockTarget string

func (m mockTarget) String() string { return string(m) }

type (
	CTest_DoWork_Request struct{ proto.Message }
	Test_DoWork_Request  struct{ proto.Message }
	CTest_Simple         struct{ proto.Message }
	C_Invalid            struct{ proto.Message }
)

func TestClient_Initialization(t *testing.T) {
	t.Parallel()

	trans := &mockTransport{}
	c := New(trans)

	c1 := c.WithAPIKey("key")
	assert.Equal(t, "key", c1.apiKey)

	c2 := c.WithAccessToken("token")
	assert.Equal(t, "token", c2.accessToken)
}

func TestClient_Do(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("Transport Error", func(t *testing.T) {
		t.Parallel()

		trans := &mockTransport{onDo: func(req *tr.Request) (*tr.Response, error) {
			return nil, errors.New("fail")
		}}
		c := New(trans)
		_, err := c.Do(ctx, tr.NewRequest(mockTarget("t"), nil))
		assert.ErrorContains(t, err, "transport error")
	})

	t.Run("Credential Injection", func(t *testing.T) {
		t.Parallel()

		trans := &mockTransport{onDo: func(req *tr.Request) (*tr.Response, error) {
			assert.Equal(t, "K", req.Params().Get("key"))
			assert.Equal(t, "T", req.Params().Get("access_token"))
			return tr.NewResponse(io.NopCloser(bytes.NewReader([]byte("{}"))), tr.HTTPMetadata{StatusCode: 200}), nil
		}}
		c := New(trans).WithAPIKey("K").WithAccessToken("T")
		_, err := c.Do(ctx, tr.NewRequest(mockTarget("t"), nil))
		assert.NoError(t, err)
	})

	t.Run("EResult Validation Error in Do", func(t *testing.T) {
		t.Parallel()

		trans := &mockTransport{onDo: func(req *tr.Request) (*tr.Response, error) {
			return tr.NewResponse(nil, tr.HTTPMetadata{StatusCode: 401}), nil
		}}
		c := New(trans)
		_, err := c.Do(ctx, tr.NewRequest(mockTarget("t"), nil))
		assert.ErrorIs(t, err, ErrSessionExpired)
	})
}

func TestClient_ValidateEResult(t *testing.T) {
	t.Parallel()

	trans := &mockTransport{}
	c := New(trans)

	// HTTP 401
	resp := tr.NewResponse(nil, tr.HTTPMetadata{StatusCode: 401})
	assert.ErrorIs(t, c.validateEResult(resp), ErrSessionExpired)

	// HTTP Result 0 -> OK
	resp = tr.NewResponse(nil, tr.HTTPMetadata{StatusCode: 200, Result: 0})
	assert.NoError(t, c.validateEResult(resp))

	// Auth Error Result
	resp = tr.NewResponse(nil, tr.HTTPMetadata{StatusCode: 200, Result: enums.EResult_InvalidPassword})
	assert.ErrorIs(t, c.validateEResult(resp), ErrSessionExpired)

	// General Result Fail
	resp = tr.NewResponse(nil, tr.HTTPMetadata{StatusCode: 200, Result: enums.EResult_Fail})
	err := c.validateEResult(resp)

	var resErr *EResultError
	require.ErrorAs(t, err, &resErr)
	assert.Equal(t, enums.EResult_Fail, resErr.Result)

	// Socket Success
	resp = tr.NewResponse(nil, tr.SocketMetadata{Result: enums.EResult_OK})
	assert.NoError(t, c.validateEResult(resp))
}

func TestInferUnifiedMethod(t *testing.T) {
	t.Parallel()

	t.Run("Nil Request", func(t *testing.T) {
		t.Parallel()

		_, _, err := inferUnifiedMethod(nil)
		assert.ErrorIs(t, err, ErrInvalidMessage)
	})

	t.Run("Naming Logic", func(t *testing.T) {
		t.Parallel()

		// Valid
		iface, method, err := inferUnifiedMethod(&CTest_DoWork_Request{})
		assert.NoError(t, err)
		assert.Equal(t, "Test", iface)
		assert.Equal(t, "DoWork", method)

		// Cache hit
		iface, _, _ = inferUnifiedMethod(&CTest_DoWork_Request{})
		assert.Equal(t, "Test", iface)

		// No "C" prefix
		iface, _, err = inferUnifiedMethod(&Test_DoWork_Request{})
		assert.NoError(t, err)
		assert.Equal(t, "Test", iface)

		// No suffix
		_, method, err = inferUnifiedMethod(&CTest_Simple{})
		assert.NoError(t, err)
		assert.Equal(t, "Simple", method)
	})

	t.Run("Invalid Names", func(t *testing.T) {
		t.Parallel()

		// No underscores
		type SingleWord struct{ proto.Message }

		_, _, err := inferUnifiedMethod(&SingleWord{})
		assert.ErrorIs(t, err, ErrInvalidMessage)
	})
}

func TestExecute_Logic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("Registry From Provider", func(t *testing.T) {
		t.Parallel()

		doer := &mockTransport{
			onDo: func(req *tr.Request) (*tr.Response, error) {
				return tr.NewResponse(
					io.NopCloser(bytes.NewReader([]byte(`{"nickname":"G"}`))),
					tr.HTTPMetadata{StatusCode: 200},
				), nil
			},
		}

		type resp struct{ Nickname string }

		res, err := Execute[resp](ctx, doer, tr.NewRequest(mockTarget("t"), nil), encoding.SteamJSONDecoder)
		assert.NoError(t, err)
		assert.Equal(t, "G", res.Nickname)
	})

	t.Run("Unmarshal Error", func(t *testing.T) {
		t.Parallel()

		doer := &mockTransport{onDo: func(req *tr.Request) (*tr.Response, error) {
			return tr.NewResponse(
				io.NopCloser(bytes.NewReader([]byte(`{invalid}`))),
				tr.HTTPMetadata{StatusCode: 200},
			), nil
		}}
		_, err := Execute[map[string]any](ctx, doer, tr.NewRequest(mockTarget("t"), nil), encoding.SteamJSONDecoder)
		assert.Error(t, err)
	})

	t.Run("Doer Error in Execute", func(t *testing.T) {
		t.Parallel()

		doer := &mockTransport{onDo: func(req *tr.Request) (*tr.Response, error) {
			return nil, errors.New("doer error")
		}}
		_, err := Execute[map[string]any](ctx, doer, tr.NewRequest(mockTarget("t"), nil), encoding.SteamJSONDecoder)
		assert.ErrorContains(t, err, "doer error")
	})

	t.Run("NoResponse Sentinel", func(t *testing.T) {
		t.Parallel()

		doer := &mockTransport{onDo: func(req *tr.Request) (*tr.Response, error) {
			return tr.NewResponse(
				io.NopCloser(bytes.NewReader([]byte(`ignored`))),
				tr.HTTPMetadata{StatusCode: 200},
			), nil
		}}
		res, err := Execute[NoResponse](ctx, doer, tr.NewRequest(mockTarget("t"), nil), encoding.SteamJSONDecoder)
		assert.NoError(t, err)
		assert.Nil(t, res)
	})
}

func TestEntryPoints(t *testing.T) {
	ctx := context.Background()
	trans := &mockTransport{onDo: func(req *tr.Request) (*tr.Response, error) {
		return tr.NewResponse(io.NopCloser(bytes.NewReader([]byte(`{}`))), tr.HTTPMetadata{StatusCode: 200}), nil
	}}

	t.Run("UnifiedSuccess", func(t *testing.T) {
		t.Parallel()

		transMock := &mockTransport{onDo: func(req *tr.Request) (*tr.Response, error) {
			assert.Equal(t, "TwoFactor", req.Target().(*UnifiedTarget).Interface)
			assert.Equal(t, "Status", req.Target().(*UnifiedTarget).Method)
			return tr.NewResponse(io.NopCloser(bytes.NewReader([]byte(`{}`))), tr.HTTPMetadata{StatusCode: 200}), nil
		}}

		_, err := Unified[NoResponse](ctx, transMock, &pb.CTwoFactor_Status_Request{})
		assert.NoError(t, err)
	})

	t.Run("UnifiedExplicit", func(t *testing.T) {
		t.Parallel()

		_, err := UnifiedExplicit[NoResponse](ctx, trans, "POST", "I", "M", 1, &emptypb.Empty{})
		assert.NoError(t, err)
	})

	t.Run("WebAPI", func(t *testing.T) {
		t.Parallel()

		_, err := WebAPI[NoResponse](ctx, trans, "GET", "I", "M", 1, nil)
		assert.NoError(t, err)

		type P struct {
			ID int `url:"id"`
		}

		_, err = WebAPI[NoResponse](ctx, trans, "GET", "I", "M", 1, &P{ID: 1})
		assert.NoError(t, err)

		_, err = WebAPI[NoResponse](ctx, trans, "GET", "I", "M", 1, make(chan int))
		assert.Error(t, err)
	})

	t.Run("Legacy", func(t *testing.T) {
		t.Parallel()

		_, err := Legacy[NoResponse](ctx, trans, enums.EMsg_ClientLogon, &pb.CMsgClientLogon{})
		assert.NoError(t, err)
	})

	t.Run("Unified Inference Failure", func(t *testing.T) {
		t.Parallel()

		_, err := Unified[NoResponse](ctx, trans, &pb.CMsgClientLogon{})
		assert.ErrorIs(t, err, ErrInvalidMessage)
	})
}

func TestClient_GettersAndOptions(t *testing.T) {
	t.Parallel()

	trans := &mockTransport{}
	c := New(trans).WithAPIKey("MY_API_KEY").WithAccessToken("MY_ACCESS_TOKEN")

	assert.Equal(t, "MY_API_KEY", c.APIKey())
	assert.Equal(t, "MY_ACCESS_TOKEN", c.AccessToken())

	t.Run("Do returns nil when transport returns nil response", func(t *testing.T) {
		t.Parallel()

		transNil := &mockTransport{onDo: func(req *tr.Request) (*tr.Response, error) {
			return nil, nil
		}}
		clientNil := New(transNil)
		resp, err := clientNil.Do(context.Background(), tr.NewRequest(mockTarget("t"), nil))
		assert.NoError(t, err)
		assert.Nil(t, resp)
	})
}

func TestCallOptions(t *testing.T) {
	t.Parallel()

	t.Run("WithHTTPMethod", func(t *testing.T) {
		t.Parallel()

		target := &UnifiedTarget{Interface: "I", Method: "M", Version: 1}
		req := tr.NewRequest(target, nil)

		WithHTTPMethod("GET")(req)
		assert.Equal(t, "GET", target.HTTPMethod())
	})

	t.Run("WithVersion", func(t *testing.T) {
		t.Parallel()

		target := &UnifiedTarget{Interface: "I", Method: "M", Version: 1}
		req := tr.NewRequest(target, nil)

		WithVersion(5)(req)
		assert.Equal(t, 5, target.Version)
	})

	t.Run("WithDecoder", func(t *testing.T) {
		t.Parallel()

		req := tr.NewRequest(mockTarget("t"), nil)
		dec := encoding.SteamJSONDecoder

		WithDecoder(dec)(req)
		// assert.Equal(t, dec, req.Decoder(nil))
	})

	t.Run("WithFormat", func(t *testing.T) {
		t.Parallel()

		formats := []encoding.ResponseFormat{
			encoding.FormatJSON,
			encoding.FormatProtobuf,
			encoding.FormatVDF,
			encoding.FormatBinaryVDF,
			encoding.FormatRaw,
			encoding.ResponseFormat(999), // Default ignore case
		}

		for _, f := range formats {
			req := tr.NewRequest(mockTarget("t"), nil)
			WithFormat(f)(req)

			if f != encoding.ResponseFormat(999) {
				assert.NotNil(t, req.Decoder(nil))
			}
		}
	})

	t.Run("WithRoutingAppID", func(t *testing.T) {
		t.Parallel()

		req := tr.NewRequest(mockTarget("t"), nil)
		WithRoutingAppID(440)(req)
		assert.Equal(t, uint32(440), req.RoutingAppID())
	})
}

func TestLegacyProto(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	trans := &mockTransport{onDo: func(req *tr.Request) (*tr.Response, error) {
		assert.True(t, req.IsForceProto())
		return tr.NewResponse(io.NopCloser(bytes.NewReader([]byte(`{}`))), tr.HTTPMetadata{StatusCode: 200}), nil
	}}

	_, err := LegacyProto[NoResponse](ctx, trans, enums.EMsg_ClientLogon, &pb.CMsgClientLogon{})
	assert.NoError(t, err)
}

type customRetriableError struct {
	retriable bool
}

func (e customRetriableError) Error() string     { return "custom" }
func (e customRetriableError) IsRetriable() bool { return e.retriable }

func TestErrors_RetriabilityAndIs(t *testing.T) {
	t.Parallel()

	t.Run("IsRetriable Helper", func(t *testing.T) {
		t.Parallel()

		assert.False(t, IsRetriable(nil))
		assert.False(t, IsRetriable(errors.New("plain error")))

		assert.True(t, IsRetriable(customRetriableError{retriable: true}))
		assert.False(t, IsRetriable(customRetriableError{retriable: false}))
	})

	t.Run("EResultError Retriability", func(t *testing.T) {
		t.Parallel()

		retriableResults := []enums.EResult{
			enums.EResult_Timeout,
			enums.EResult_TryAnotherCM,
			enums.EResult_ServiceUnavailable,
			enums.EResult_Pending,
			enums.EResult_Busy,
			enums.EResult_LimitExceeded,
			enums.EResult_Fail, // Should be false
		}

		for _, res := range retriableResults {
			err := NewEResultError(res, nil)
			if res == enums.EResult_Fail {
				assert.False(t, err.IsRetriable())
			} else {
				assert.True(t, err.IsRetriable())
			}
		}
	})

	t.Run("EResultError Is comparison", func(t *testing.T) {
		t.Parallel()

		err1 := NewEResultError(enums.EResult_Busy, nil)
		err2 := NewEResultError(enums.EResult_Busy, nil)
		err3 := NewEResultError(enums.EResult_Timeout, nil)

		assert.True(t, errors.Is(err1, err2))
		assert.False(t, errors.Is(err1, err3))
		assert.False(t, errors.Is(err1, errors.New("other")))
	})

	t.Run("SteamAPIError Retriability", func(t *testing.T) {
		t.Parallel()

		// Internal Server Error (>= 500)
		err500 := NewSteamAPIError("fail", 500, nil)
		assert.True(t, err500.IsRetriable())

		// Too Many Requests (429)
		err429 := NewSteamAPIError("fail", 429, nil)
		assert.True(t, err429.IsRetriable())

		// Unauthorized (401)
		err401 := NewSteamAPIError("fail", 401, nil)
		assert.False(t, err401.IsRetriable())
	})

	t.Run("SteamAPIError Is comparison", func(t *testing.T) {
		t.Parallel()

		underlying := errors.New("underlying error")
		err1 := NewSteamAPIError("unauthorized", 401, underlying)

		// Matching exactly
		err2 := NewSteamAPIError("unauthorized", 401, nil)
		assert.True(t, errors.Is(err1, err2))

		// Matching by code only (target message is empty)
		err3 := NewSteamAPIError("", 401, nil)
		assert.True(t, errors.Is(err1, err3))

		// Matching underlying wrapped error
		assert.True(t, errors.Is(err1, underlying))

		// Mismatched status code
		err4 := NewSteamAPIError("unauthorized", 403, nil)
		assert.False(t, errors.Is(err1, err4))

		// Mismatched message (when target message is non-empty)
		err5 := NewSteamAPIError("forbidden", 401, nil)
		assert.False(t, errors.Is(err1, err5))

		// Non-SteamAPIError target
		assert.False(t, errors.Is(err1, errors.New("other")))
	})
}
