// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socket_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lemon4ksan/miyako/jobs"
	"github.com/lemon4ksan/miyako/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/lemon4ksan/g-man/pkg/network"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/connector"
)

type mockConnection struct {
	network.BaseConnection
	sendErr  error
	sentMsgs chan []byte

	msgChan    chan network.Message
	errChan    chan error
	closedChan chan struct{}
}

func newMockConnection() *mockConnection {
	return &mockConnection{
		sentMsgs:   make(chan []byte, 100),
		msgChan:    make(chan network.Message, 100),
		errChan:    make(chan error, 10),
		closedChan: make(chan struct{}),
	}
}

func (m *mockConnection) Name() string { return "mock" }

func (m *mockConnection) Send(_ context.Context, d []byte) error {
	cp := make([]byte, len(d))
	copy(cp, d)

	select {
	case m.sentMsgs <- cp:
	default:
	}

	return m.sendErr
}

func (m *mockConnection) Close() error                     { return nil }
func (m *mockConnection) Messages() <-chan network.Message { return m.msgChan }
func (m *mockConnection) Errors() <-chan error             { return m.errChan }
func (m *mockConnection) Closed() <-chan struct{}          { return m.closedChan }

func setupMockSocket(t *testing.T) (*socket.Socket, *mockConnection) {
	t.Helper()

	mConn := newMockConnection()
	cfg := socket.DefaultConfig()
	cfg.Connector.Dialers = map[string]connector.Dialer{
		"mock": func(ctx context.Context, l log.Logger, ep, _ string, _ http.Header) (network.Connection, error) {
			return mConn, nil
		},
	}

	s := socket.New(cfg)
	t.Cleanup(func() { s.Close() })

	return s, mConn
}

func TestSocket_DefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := socket.DefaultConfig()
	assert.NotNil(t, cfg.Connector)
	assert.NotNil(t, cfg.Processor)
	assert.True(t, cfg.MaxJobs > 0)
}

