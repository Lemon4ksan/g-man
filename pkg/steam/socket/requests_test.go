// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socket

import (
	"bytes"
	"context"
	"errors"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/connector"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/network"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/session"
)

type mockConnection struct {
	network.BaseConnection
	sendFunc func(ctx context.Context, data []byte) error
	sentMsgs chan []byte
}

func newMockConnection() *mockConnection {
	return &mockConnection{
		sendFunc: func(ctx context.Context, data []byte) error { return nil },
		sentMsgs: make(chan []byte, 10),
	}
}

func (m *mockConnection) Name() string {
	return "mock"
}

func (m *mockConnection) Send(ctx context.Context, d []byte) error {
	m.sentMsgs <- d
	return m.sendFunc(ctx, d)
}
func (m *mockConnection) Close() error { return nil }

func TestPayloadBuilders(t *testing.T) {
	sess := &session.Session{}
	sess.SetSteamID(76561197960287930)

	buf := new(bytes.Buffer)

	t.Run("Proto Builder", func(t *testing.T) {
		buf.Reset()
		err := Proto(enums.EMsg_ClientLogon, &pb.CMsgClientLogon{
			AccountName: proto.String("test"),
		})(sess, buf, 123, "")

		require.NoError(t, err)
		pkt, err := protocol.ParsePacket(buf)
		require.NoError(t, err)

		assert.True(t, pkt.IsProto)
		hdr := pkt.Header.(*protocol.MsgHdrProtoBuf)
		assert.Equal(t, uint64(123), hdr.Proto.GetJobidSource())
		assert.Equal(t, uint64(76561197960287930), hdr.Proto.GetSteamid())
	})

	t.Run("Unified Builder", func(t *testing.T) {
		buf.Reset()
		err := Unified("Player.GetNickname#1", nil)(sess, buf, 456, "token_abc")

		require.NoError(t, err)

		pkt, _ := protocol.ParsePacket(buf)
		hdr := pkt.Header.(*protocol.MsgHdrProtoBuf)

		assert.Equal(t, "Player.GetNickname#1", hdr.Proto.GetTargetJobName())
		assert.Equal(t, "token_abc", hdr.Proto.GetWgToken())
	})

	t.Run("Raw Builder", func(t *testing.T) {
		buf.Reset()

		payload := []byte{0xDE, 0xAD, 0xBE, 0xEF}
		err := Raw(enums.EMsg_ClientHeartBeat, payload)(sess, buf, 789, "")
		require.NoError(t, err)

		pkt, err := protocol.ParsePacket(buf)
		require.NoError(t, err)

		assert.False(t, pkt.IsProto)
		hdr, ok := pkt.Header.(*protocol.MsgHdrExtended)
		require.True(t, ok, "Header should be MsgHdrExtended")
		assert.Equal(t, uint64(789), hdr.SourceJobID)
		assert.Equal(t, payload, pkt.Payload)
	})
}

func TestSocket_Send(t *testing.T) {
	mConn := newMockConnection()
	dialers := map[string]connector.Dialer{
		"mock": func(ctx context.Context, nh network.Handler, l log.Logger, ep string) (network.Connection, error) {
			return mConn, nil
		},
	}

	cfg := DefaultConfig()
	cfg.Connector.Dialers = dialers

	s := NewSocket(cfg)
	defer s.Close()

	// Connect to initialize transport
	err := s.Connect(context.Background(), connector.CMServer{Type: "mock", Endpoint: "localhost"})
	require.NoError(t, err)

	t.Run("Builder Error Cleanup", func(t *testing.T) {
		cbCalled := make(chan struct{})
		errBuild := errors.New("fail")

		err := s.Send(
			context.Background(),
			func(sess Session, buf *bytes.Buffer, jid uint64, token string) error {
				return errBuild
			},
			WithCallback(func(p *protocol.Packet, e error) {
				assert.ErrorIs(t, e, errBuild)
				close(cbCalled)
			}),
		)

		assert.ErrorIs(t, err, errBuild)

		select {
		case <-cbCalled:
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Callback must be resolved even if builder fails")
		}
	})

	t.Run("Fatal Network Error Trigger", func(t *testing.T) {
		mConn.sendFunc = func(ctx context.Context, data []byte) error {
			return syscall.ECONNRESET
		}

		err := s.SendProto(context.Background(), enums.EMsg_ClientHeartBeat, nil)
		assert.Error(t, err)
	})
}

func TestSocket_SendSync(t *testing.T) {
	mConn := newMockConnection()
	dialers := map[string]connector.Dialer{
		"mock": func(ctx context.Context, nh network.Handler, l log.Logger, ep string) (network.Connection, error) {
			return mConn, nil
		},
	}

	cfg := DefaultConfig()
	cfg.Connector.Dialers = dialers

	s := NewSocket(cfg)
	defer s.Close()

	_ = s.Connect(context.Background(), connector.CMServer{Type: "mock"})

	// Mock response logic
	go func() {
		data := <-mConn.sentMsgs
		req, _ := protocol.ParsePacket(bytes.NewReader(data))

		// Create a response packet with matching TargetJobID
		respHdr := protocol.NewMsgHdrProtoBuf(enums.EMsg_ClientHeartBeat, 0, 0)
		respHdr.Proto.JobidTarget = proto.Uint64(req.GetSourceJobID())

		resp := &protocol.Packet{
			EMsg:    enums.EMsg_ClientHeartBeat,
			IsProto: true,
			Header:  respHdr,
			Payload: []byte("pong"),
		}

		// Manually dispatch response into the socket
		s.RegisterMsgHandler(
			enums.EMsg_ClientHeartBeat,
			func(p *protocol.Packet) {},
		) // Ensure handler exists or just use Dispatch
		// Since we want to test Sync, we rely on the Dispatcher -> JobManager path
		// We access internal dispatcher via reflection or provide a test hook.
		// For this test, we use the fact that Connector calls Processor calls Dispatcher.

		rawResp := new(bytes.Buffer)
		_ = resp.SerializeTo(rawResp)

		// Simulate network incoming
		s.conn.OnNetMessage(rawResp.Bytes())
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	resp, err := s.SendSync(ctx, Proto(enums.EMsg_ClientHeartBeat, nil))
	require.NoError(t, err)
	assert.Equal(t, []byte("pong"), resp.Payload)
}

func TestSocket_JobCancellation(t *testing.T) {
	s := NewSocket(DefaultConfig())
	defer s.Close()

	t.Run("Timeout Resolution", func(t *testing.T) {
		s, _ := setupMockSocket(DefaultConfig())
		defer s.Close()

		_ = s.Connect(context.Background(), connector.CMServer{Type: "mock", Endpoint: "localhost"})

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		cbErr := make(chan error, 1)

		err := s.Send(ctx, Raw(enums.EMsg_ClientHeartBeat, nil),
			WithCallback(func(p *protocol.Packet, err error) {
				cbErr <- err
			}),
		)
		require.NoError(t, err)

		select {
		case err := <-cbErr:
			assert.ErrorIs(t, err, context.DeadlineExceeded)
		case <-time.After(200 * time.Millisecond):
			t.Fatal("Job callback was not notified of timeout")
		}
	})
}
