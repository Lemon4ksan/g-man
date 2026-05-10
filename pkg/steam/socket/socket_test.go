// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socket

import (
	"bytes"
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/connector"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/network"
)

type mockConnection struct {
	network.BaseConnection
	sendErr  error
	sentMsgs chan []byte
}

func (m *mockConnection) Name() string { return "mock" }
func (m *mockConnection) Send(_ context.Context, d []byte) error {
	if m.sendErr != nil {
		return m.sendErr
	}

	m.sentMsgs <- d

	return nil
}
func (m *mockConnection) Close() error { return nil }

func setupMockSocket(t *testing.T) (*Socket, *mockConnection) {
	mConn := &mockConnection{sentMsgs: make(chan []byte, 100)}
	cfg := DefaultConfig()
	cfg.Connector.Dialers = map[string]connector.Dialer{
		"mock": func(ctx context.Context, nh network.Handler, l log.Logger, ep string) (network.Connection, error) {
			return mConn, nil
		},
	}

	s := NewSocket(cfg, log.Discard)
	t.Cleanup(func() { s.Close() })

	return s, mConn
}

func TestSocket_LifecycleAndAccessors(t *testing.T) {
	s, _ := setupMockSocket(t)

	t.Run("Accessors", func(t *testing.T) {
		assert.NotNil(t, s.Connector())
		assert.NotNil(t, s.Session())
		assert.False(t, s.IsConnected())
	})

	t.Run("UpdateServers", func(t *testing.T) {
		// Just verify it doesn't panic
		s.UpdateServers([]CMServer{{Type: "mock", Endpoint: "127.0.0.1"}})
	})

	t.Run("EncryptionKey Wrapper", func(t *testing.T) {
		// Cannot set key on nil connection
		assert.False(t, s.SetEncryptionKey([]byte("secret")))
	})

	t.Run("Session implementation", func(t *testing.T) {
		sess := s.Session()
		sess.SetSteamID(123)
		sess.SetSessionID(456)
		sess.SetAccessToken("at")
		sess.SetRefreshToken("rt")

		assert.Equal(t, uint64(123), sess.SteamID())
		assert.Equal(t, int32(456), sess.SessionID())
		assert.Equal(t, "at", sess.AccessToken())
		assert.Equal(t, "rt", sess.RefreshToken())
		assert.True(t, sess.IsAuthenticated())
	})
}

func TestSocket_ClosedState(t *testing.T) {
	s, _ := setupMockSocket(t)
	_ = s.Close()

	ctx := context.Background()
	assert.ErrorIs(t, s.Connect(ctx, CMServer{}), ErrClosed)
	assert.ErrorIs(t, s.Send(ctx, Raw(enums.EMsg_ClientLogon, nil)), ErrClosed)
	assert.ErrorIs(t, s.StartHeartbeat(time.Second), ErrClosed)
}

func TestSocket_MessagingHelpers(t *testing.T) {
	s, mConn := setupMockSocket(t)
	_ = s.Connect(context.Background(), CMServer{Type: "mock"})

	t.Run("SendRaw", func(t *testing.T) {
		err := s.SendRaw(context.Background(), enums.EMsg_ClientLogon, []byte("raw"))
		assert.NoError(t, err)
		<-mConn.sentMsgs
	})

	t.Run("SendProto", func(t *testing.T) {
		err := s.SendProto(context.Background(), enums.EMsg_ClientLogon, &emptypb.Empty{})
		assert.NoError(t, err)
		<-mConn.sentMsgs
	})

	t.Run("SendUnified", func(t *testing.T) {
		err := s.SendUnified(context.Background(), "Method", &emptypb.Empty{})
		assert.NoError(t, err)
		<-mConn.sentMsgs
	})
}

