// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
)

type mockTransport struct {
	onDo func(req *tr.Request) (*tr.Response, error)
}

func (m *mockTransport) Do(ctx context.Context, req *tr.Request) (*tr.Response, error) {
	return m.onDo(req)
}
func (m *mockTransport) Close() error { return nil }

type mockTarget string

func (m mockTarget) String() string { return string(m) }

type mockDoerWithRegistry struct {
	reg  *api.UnmarshalRegistry
	onDo func(req *tr.Request) (*tr.Response, error)
}

func (m *mockDoerWithRegistry) Do(ctx context.Context, req *tr.Request) (*tr.Response, error) {
	return m.onDo(req)
}
func (m *mockDoerWithRegistry) Registry() *api.UnmarshalRegistry { return m.reg }

// These are only used for reflection tests and won't be passed to proto.Marshal.
type (
	CTest_DoWork_Request struct{ proto.Message }
	Test_DoWork_Request  struct{ proto.Message }
	CTest_Simple         struct{ proto.Message }
	C_Invalid            struct{ proto.Message }
)

func TestClient_Initialization(t *testing.T) {
	trans := &mockTransport{}
	c := New(trans)
	assert.NotNil(t, c.Registry())

	c1 := c.WithAPIKey("key")
	assert.Equal(t, "key", c1.apiKey)

	c2 := c.WithAccessToken("token")
	assert.Equal(t, "token", c2.accessToken)

	reg := api.NewUnmarshalRegistry()
	c3 := c.WithRegistry(reg)
	assert.Same(t, reg, c3.Registry())
}

func TestClient_Do(t *testing.T) {
	ctx := context.Background()

	t.Run("Transport Error", func(t *testing.T) {
		trans := &mockTransport{onDo: func(req *tr.Request) (*tr.Response, error) {
			return nil, errors.New("fail")
		}}
		c := New(trans)
		_, err := c.Do(ctx, tr.NewRequest(mockTarget("t"), nil))
		assert.ErrorContains(t, err, "transport error")
	})

	t.Run("Credential Injection", func(t *testing.T) {
		trans := &mockTransport{onDo: func(req *tr.Request) (*tr.Response, error) {
			assert.Equal(t, "K", req.Params().Get("key"))
			assert.Equal(t, "T", req.Params().Get("access_token"))
			return tr.NewResponse([]byte("{}"), tr.HTTPMetadata{StatusCode: 200}), nil
		}}
		c := New(trans).WithAPIKey("K").WithAccessToken("T")
		_, err := c.Do(ctx, tr.NewRequest(mockTarget("t"), nil))
		assert.NoError(t, err)
	})
}

func TestClient_ValidateEResult(t *testing.T) {
	trans := &mockTransport{}
	c := New(trans)

	// HTTP 401
	resp := tr.NewResponse(nil, tr.HTTPMetadata{StatusCode: 401})
	assert.ErrorIs(t, c.validateEResult(resp), api.ErrSessionExpired)

	// HTTP Result 0 -> OK
	resp = tr.NewResponse(nil, tr.HTTPMetadata{StatusCode: 200, Result: 0})
	assert.NoError(t, c.validateEResult(resp))

	// Auth Error Result
	resp = tr.NewResponse(nil, tr.HTTPMetadata{StatusCode: 200, Result: enums.EResult_InvalidPassword})
	assert.ErrorIs(t, c.validateEResult(resp), api.ErrSessionExpired)

	// General Result Fail
	resp = tr.NewResponse(nil, tr.HTTPMetadata{StatusCode: 200, Result: enums.EResult_Fail})
	err := c.validateEResult(resp)

	var resErr *api.EResultError
	require.ErrorAs(t, err, &resErr)
	assert.Equal(t, enums.EResult_Fail, resErr.Result)

	// Socket Success
	resp = tr.NewResponse(nil, tr.SocketMetadata{Result: enums.EResult_OK})
	assert.NoError(t, c.validateEResult(resp))
}

