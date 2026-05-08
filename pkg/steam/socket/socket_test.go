// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socket

import (
	"bytes"
	"context"
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
)

// setupMockSocket initializes a complete Socket facade with a mocked network layer.
func setupMockSocket(cfg Config) (*Socket, *mockConnection) {
	mConn := newMockConnection()

	// Inject the mock dialer into the connector config
	cfg.Connector.Dialers = map[string]connector.Dialer{
		"mock": func(ctx context.Context, nh network.Handler, l log.Logger, ep string) (network.Connection, error) {
			return mConn, nil
		},
	}

	s := NewSocket(cfg)

	return s, mConn
}

func packProto(eMsg enums.EMsg, targetJob uint64, payload []byte) []byte {
	pkt := &protocol.Packet{
		EMsg:    eMsg,
		IsProto: true,
		Header: &protocol.MsgHdrProtoBuf{
			EMsg: eMsg,
			Proto: &pb.CMsgProtoBufHeader{
				JobidTarget: proto.Uint64(targetJob),
			},
		},
		Payload: payload,
	}
	buf := new(bytes.Buffer)
	_ = pkt.SerializeTo(buf)

	return buf.Bytes()
}

func TestSocket_Initialization(t *testing.T) {
	t.Run("Default Components", func(t *testing.T) {
		s := NewSocket(DefaultConfig())
		defer s.Close()

		// Verify that the facade correctly initialized all its sub-components
		assert.NotNil(t, s.conn)
		assert.NotNil(t, s.proc)
		assert.NotNil(t, s.dispatch)
		assert.NotNil(t, s.session)
		assert.NotNil(t, s.jobManager)
	})

	t.Run("With Options", func(t *testing.T) {
		l := log.Discard

		s := NewSocket(DefaultConfig(), WithLogger(l))
		defer s.Close()

		assert.NotNil(t, s.logger)
	})
}

func TestSocket_ConnectLifecycle(t *testing.T) {
	s, _ := setupMockSocket(DefaultConfig())
	defer s.Close()

	err := s.Connect(context.Background(), connector.CMServer{Type: "mock", Endpoint: "localhost"})
	require.NoError(t, err)
	assert.True(t, s.IsConnected())

	// Set fake session data to verify cleanup
	s.session.SetSessionID(999)

	err = s.Disconnect()
	assert.NoError(t, err)
	assert.Equal(t, int32(0), s.session.SessionID(), "SessionID should be cleared on disconnect")
}

func TestSocket_RoutingIntegration(t *testing.T) {
	s, _ := setupMockSocket(DefaultConfig())

	s.proc.Start()
	defer s.Close()

	called := make(chan struct{})
	s.RegisterMsgHandler(enums.EMsg_ClientLogOnResponse, func(p *protocol.Packet) {
		close(called)
	})

	pktData := packProto(enums.EMsg_ClientLogOnResponse, 0, nil)

	s.proc.Process(pktData)

	select {
	case <-called:
		// Success
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Handler should be called via Processor pool")
	}
}

func TestSocket_JobSystem(t *testing.T) {
	s, mConn := setupMockSocket(DefaultConfig())

	s.proc.Start()
	defer s.Close()

	_ = s.Connect(context.Background(), connector.CMServer{Type: "mock"})

	t.Run("Successful Sync Request", func(t *testing.T) {
		go func() {
			// Wait for the outgoing request
			data := <-mConn.sentMsgs
			req, _ := protocol.ParsePacket(bytes.NewReader(data))

			// Craft a fake response from Steam targeting the specific JobID
			resp := &protocol.Packet{
				EMsg:    enums.EMsg_ClientLogOnResponse,
				IsProto: true,
				Header:  protocol.NewMsgHdrProtoBuf(enums.EMsg_ClientLogOnResponse, 0, 0),
				Payload: []byte("success"),
			}
			resp.Header.(*protocol.MsgHdrProtoBuf).Proto.JobidTarget = proto.Uint64(req.GetSourceJobID())

			buf := new(bytes.Buffer)
			_ = resp.SerializeTo(buf)

			// Inject response into the pipeline
			s.proc.Process(buf.Bytes())
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		// Send request and block until response is routed back via JobManager
		resp, err := s.SendSync(ctx, Proto(enums.EMsg_ClientLogon, nil))

		require.NoError(t, err)
		assert.Equal(t, []byte("success"), resp.Payload)
	})
}

func TestSocket_Close(t *testing.T) {
	s := NewSocket(DefaultConfig())
	s.proc.Start()

	// Verify subsystems are running
	s.RegisterMsgHandler(enums.EMsg_ClientHeartBeat, func(p *protocol.Packet) {})

	err := s.Close()
	require.NoError(t, err)

	// Test idempotency
	err = s.Close()
	assert.NoError(t, err)

	// Verify Send fails after close
	err = s.Send(context.Background(), Raw(enums.EMsg_ClientHeartBeat, nil))
	assert.ErrorIs(t, err, ErrClosed)
}

func TestSocket_PanicRecovery(t *testing.T) {
	s := NewSocket(DefaultConfig())

	s.proc.Start()
	defer s.Close()

	// Register a handler that panics
	s.RegisterMsgHandler(enums.EMsg_ClientHeartBeat, func(p *protocol.Packet) {
		panic("internal handler crash")
	})

	// Dispatch packet manually (bypassing Processor for direct test)
	assert.NotPanics(t, func() {
		s.dispatch.Dispatch(&protocol.Packet{EMsg: enums.EMsg_ClientHeartBeat})
	})
}

func TestSocket_InternalUtilities(t *testing.T) {
	s := NewSocket(DefaultConfig())
	defer s.Close()

	t.Run("Buffer Pool", func(t *testing.T) {
		buf := s.getBuffer()
		assert.NotNil(t, buf)
		assert.Equal(t, 0, buf.Len())

		buf.WriteString("some data")
		s.putBuffer(buf)

		buf2 := s.getBuffer()
		assert.Equal(t, 0, buf2.Len(), "Pooled buffer must be reset")
	})

	t.Run("NewPacket Logic", func(t *testing.T) {
		s.session.SetSteamID(12345)
		s.session.SetSessionID(999)

		// Test Proto Header
		pkt := newPacket(s.session, enums.EMsg_ClientLogon, 777, true, "Target.Name", "token")
		hdr := pkt.Header.(*protocol.MsgHdrProtoBuf)

		assert.Equal(t, uint64(12345), hdr.Proto.GetSteamid())
		assert.Equal(t, int32(999), hdr.Proto.GetClientSessionid())
		assert.Equal(t, uint64(777), hdr.Proto.GetJobidSource())
		assert.Equal(t, "Target.Name", hdr.Proto.GetTargetJobName())
		assert.Equal(t, "token", hdr.Proto.GetWgToken())

		// Test Extended Header (Raw)
		pktRaw := newPacket(s.session, enums.EMsg_ChannelEncryptResponse, 888, false, "", "")
		hdrRaw := pktRaw.Header.(*protocol.MsgHdrExtended)
		assert.Equal(t, uint64(888), hdrRaw.SourceJobID)
		assert.Equal(t, uint64(12345), hdrRaw.SteamID)
	})
}