func TestSocket_SendSync(t *testing.T) {
	t.Run("Successful sync", func(t *testing.T) {
		s, mConn := setupMockSocket(t)
		_ = s.Connect(context.Background(), CMServer{Type: "mock"})

		go func() {
			data := <-mConn.sentMsgs
			req, _ := protocol.ParsePacket(bytes.NewReader(data))

			// Response
			resp := &protocol.Packet{EMsg: enums.EMsg_ClientLogOnResponse, IsProto: true}
			hdr := protocol.NewMsgHdrProtoBuf(enums.EMsg_ClientLogOnResponse, 0, 0)
			hdr.Proto.JobidTarget = proto.Uint64(req.GetSourceJobID())
			resp.Header = hdr
			resp.Payload = []byte("payload")

			buf := new(bytes.Buffer)
			_ = resp.SerializeTo(buf)
			s.conn.OnNetMessage(buf.Bytes())
		}()

		resp, err := s.SendSync(context.Background(), Proto(enums.EMsg_ClientLogon, nil))
		assert.NoError(t, err)
		assert.Equal(t, []byte("payload"), resp.Payload)
	})

	t.Run("Context cancellation", func(t *testing.T) {
		s, _ := setupMockSocket(t)
		_ = s.Connect(context.Background(), CMServer{Type: "mock"})

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := s.SendSync(ctx, Proto(enums.EMsg_ClientLogon, nil))
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("Immediate Send error", func(t *testing.T) {
		s, _ := setupMockSocket(t)
		// No connection = Send error
		_, err := s.SendSync(context.Background(), Proto(enums.EMsg_ClientLogon, nil))
		assert.Error(t, err)
	})
}

func TestSocket_Heartbeat(t *testing.T) {
	t.Run("Heartbeat Loop Logic", func(t *testing.T) {
		s, mConn := setupMockSocket(t)
		_ = s.Connect(context.Background(), CMServer{Type: "mock"})

		// Use very fast interval for test
		err := s.StartHeartbeat(10 * time.Millisecond)
		assert.NoError(t, err)

		// Verify at least one heartbeat was sent
		select {
		case data := <-mConn.sentMsgs:
			p, _ := protocol.ParsePacket(bytes.NewReader(data))
			assert.Equal(t, enums.EMsg_ClientHeartBeat, p.EMsg)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Heartbeat not sent")
		}

		// Drop connection to stop loop
		_ = s.Disconnect()

		// Wait a bit to ensure it doesn't panic while disconnected
		time.Sleep(50 * time.Millisecond)
	})

	t.Run("Failed Heartbeat Send", func(t *testing.T) {
		s, mConn := setupMockSocket(t)
		_ = s.Connect(context.Background(), CMServer{Type: "mock"})

		mConn.sendErr = errors.New("broken")

		err := s.StartHeartbeat(5 * time.Millisecond)
		assert.NoError(t, err)

		time.Sleep(20 * time.Millisecond)
		// Should log warning but not crash
	})
}

func TestSocket_Registration(t *testing.T) {
	s, _ := setupMockSocket(t)

	t.Run("Msg Handlers", func(t *testing.T) {
		called := atomic.Bool{}
		h := func(p *protocol.Packet) { called.Store(true) }

		s.RegisterMsgHandler(enums.EMsg_ClientLogon, h)
		s.dispatch.Dispatch(&protocol.Packet{EMsg: enums.EMsg_ClientLogon})
		assert.True(t, called.Load())

		s.UnregisterMsgHandler(enums.EMsg_ClientLogon)
		called.Store(false)
		s.dispatch.Dispatch(&protocol.Packet{EMsg: enums.EMsg_ClientLogon})
		assert.False(t, called.Load())
	})

	t.Run("Service Handlers", func(t *testing.T) {
		called := atomic.Bool{}
		h := func(p *protocol.Packet) { called.Store(true) }

		s.RegisterServiceHandler("Method", h)

		hdr := protocol.NewMsgHdrProtoBuf(enums.EMsg_ServiceMethod, 0, 0)
		hdr.Proto.TargetJobName = proto.String("Method")
		s.dispatch.Dispatch(&protocol.Packet{EMsg: enums.EMsg_ServiceMethod, Header: hdr})
		assert.True(t, called.Load())

		s.UnregisterServiceHandler("Method")
		called.Store(false)
		s.dispatch.Dispatch(&protocol.Packet{EMsg: enums.EMsg_ServiceMethod, Header: hdr})
		assert.False(t, called.Load())
	})
}