func TestInferUnifiedMethod(t *testing.T) {
	t.Run("Nil Request", func(t *testing.T) {
		_, _, err := inferUnifiedMethod(nil)
		assert.ErrorIs(t, err, ErrInvalidMessage)
	})

	t.Run("Naming Logic", func(t *testing.T) {
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
		// No underscores
		type SingleWord struct{ proto.Message }

		_, _, err := inferUnifiedMethod(&SingleWord{})
		assert.ErrorIs(t, err, ErrInvalidMessage)
	})
}

func TestExecute_Logic(t *testing.T) {
	ctx := context.Background()

	t.Run("Registry From Provider", func(t *testing.T) {
		reg := api.NewUnmarshalRegistry()
		doer := &mockDoerWithRegistry{
			reg: reg,
			onDo: func(req *tr.Request) (*tr.Response, error) {
				return tr.NewResponse([]byte(`{"nickname":"G"}`), tr.HTTPMetadata{StatusCode: 200}), nil
			},
		}

		type resp struct{ Nickname string }

		res, err := execute[resp](ctx, doer, tr.NewRequest(mockTarget("t"), nil), api.FormatJSON)
		assert.NoError(t, err)
		assert.Equal(t, "G", res.Nickname)
	})

	t.Run("Unmarshal Error", func(t *testing.T) {
		doer := &mockTransport{onDo: func(req *tr.Request) (*tr.Response, error) {
			return tr.NewResponse([]byte(`{invalid}`), tr.HTTPMetadata{StatusCode: 200}), nil
		}}
		_, err := execute[map[string]any](ctx, doer, tr.NewRequest(mockTarget("t"), nil), api.FormatJSON)
		assert.Error(t, err)
	})

	t.Run("NoResponse Sentinel", func(t *testing.T) {
		doer := &mockTransport{onDo: func(req *tr.Request) (*tr.Response, error) {
			return tr.NewResponse([]byte(`ignored`), tr.HTTPMetadata{StatusCode: 200}), nil
		}}
		res, err := execute[NoResponse](ctx, doer, tr.NewRequest(mockTarget("t"), nil), api.FormatJSON)
		assert.NoError(t, err)
		assert.Nil(t, res)
	})
}

func TestEntryPoints(t *testing.T) {
	ctx := context.Background()
	trans := &mockTransport{onDo: func(req *tr.Request) (*tr.Response, error) {
		return tr.NewResponse([]byte(`{}`), tr.HTTPMetadata{StatusCode: 200}), nil
	}}

	t.Run("UnifiedExplicit", func(t *testing.T) {
		// Use a real generated proto message to avoid Marshal panic
		_, err := UnifiedExplicit[NoResponse](ctx, trans, "POST", "I", "M", 1, &emptypb.Empty{})
		assert.NoError(t, err)
	})

	t.Run("WebAPI", func(t *testing.T) {
		// Nil request msg
		_, err := WebAPI[NoResponse](ctx, trans, "GET", "I", "M", 1, nil)
		assert.NoError(t, err)

		// With struct params
		type P struct {
			ID int `url:"id"`
		}

		_, err = WebAPI[NoResponse](ctx, trans, "GET", "I", "M", 1, &P{ID: 1})
		assert.NoError(t, err)

		// Param conversion error
		_, err = WebAPI[NoResponse](ctx, trans, "GET", "I", "M", 1, make(chan int))
		assert.Error(t, err)
	})

	t.Run("Legacy", func(t *testing.T) {
		_, err := Legacy[NoResponse](ctx, trans, enums.EMsg_ClientLogon, &pb.CMsgClientLogon{})
		assert.NoError(t, err)
	})

	t.Run("Unified Inference Failure", func(t *testing.T) {
		// Use a real message that doesn't follow the naming convention
		_, err := Unified[NoResponse](ctx, trans, &pb.CMsgClientLogon{})
		assert.ErrorIs(t, err, ErrInvalidMessage)
	})
}
