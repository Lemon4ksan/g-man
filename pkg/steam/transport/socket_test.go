// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport

import (
	"context"
	"errors"
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

func TestSocketTransport_Do_Coverage(t *testing.T) {
	ctx := context.Background()

	t.Run("Success with EHeader", func(t *testing.T) {
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

		resp, err := tr.Do(ctx, req)
		require.NoError(t, err)

		meta, ok := resp.Socket()
		assert.True(t, ok)
		assert.Equal(t, enums.EResult_Fail, meta.Result)
		assert.Equal(t, uint64(777), meta.SourceJobID)
	})

	t.Run("Success with Simple Header (No EResult)", func(t *testing.T) {
		// This tests the branch where Header exists but isn't an EHeader
		caller := &mockSocketCaller{
			session: &mockSession{authed: false},
			mockPacket: &protocol.Packet{
				Payload: []byte("payload"),
				Header:  simpleHeader{sourceJob: 888},
			},
		}
		tr := NewSocketTransport(caller)
		req := NewRequest(mockSocketTarget{emsg: 1}, nil)

		resp, err := tr.Do(ctx, req)
		require.NoError(t, err)

		meta, _ := resp.Socket()
		assert.Equal(t, enums.EResult_OK, meta.Result) // Default
		assert.Equal(t, uint64(888), meta.SourceJobID)
	})

	t.Run("Error - Disconnected (Nil Session)", func(t *testing.T) {
		caller := &mockSocketCaller{session: nil}
		tr := NewSocketTransport(caller)
		_, err := tr.Do(ctx, NewRequest(mockSocketTarget{}, nil))
		assert.ErrorContains(t, err, "socket is disconnected")
	})

	t.Run("Error - Unsupported Target", func(t *testing.T) {
		tr := NewSocketTransport(&mockSocketCaller{})
		_, err := tr.Do(ctx, NewRequest(mockTarget{name: "http_only"}, nil))
		assert.ErrorContains(t, err, "does not support socket protocol")
	})

	t.Run("Error - SendSync Failed", func(t *testing.T) {
		caller := &mockSocketCaller{
			session:     &mockSession{},
			mockCallErr: errors.New("network error"),
		}
		tr := NewSocketTransport(caller)
		_, err := tr.Do(ctx, NewRequest(mockSocketTarget{}, nil))
		assert.ErrorContains(t, err, "socket_transport call failed")
	})
}