func TestSocket_LifecycleAndAccessors(t *testing.T) {
	t.Parallel()

	t.Run("accessors", func(t *testing.T) {
		t.Parallel()
		s, _ := setupMockSocket(t)

		assert.NotNil(t, s.Connector())
		assert.NotNil(t, s.Session())
		assert.False(t, s.IsConnected())
	})

	t.Run("update_servers", func(t *testing.T) {
		t.Parallel()
		s, _ := setupMockSocket(t)

		s.UpdateServers([]socket.CMServer{{Type: "mock", Endpoint: "127.0.0.1"}})
	})

	t.Run("encryption_key_wrapper", func(t *testing.T) {
		t.Parallel()
		s, _ := setupMockSocket(t)

		assert.False(t, s.SetEncryptionKey([]byte("secret")))
	})

	t.Run("session_implementation", func(t *testing.T) {
		t.Parallel()
		s, _ := setupMockSocket(t)

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

	t.Run("update_logger", func(t *testing.T) {
		t.Parallel()
		s, _ := setupMockSocket(t)

		s.UpdateLogger(log.Discard)
		assert.NotNil(t, s.Logger())
	})

	t.Run("is_connected_true", func(t *testing.T) {
		t.Parallel()
		s, _ := setupMockSocket(t)
		err := s.Connect(t.Context(), socket.CMServer{Type: "mock"})
		require.NoError(t, err)
		assert.True(t, s.IsConnected())
	})
}

func TestSocket_ClosedState(t *testing.T) {
	t.Parallel()

	s, _ := setupMockSocket(t)
	_ = s.Close()

	ctx := t.Context()
	assert.ErrorIs(t, s.Connect(ctx, socket.CMServer{}), socket.ErrClosed)
	assert.ErrorIs(t, s.Send(ctx, socket.Raw(enums.EMsg_ClientLogon, nil)), socket.ErrClosed)
	assert.ErrorIs(t, s.StartHeartbeat(time.Second), socket.ErrClosed)
}

func TestSocket_MessagingHelpers(t *testing.T) {
	t.Parallel()

	t.Run("send_raw", func(t *testing.T) {
		t.Parallel()
		s, mConn := setupMockSocket(t)
		err := s.Connect(t.Context(), socket.CMServer{Type: "mock"})
		require.NoError(t, err)

		err = s.SendRaw(t.Context(), enums.EMsg_ClientLogon, []byte("raw"))
		assert.NoError(t, err)

		select {
		case <-mConn.sentMsgs:
		case <-time.After(time.Second):
			t.Fatal("msg not sent")
		}
	})

	t.Run("send_proto", func(t *testing.T) {
		t.Parallel()
		s, mConn := setupMockSocket(t)
		err := s.Connect(t.Context(), socket.CMServer{Type: "mock"})
		require.NoError(t, err)

		err = s.SendProto(t.Context(), enums.EMsg_ClientLogon, &emptypb.Empty{})
		assert.NoError(t, err)

		select {
		case <-mConn.sentMsgs:
		case <-time.After(time.Second):
			t.Fatal("msg not sent")
		}
	})

	t.Run("send_unified", func(t *testing.T) {
		t.Parallel()
		s, mConn := setupMockSocket(t)
		err := s.Connect(t.Context(), socket.CMServer{Type: "mock"})
		require.NoError(t, err)

		err = s.SendUnified(t.Context(), "Method", &emptypb.Empty{})
		assert.NoError(t, err)

		select {
		case <-mConn.sentMsgs:
		case <-time.After(time.Second):
			t.Fatal("msg not sent")
		}
	})

	t.Run("send_dynamic_raw_proto", func(t *testing.T) {
		t.Parallel()
		s, mConn := setupMockSocket(t)
		err := s.Connect(t.Context(), socket.CMServer{Type: "mock"})
		require.NoError(t, err)

		err = s.Send(t.Context(), socket.DynamicRawProto(enums.EMsg_ClientLogon, []byte("data"), 440))
		assert.NoError(t, err)

		select {
		case data := <-mConn.sentMsgs:
			p, err := protocol.ParsePacket(bytes.NewReader(data))
			require.NoError(t, err)
			assert.True(t, p.IsProto)
			hdr := p.Header.(*protocol.MsgHdrProtoBuf)
			assert.Equal(t, uint32(440), hdr.Proto.GetRoutingAppid())

		case <-time.After(time.Second):
			t.Fatal("msg not sent")
		}
	})
}

func TestSocket_SendSync(t *testing.T) {
	t.Parallel()

	t.Run("successful_sync", func(t *testing.T) {
		t.Parallel()
		s, mConn := setupMockSocket(t)
		err := s.Connect(t.Context(), socket.CMServer{Type: "mock"})
		require.NoError(t, err)

		go func() {
			data := <-mConn.sentMsgs
			req, _ := protocol.ParsePacket(bytes.NewReader(data))

			resp := &protocol.Packet{EMsg: enums.EMsg_ClientLogOnResponse, IsProto: true}
			hdr := protocol.NewMsgHdrProtoBuf(enums.EMsg_ClientLogOnResponse, 0, 0)
			hdr.Proto.JobidTarget = proto.Uint64(req.GetSourceJobID())
			resp.Header = hdr
			resp.Payload = []byte("payload")

			buf := new(bytes.Buffer)

			_ = resp.SerializeTo(buf)
			mConn.msgChan <- buf.Bytes()
		}()

		resp, err := s.SendSync(t.Context(), socket.Proto(enums.EMsg_ClientLogon, nil))
		assert.NoError(t, err)
		assert.Equal(t, []byte("payload"), resp.Payload)
	})

	t.Run("context_cancellation", func(t *testing.T) {
		t.Parallel()
		s, _ := setupMockSocket(t)
		err := s.Connect(t.Context(), socket.CMServer{Type: "mock"})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err = s.SendSync(ctx, socket.Proto(enums.EMsg_ClientLogon, nil))
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("immediate_send_error", func(t *testing.T) {
		t.Parallel()
		s, _ := setupMockSocket(t)
		_, err := s.SendSync(t.Context(), socket.Proto(enums.EMsg_ClientLogon, nil))
		assert.Error(t, err)
	})

	t.Run("sendsync_job_cancelled_via_dispatcher_close", func(t *testing.T) {
		t.Parallel()
		s, mConn := setupMockSocket(t)
		err := s.Connect(t.Context(), socket.CMServer{Type: "mock"})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		go func() {
			<-mConn.sentMsgs

			_ = s.Close()
		}()

		_, err = s.SendSync(ctx, socket.Proto(enums.EMsg_ClientLogon, nil))
		assert.ErrorIs(t, err, jobs.ErrJobClosed)
	})
}

func TestSocket_SendAsync(t *testing.T) {
	t.Parallel()

	t.Run("sendasync_success", func(t *testing.T) {
		t.Parallel()
		s, mConn := setupMockSocket(t)
		err := s.Connect(t.Context(), socket.CMServer{Type: "mock"})
		require.NoError(t, err)

		go func() {
			data := <-mConn.sentMsgs
			req, _ := protocol.ParsePacket(bytes.NewReader(data))

			resp := &protocol.Packet{EMsg: enums.EMsg_ClientLogOnResponse, IsProto: true}
			hdr := protocol.NewMsgHdrProtoBuf(enums.EMsg_ClientLogOnResponse, 0, 0)
			hdr.Proto.JobidTarget = proto.Uint64(req.GetSourceJobID())
			resp.Header = hdr
			resp.Payload = []byte("async_payload")

			buf := new(bytes.Buffer)

			_ = resp.SerializeTo(buf)
			mConn.msgChan <- buf.Bytes()
		}()

		future := s.SendAsync(t.Context(), socket.Proto(enums.EMsg_ClientLogon, nil))
		resp, err := future.Get(t.Context())
		assert.NoError(t, err)
		assert.Equal(t, []byte("async_payload"), resp.Payload)
	})
}

func TestSocket_Heartbeat(t *testing.T) {
	t.Parallel()

	t.Run("heartbeat_loop_logic", func(t *testing.T) {
		t.Parallel()
		s, mConn := setupMockSocket(t)
		err := s.Connect(t.Context(), socket.CMServer{Type: "mock"})
		require.NoError(t, err)

		err = s.StartHeartbeat(5 * time.Millisecond)
		assert.NoError(t, err)

		select {
		case data := <-mConn.sentMsgs:
			p, _ := protocol.ParsePacket(bytes.NewReader(data))
			assert.Equal(t, enums.EMsg_ClientHeartBeat, p.EMsg)
		case <-time.After(1 * time.Second):
			t.Fatal("Heartbeat not sent")
		}

		err = s.Disconnect()
		assert.NoError(t, err)
		time.Sleep(15 * time.Millisecond)
	})

	t.Run("failed_heartbeat_send", func(t *testing.T) {
		t.Parallel()
		s, mConn := setupMockSocket(t)
		err := s.Connect(t.Context(), socket.CMServer{Type: "mock"})
		require.NoError(t, err)

		mConn.sendErr = errors.New("broken")

		err = s.StartHeartbeat(5 * time.Millisecond)
		assert.NoError(t, err)

		select {
		case <-mConn.sentMsgs:
		case <-time.After(1 * time.Second):
			t.Fatal("Heartbeat was not triggered")
		}
	})

	t.Run("heartbeat_re_start", func(t *testing.T) {
		t.Parallel()
		s, _ := setupMockSocket(t)
		err := s.Connect(t.Context(), socket.CMServer{Type: "mock"})
		require.NoError(t, err)

		err = s.StartHeartbeat(100 * time.Millisecond)
		assert.NoError(t, err)

		err = s.StartHeartbeat(100 * time.Millisecond)
		assert.NoError(t, err)
	})

	t.Run("heartbeat_stopped_on_close", func(t *testing.T) {
		t.Parallel()
		s, _ := setupMockSocket(t)
		err := s.Connect(t.Context(), socket.CMServer{Type: "mock"})
		require.NoError(t, err)

		err = s.StartHeartbeat(time.Second)
		assert.NoError(t, err)

		err = s.Close()
		assert.NoError(t, err)
	})
}

func TestSocket_Registration(t *testing.T) {
	t.Parallel()

	t.Run("msg_handlers", func(t *testing.T) {
		t.Parallel()
		s, _ := setupMockSocket(t)

		called := atomic.Bool{}
		h := func(p *protocol.Packet) { called.Store(true) }

		s.RegisterMsgHandler(enums.EMsg_ClientLogon, h)
		s.Dispatcher().Dispatch(&protocol.Packet{EMsg: enums.EMsg_ClientLogon})
		assert.True(t, called.Load())

		s.UnregisterMsgHandler(enums.EMsg_ClientLogon)
		called.Store(false)
		s.Dispatcher().Dispatch(&protocol.Packet{EMsg: enums.EMsg_ClientLogon})
		assert.False(t, called.Load())
	})

	t.Run("service_handlers", func(t *testing.T) {
		t.Parallel()
		s, _ := setupMockSocket(t)

		called := atomic.Bool{}
		h := func(p *protocol.Packet) { called.Store(true) }

		s.RegisterServiceHandler("Method", h)

		hdr := protocol.NewMsgHdrProtoBuf(enums.EMsg_ServiceMethod, 0, 0)
		hdr.Proto.TargetJobName = proto.String("Method")
		s.Dispatcher().Dispatch(&protocol.Packet{EMsg: enums.EMsg_ServiceMethod, Header: hdr})
		assert.True(t, called.Load())

		s.UnregisterServiceHandler("Method")
		called.Store(false)
		s.Dispatcher().Dispatch(&protocol.Packet{EMsg: enums.EMsg_ServiceMethod, Header: hdr})
		assert.False(t, called.Load())
	})
}

func TestSocket_Close(t *testing.T) {
	t.Parallel()

	t.Run("close_twice", func(t *testing.T) {
		t.Parallel()
		s, _ := setupMockSocket(t)
		err := s.Close()
		assert.NoError(t, err)

		err = s.Close()
		assert.NoError(t, err)
	})
}
