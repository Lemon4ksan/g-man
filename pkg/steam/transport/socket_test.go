// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
)

type mockSocketCaller struct {
	session     *mockSession
	mockCallErr error
	mockPacket  *protocol.Packet
	mockCbErr   error
}

func (m *mockSocketCaller) Session() socket.Session {
	if m.session == nil {
		return nil
	}

	return m.session
}

func (m *mockSocketCaller) Send(
	ctx context.Context,
	build socket.PayloadBuilder,
	opts ...socket.SendOption,
) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if m.mockCallErr != nil {
		return m.mockCallErr
	}

	return nil
}

func (m *mockSocketCaller) SendSync(
	ctx context.Context,
	build socket.PayloadBuilder,
	opts ...socket.SendOption,
) (*protocol.Packet, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if m.mockCallErr != nil {
		return nil, m.mockCallErr
	}

	if m.mockCbErr != nil {
		return nil, m.mockCbErr
	}

	return m.mockPacket, nil
}

type mockSocketTarget struct {
	emsg uint32
}

func (m mockSocketTarget) String() string              { return "mock" }
func (m mockSocketTarget) EMsg(isAuth bool) enums.EMsg { return enums.EMsg(m.emsg) }
func (m mockSocketTarget) ObjectName() string          { return "MockObject" }

type mockSession struct {
	socket.Session
	authed bool
}

func (m *mockSession) IsAuthenticated() bool { return m.authed }

type simpleHeader struct {
	protocol.Header
	sourceJob uint64
}

func (s simpleHeader) GetSourceJob() uint64 { return s.sourceJob }

func TestNewSocketTransport_ValidCaller_CreatesTransport(t *testing.T) {
	t.Parallel()

	caller := &mockSocketCaller{}
	tr := NewSocketTransport(caller)
	assert.NotNil(t, tr)
}

func TestSocketTransportDo_VariousRequests_SendsPacketsAndMapsHeader(t *testing.T) {
	t.Parallel()

	t.Run("success_with_eheader", func(t *testing.T) {
		t.Parallel()

		caller := &mockSocketCaller{
			session: &mockSession{authed: true},
			mockPacket: &protocol.Packet{
				Payload: []byte("payload"),
				Header: mockEHeader{
					result:    enums.EResult_Fail,
					sourceJob: 777,
				},
			},
		}
		tr := NewSocketTransport(caller)
		req := NewRequest(mockSocketTarget{emsg: 1}, nil)

		resp, err := tr.Do(t.Context(), req)
		require.NoError(t, err)

		meta, ok := resp.Socket()
		assert.True(t, ok)
		assert.Equal(t, enums.EResult_Fail, meta.Result)
		assert.Equal(t, uint64(777), meta.SourceJobID)
	})

	t.Run("success_with_simple_header_no_eresult", func(t *testing.T) {
		t.Parallel()

		caller := &mockSocketCaller{
			session: &mockSession{authed: false},
			mockPacket: &protocol.Packet{
				Payload: []byte("payload"),
				Header:  simpleHeader{sourceJob: 888},
			},
		}
		tr := NewSocketTransport(caller)
		req := NewRequest(mockSocketTarget{emsg: 1}, nil)

		resp, err := tr.Do(t.Context(), req)
		require.NoError(t, err)

		meta, _ := resp.Socket()
		assert.Equal(t, enums.EResult_OK, meta.Result)
		assert.Equal(t, uint64(888), meta.SourceJobID)
	})

	t.Run("error_disconnected_nil_session", func(t *testing.T) {
		t.Parallel()

		caller := &mockSocketCaller{session: nil}
		tr := NewSocketTransport(caller)
		_, err := tr.Do(t.Context(), NewRequest(mockSocketTarget{}, nil))
		assert.ErrorContains(t, err, "socket is disconnected")
	})

	t.Run("error_unsupported_target", func(t *testing.T) {
		t.Parallel()

		tr := NewSocketTransport(&mockSocketCaller{session: &mockSession{}})
		_, err := tr.Do(t.Context(), NewRequest(mockTarget{name: "http_only"}, nil))
		assert.ErrorContains(t, err, "does not support socket protocol")
	})

	t.Run("error_sendsync_failed", func(t *testing.T) {
		t.Parallel()

		caller := &mockSocketCaller{
			session:     &mockSession{},
			mockCallErr: errors.New("network error"),
		}
		tr := NewSocketTransport(caller)
		_, err := tr.Do(t.Context(), NewRequest(mockSocketTarget{}, nil))
		assert.ErrorContains(t, err, "socket_transport call failed")
	})

	t.Run("request_body_read_error", func(t *testing.T) {
		t.Parallel()

		caller := &mockSocketCaller{session: &mockSession{}}
		tr := NewSocketTransport(caller)
		req := NewRequest(mockSocketTarget{emsg: 1}, faultyReader{})

		_, err := tr.Do(t.Context(), req)
		assert.ErrorContains(t, err, "failed to read request body")
	})

	t.Run("force_proto_enabled", func(t *testing.T) {
		t.Parallel()

		caller := &mockSocketCaller{
			session: &mockSession{authed: true},
			mockPacket: &protocol.Packet{
				Payload: []byte("proto_payload"),
				Header:  simpleHeader{sourceJob: 999},
			},
		}
		tr := NewSocketTransport(caller)
		req := NewRequest(mockSocketTarget{emsg: 1}, nil).WithForceProto()

		resp, err := tr.Do(t.Context(), req)
		require.NoError(t, err)

		bodyBytes, _ := io.ReadAll(resp.Body)
		assert.Equal(t, "proto_payload", string(bodyBytes))
	})

	t.Run("no_response_mode_success", func(t *testing.T) {
		t.Parallel()

		caller := &mockSocketCaller{
			session: &mockSession{authed: true},
		}
		tr := NewSocketTransport(caller)
		req := NewRequest(mockSocketTarget{emsg: 1}, nil).WithParam("__no_response", "true")

		resp, err := tr.Do(t.Context(), req)
		require.NoError(t, err)

		bodyBytes, _ := io.ReadAll(resp.Body)
		assert.Empty(t, bodyBytes)
	})

	t.Run("no_response_mode_failure", func(t *testing.T) {
		t.Parallel()

		caller := &mockSocketCaller{
			session:     &mockSession{authed: true},
			mockCallErr: errors.New("send failed"),
		}
		tr := NewSocketTransport(caller)
		req := NewRequest(mockSocketTarget{emsg: 1}, nil).WithParam("__no_response", "true")

		_, err := tr.Do(t.Context(), req)
		assert.ErrorContains(t, err, "socket_transport send failed")
	})
}
