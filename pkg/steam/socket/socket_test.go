// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socket

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/internal/network"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/internal/session"
)

func DefaultTestConfig() Config {
	return Config{
		EventChanSize: 5,
	}
}

type mockConnection struct {
	network.BaseConnection
	sendFunc  func(ctx context.Context, data []byte) error
	closeFunc func() error
	sentMsgs  chan []byte
}

func newMockConnection() *mockConnection {
	return &mockConnection{
		BaseConnection: network.NewBaseConnection("mock"),
		sendFunc:       func(ctx context.Context, data []byte) error { return nil },
		closeFunc:      func() error { return nil },
		sentMsgs:       make(chan []byte, 10),
	}
}

func (m *mockConnection) Name() string { return "MOCK" }
func (m *mockConnection) Send(ctx context.Context, data []byte) error {
	m.sentMsgs <- data
	return m.sendFunc(ctx, data)
}
func (m *mockConnection) Close() error { return m.closeFunc() }

func packProto(eMsg enums.EMsg, jobId uint64, payload []byte) []byte {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, uint32(eMsg)|0x80000000)

	hdr := &pb.CMsgProtoBufHeader{JobidTarget: proto.Uint64(jobId)}
	hdrBytes, _ := proto.Marshal(hdr)
	_ = binary.Write(buf, binary.LittleEndian, uint32(len(hdrBytes)))
	buf.Write(hdrBytes)
	buf.Write(payload)

	return buf.Bytes()
}

func packExtended(eMsg enums.EMsg, steamID uint64, sessionID int32, payload []byte) []byte {
	buf := new(bytes.Buffer)

	_ = binary.Write(buf, binary.LittleEndian, uint32(eMsg))           // EMsg
	buf.WriteByte(36)                                                  // HeaderSize
	_ = binary.Write(buf, binary.LittleEndian, uint16(2))              // HeaderVer
	_ = binary.Write(buf, binary.LittleEndian, uint64(math.MaxUint64)) // TargetJobID
	_ = binary.Write(buf, binary.LittleEndian, uint64(math.MaxUint64)) // SourceJobID
	buf.WriteByte(239)                                                 // Canary
	_ = binary.Write(buf, binary.LittleEndian, steamID)                // SteamID
	_ = binary.Write(buf, binary.LittleEndian, sessionID)              // SessionID
	buf.Write(payload)

	return buf.Bytes()
}

func packBasic(eMsg enums.EMsg, targetJob, sourceJob uint64, payload []byte) []byte {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, uint32(eMsg))
	_ = binary.Write(buf, binary.LittleEndian, targetJob)
	_ = binary.Write(buf, binary.LittleEndian, sourceJob)
	buf.Write(payload)

	return buf.Bytes()
}

func TestSocket_Initialization(t *testing.T) {
	sock := NewSocket(DefaultTestConfig())
	defer sock.Close()

	if sock.State() != StateDisconnected {
		t.Errorf("Expected initial state to be Disconnected, got %v", sock.State())
	}

	if sock.Bus() == nil {
		t.Error("Expected event bus to be initialized")
	}
}

func TestSocket_HandlersManagement(t *testing.T) {
	sock := NewSocket(DefaultTestConfig())
	defer sock.Close()

	var called atomic.Bool

	sock.RegisterMsgHandler(enums.EMsg_ClientLogon, func(p *protocol.Packet) {
		called.Store(true)
	})

	sock.handlersMu.RLock()
	_, exists := sock.handlers[enums.EMsg_ClientLogon]
	sock.handlersMu.RUnlock()

	if !exists {
		t.Error("Handler was not registered")
	}

	sock.ClearHandlers()

	sock.handlersMu.RLock()
	_, exists = sock.handlers[enums.EMsg_ClientLogon]
	sock.handlersMu.RUnlock()

	if exists {
		t.Error("Handler should be removed after ClearHandlers")
	}
}

func TestSocket_ConnectAndDisconnect(t *testing.T) {
	cfg := DefaultTestConfig()
	cfg.Dialers = map[string]ConnectionDialer{
		"mock": func(ctx context.Context, nh network.Handler, l log.Logger, s string) (network.Connection, error) {
			return newMockConnection(), nil
		},
	}

	eventBus := bus.New()

	sock := NewSocket(cfg, WithBus(eventBus))
	defer sock.Close()

	sub := eventBus.Subscribe(ConnectedEvent{}, DisconnectedEvent{})
	defer sub.Unsubscribe()

	err := sock.Connect(CMServer{Type: "mock", Endpoint: "localhost:1234"})
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	if sock.State() != StateConnected {
		t.Errorf("Expected state to be Connected, got %v", sock.State())
	}

	select {
	case ev := <-sub.C():
		if _, ok := ev.(*ConnectedEvent); !ok {
			t.Errorf("Expected ConnectedEvent, got %T", ev)
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for ConnectedEvent")
	}

	sock.Disconnect()

	if sock.State() != StateDisconnected {
		t.Errorf("Expected state to be Disconnected, got %v", sock.State())
	}

	select {
	case ev := <-sub.C():
		if _, ok := ev.(*DisconnectedEvent); !ok {
			t.Errorf("Expected DisconnectedEvent, got %T", ev)
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for DisconnectedEvent")
	}
}

func TestSocket_Routing(t *testing.T) {
	sock := NewSocket(DefaultTestConfig())
	defer sock.Close()

	sock.startWorkers()

	wg := sync.WaitGroup{}
	wg.Add(1)

	sock.RegisterMsgHandler(enums.EMsg_ClientLogOnResponse, func(p *protocol.Packet) {
		if string(p.Payload) != "test_payload" {
			t.Errorf("Unexpected payload: %s", string(p.Payload))
		}

		wg.Done()
	})

	packet := &protocol.Packet{
		EMsg:    enums.EMsg_ClientLogOnResponse,
		Payload: []byte("test_payload"),
	}

	sock.msgCh <- packet

	done := make(chan struct{})

	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("Timeout waiting for handler to be called")
	}
}

func TestSocket_JobTracking(t *testing.T) {
	cfg := DefaultTestConfig()

	conn := newMockConnection()
	cfg.Dialers = map[string]ConnectionDialer{
		"mock": func(ctx context.Context, nh network.Handler, l log.Logger, s string) (network.Connection, error) {
			return conn, nil
		},
	}

	sock := NewSocket(cfg)
	defer sock.Close()

	if err := sock.Connect(CMServer{Type: "mock"}); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	wg := sync.WaitGroup{}
	wg.Add(1)

	var (
		receivedErr   error
		receivedResp  *protocol.Packet
		capturedJobID uint64
	)

	builder := func(sess Session, buf *bytes.Buffer, sourceJobID uint64, token string) error {
		capturedJobID = sourceJobID
		return Raw(enums.EMsg_ClientGamesPlayed, []byte("data"))(sess, buf, sourceJobID, token)
	}

	err := sock.Send(t.Context(), builder,
		WithCallback(func(resp *protocol.Packet, err error) {
			receivedResp, receivedErr = resp, err

			wg.Done()
		}),
	)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if capturedJobID == 0 || capturedJobID == protocol.NoJob {
		t.Fatalf("Invalid job ID captured: %d", capturedJobID)
	}

	respPacket := &protocol.Packet{
		EMsg: enums.EMsg_ClientGamesPlayed,
		Header: &protocol.MsgHdr{
			TargetJobID: capturedJobID,
			SourceJobID: protocol.NoJob,
		},
		Payload: []byte("response_data"),
	}

	sock.msgCh <- respPacket

	done := make(chan struct{})

	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		if receivedErr != nil {
			t.Errorf("Unexpected job error: %v", receivedErr)
		}

		if string(receivedResp.Payload) != "response_data" {
			t.Errorf("Expected payload 'response_data', got '%s'", string(receivedResp.Payload))
		}

	case <-time.After(2 * time.Second):
		t.Error("Timeout: Job callback was never called. Correlation by JobID failed.")
	}
}

func TestSocket_SessionUpdateFromHeader(t *testing.T) {
	mockConn := newMockConnection()
	sess := session.New(mockConn)

	sock := NewSocket(DefaultTestConfig(), WithSession(sess))
	defer sock.Close()

	packet := &protocol.Packet{
		EMsg: enums.EMsg_ClientLogOnResponse,
		Header: &protocol.MsgHdrExtended{
			SteamID:   76561197960287930,
			SessionID: 123456,
		},
		Payload: []byte{},
	}

	sock.routePacket(packet)

	if sess.SteamID() != 76561197960287930 {
		t.Errorf("SteamID was not updated. Got %d", sess.SteamID())
	}

	if sess.SessionID() != 123456 {
		t.Errorf("SessionID was not updated. Got %d", sess.SessionID())
	}
}

func TestSocket_HandleMultiPacket(t *testing.T) {
	sock := NewSocket(DefaultTestConfig())

	sock.startWorkers()
	defer sock.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	sock.RegisterMsgHandler(enums.EMsg_ClientLogOnResponse, func(p *protocol.Packet) {
		if string(p.Payload) != "sub_payload_1" {
			t.Errorf("Wrong payload 1: %s", string(p.Payload))
		}

		wg.Done()
	})
	sock.RegisterMsgHandler(enums.EMsg_ClientPersonaState, func(p *protocol.Packet) {
		if string(p.Payload) != "sub_payload_2" {
			t.Errorf("Wrong payload 2: %s", string(p.Payload))
		}

		wg.Done()
	})

	subPkt1 := packProto(enums.EMsg_ClientLogOnResponse, 0, []byte("sub_payload_1"))
	subPkt2 := packProto(enums.EMsg_ClientPersonaState, 0, []byte("sub_payload_2"))

	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, uint32(len(subPkt1)))
	buf.Write(subPkt1)
	_ = binary.Write(buf, binary.LittleEndian, uint32(len(subPkt2)))
	buf.Write(subPkt2)

	var gzipBuf bytes.Buffer

	gw := gzip.NewWriter(&gzipBuf)
	_, _ = gw.Write(buf.Bytes())
	_ = gw.Close()

	multiMsg := &pb.CMsgMulti{
		SizeUnzipped: proto.Uint32(uint32(buf.Len())),
		MessageBody:  gzipBuf.Bytes(),
	}
	multiPayload, _ := proto.Marshal(multiMsg)

	packet := &protocol.Packet{
		EMsg:    enums.EMsg_Multi,
		Payload: multiPayload,
	}

	sock.handleMulti(packet)

	done := make(chan struct{})

	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout: Handlers were not called. Possible ParsePacket failure or worker stuck.")
	}
}

func TestSocket_ServiceMethodRouting(t *testing.T) {
	sock := NewSocket(DefaultTestConfig())
	defer sock.Close()

	method := "Player.GetOwnedGames#1"
	called := make(chan bool, 1)

	sock.RegisterServiceHandler(method, func(p *protocol.Packet) {
		called <- true
	})

	packet := &protocol.Packet{
		EMsg: enums.EMsg_ServiceMethodResponse,
		Header: &protocol.MsgHdrProtoBuf{
			Proto: &pb.CMsgProtoBufHeader{
				TargetJobName: proto.String(method),
			},
		},
	}

	sock.handleService(packet)

	select {
	case <-called:
	case <-time.After(100 * time.Millisecond):
		t.Error("Service handler was not called")
	}
}

func TestSocket_StateTransitions(t *testing.T) {
	eventBus := bus.New()

	sock := NewSocket(DefaultTestConfig(), WithBus(eventBus))
	defer sock.Close()

	sub := eventBus.Subscribe(StateEvent{})
	defer sub.Unsubscribe()

	sock.setState(StateConnecting)

	select {
	case ev := <-sub.C():
		stateEv := ev.(*StateEvent)
		if stateEv.New != StateConnecting || stateEv.Old != StateDisconnected {
			t.Errorf("Unexpected state transition: %v -> %v", stateEv.Old, stateEv.New)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("StateEvent not published to bus")
	}
}

func TestSocket_ProcessProtobufPacket(t *testing.T) {
	sock := NewSocket(DefaultTestConfig())

	sock.startWorkers()
	defer sock.Close()

	resCh := make(chan *protocol.Packet, 1)

	sock.RegisterMsgHandler(enums.EMsg_ClientLogOnResponse, func(p *protocol.Packet) {
		resCh <- p
	})

	raw := packProto(enums.EMsg_ClientLogOnResponse, 999, []byte("hello_proto"))

	sock.processSingle(bytes.NewReader(raw))

	select {
	case p := <-resCh:
		if !p.IsProto {
			t.Error("Expected IsProto to be true")
		}

		if string(p.Payload) != "hello_proto" {
			t.Errorf("Unexpected payload: %s", string(p.Payload))
		}

		if protoHdr, ok := p.Header.(*protocol.MsgHdrProtoBuf); ok {
			if protoHdr.Proto.GetJobidTarget() != 999 {
				t.Errorf("JobID mismatch: %d", protoHdr.Proto.GetJobidTarget())
			}
		} else {
			t.Errorf("Expected MsgHdrProtoBuf, got %T", p.Header)
		}

	case <-time.After(500 * time.Millisecond):
		t.Fatal("Timeout: Protobuf packet was not processed")
	}
}

func TestSocket_ProcessExtendedPacket(t *testing.T) {
	mockConn := newMockConnection()
	sess := session.New(mockConn)
	sock := NewSocket(DefaultTestConfig(), WithSession(sess))

	sock.startWorkers()
	defer sock.Close()

	resCh := make(chan *protocol.Packet, 1)

	sock.RegisterMsgHandler(enums.EMsg_ClientPersonaState, func(p *protocol.Packet) {
		resCh <- p
	})

	raw := packExtended(enums.EMsg_ClientPersonaState, 777, 123, []byte("legacy_data"))

	sock.processSingle(bytes.NewReader(raw))

	select {
	case p := <-resCh:
		if p.IsProto {
			t.Error("Expected IsProto to be false")
		}

		if sess.SteamID() != 777 {
			t.Errorf("Session SteamID should be updated to 777, got %d", sess.SteamID())
		}

		if sess.SessionID() != 123 {
			t.Errorf("Session SessionID should be 123, got %d", sess.SessionID())
		}

	case <-time.After(500 * time.Millisecond):
		t.Fatal("Timeout: Extended packet was not processed")
	}
}

func TestSocket_ProcessBasicCryptoPacket(t *testing.T) {
	sock := NewSocket(DefaultTestConfig())

	sock.startWorkers()
	defer sock.Close()

	resCh := make(chan *protocol.Packet, 1)

	sock.RegisterMsgHandler(enums.EMsg_ChannelEncryptRequest, func(p *protocol.Packet) {
		resCh <- p
	})

	raw := packBasic(enums.EMsg_ChannelEncryptRequest, 111, 222, []byte("crypto_key_here"))

	sock.processSingle(bytes.NewReader(raw))

	select {
	case p := <-resCh:
		hdr, ok := p.Header.(*protocol.MsgHdr)
		if !ok {
			t.Fatalf("Expected MsgHdr, got %T", p.Header)
		}

		if hdr.TargetJobID != 111 || hdr.SourceJobID != 222 {
			t.Errorf("JobID mismatch in basic header")
		}

		if string(p.Payload) != "crypto_key_here" {
			t.Error("Payload corrupted")
		}

	case <-time.After(500 * time.Millisecond):
		t.Fatal("Timeout: Crypto packet was not processed")
	}
}

func TestSocket_InvalidPacket_UnexpectedEOF(t *testing.T) {
	sock := NewSocket(DefaultTestConfig())
	defer sock.Close()

	invalid := new(bytes.Buffer)
	_ = binary.Write(invalid, binary.LittleEndian, uint32(enums.EMsg_ClientLogon)|0x80000000)

	sock.processSingle(invalid)
}

func TestSocket_StartHeartbeat(t *testing.T) {
	mockConn := newMockConnection()
	cfg := DefaultTestConfig()
	cfg.Dialers = map[string]ConnectionDialer{
		"mock": func(ctx context.Context, nh network.Handler, l log.Logger, s string) (network.Connection, error) {
			return mockConn, nil
		},
	}

	sock := NewSocket(cfg)
	defer sock.Close()

	err := sock.Connect(CMServer{Type: "mock", Endpoint: "localhost"})
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	interval := 20 * time.Millisecond
	sock.StartHeartbeat(interval)

	select {
	case data := <-mockConn.sentMsgs:
		pkt, err := protocol.ParsePacket(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("Failed to parse sent heartbeat: %v", err)
		}

		if pkt.EMsg != enums.EMsg_ClientHeartBeat {
			t.Errorf("Expected EMsg_ClientHeartBeat, got %v", pkt.EMsg)
		}

	case <-time.After(200 * time.Millisecond):
		t.Error("Timeout: Heartbeat was not sent")
	}
}

func TestSocket_InboundHandler(t *testing.T) {
	sock := NewSocket(DefaultTestConfig())
	defer sock.Close()

	sock.startWorkers()

	handler := inboundHandler{sock: sock}

	t.Run("OnNetMessage", func(t *testing.T) {
		called := make(chan bool, 1)

		sock.RegisterMsgHandler(enums.EMsg_ClientLogon, func(p *protocol.Packet) {
			called <- true
		})

		msg := packProto(enums.EMsg_ClientLogon, 0, []byte("hello"))
		handler.OnNetMessage(msg)

		select {
		case <-called:
			// Success
		case <-time.After(100 * time.Millisecond):
			t.Error("OnNetMessage did not result in handler call")
		}
	})

	t.Run("OnNetError", func(t *testing.T) {
		sub := sock.Bus().Subscribe(NetworkErrorEvent{})
		defer sub.Unsubscribe()

		testErr := errors.New("network failure")
		handler.OnNetError(testErr)

		select {
		case ev := <-sub.C():
			netErr := ev.(*NetworkErrorEvent)
			if !errors.Is(netErr.Error, testErr) {
				t.Errorf("Expected error %v, got %v", testErr, netErr.Error)
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("OnNetError did not publish NetworkErrorEvent")
		}
	})

	t.Run("OnNetClose", func(t *testing.T) {
		sock.setState(StateConnected)

		sub := sock.Bus().Subscribe(DisconnectedEvent{})
		defer sub.Unsubscribe()

		handler.OnNetClose()

		if sock.State() != StateDisconnected {
			t.Errorf("Expected state Disconnected after OnNetClose, got %v", sock.State())
		}

		select {
		case ev := <-sub.C():
			discEv := ev.(*DisconnectedEvent)
			if discEv.Error == nil {
				t.Error("Expected error in DisconnectedEvent on unexpected close")
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("OnNetClose did not publish DisconnectedEvent")
		}
	})
}

func TestSocket_ConnectErrors(t *testing.T) {
	cfg := DefaultTestConfig()

	sock := NewSocket(cfg)
	defer sock.Close()

	t.Run("Unsupported Type", func(t *testing.T) {
		err := sock.Connect(CMServer{Type: "unknown"})
		if !errors.Is(err, ErrUnsupportedType) {
			t.Errorf("expected ErrUnsupportedType, got %v", err)
		}
	})

	t.Run("Already Connected", func(t *testing.T) {
		sock.setState(StateConnected)

		err := sock.Connect(CMServer{Type: "tcp"})
		if !errors.Is(err, ErrAlreadyConnected) {
			t.Errorf("expected ErrAlreadyConnected, got %v", err)
		}

		sock.setState(StateDisconnected)
	})

	t.Run("Already Connecting", func(t *testing.T) {
		sock.setState(StateConnecting)

		err := sock.Connect(CMServer{Type: "tcp"})
		if !errors.Is(err, ErrAlreadyConnecting) {
			t.Errorf("expected ErrAlreadyConnecting, got %v", err)
		}

		sock.setState(StateDisconnected)
	})
}

func TestSocket_DecompressionSafety(t *testing.T) {
	sock := NewSocket(DefaultTestConfig())
	defer sock.Close()

	t.Run("Zip Bomb Protection", func(t *testing.T) {
		multi := &pb.CMsgMulti{
			SizeUnzipped: proto.Uint32(101 * 1024 * 1024),
			MessageBody:  []byte("some data"),
		}
		data, _ := proto.Marshal(multi)
		pkt := &protocol.Packet{EMsg: enums.EMsg_Multi, Payload: data}

		sock.handleMulti(pkt)
	})

	t.Run("Corrupt Gzip Data", func(t *testing.T) {
		multi := &pb.CMsgMulti{
			SizeUnzipped: proto.Uint32(100),
			MessageBody:  []byte("not a gzip"),
		}
		data, _ := proto.Marshal(multi)
		pkt := &protocol.Packet{EMsg: enums.EMsg_Multi, Payload: data}
		sock.handleMulti(pkt)
	})
}

func TestSocket_SendSync_Timeout(t *testing.T) {
	mockConn := newMockConnection()
	cfg := DefaultTestConfig()
	cfg.Dialers = map[string]ConnectionDialer{
		"mock": func(ctx context.Context, nh network.Handler, l log.Logger, s string) (network.Connection, error) {
			return mockConn, nil
		},
	}

	sock := NewSocket(cfg)
	defer sock.Close()

	_ = sock.Connect(CMServer{Type: "mock"})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := sock.SendSync(ctx, Raw(enums.EMsg_ClientLogon, []byte{1}))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context deadline exceeded, got %v", err)
	}
}

func TestSocket_ChannelSaturation(t *testing.T) {
	cfg := DefaultTestConfig()
	cfg.EventChanSize = 1

	sock := NewSocket(cfg)
	defer sock.Close()

	sock.msgCh <- &protocol.Packet{EMsg: enums.EMsg_ClientLogon}

	sock.processSingle(bytes.NewReader(packProto(enums.EMsg_ClientLogon, 0, []byte{1})))

	jobPkt := packProto(enums.EMsg_ClientLogon, 123, []byte{1})
	go sock.processSingle(bytes.NewReader(jobPkt))

	time.Sleep(10 * time.Millisecond)
	<-sock.msgCh
}

func TestSocket_ReconnectLoop_MaxAttempts(t *testing.T) {
	cfg := DefaultTestConfig()
	cfg.ReconnectPolicy = ReconnectPolicy{
		MaxAttempts:    2,
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     5 * time.Millisecond,
		BackoffFactor:  1.1,
		ServerSelector: func(s []CMServer) CMServer { return CMServer{Type: "fail"} },
	}
	cfg.Dialers = map[string]ConnectionDialer{
		"fail": func(ctx context.Context, nh network.Handler, l log.Logger, s string) (network.Connection, error) {
			return nil, errors.New("dial failed")
		},
	}

	sock := NewSocket(cfg)
	defer sock.Close()

	sock.reconnectLoop()

	if sock.State() != StateDisconnected {
		t.Error("socket should be disconnected after failed reconnect loop")
	}
}

func TestSocket_PanicRecovery(t *testing.T) {
	sock := NewSocket(DefaultTestConfig())
	defer sock.Close()

	sock.RegisterMsgHandler(enums.EMsg_ClientLogon, func(p *protocol.Packet) {
		panic("test panic")
	})

	sock.routePacket(&protocol.Packet{EMsg: enums.EMsg_ClientLogon})
}

func TestSocket_DestJobFailed(t *testing.T) {
	sock := NewSocket(DefaultTestConfig())
	defer sock.Close()

	resCh := make(chan error, 1)
	id := sock.jobManager.NextID()
	_ = sock.jobManager.Add(id, func(p *protocol.Packet, err error) {
		resCh <- err
	})

	failPkt := &protocol.Packet{
		EMsg:   enums.EMsg_DestJobFailed,
		Header: &protocol.MsgHdr{TargetJobID: id},
	}

	sock.handleJobResponse(failPkt)

	err := <-resCh
	if !errors.Is(err, ErrDestJobFailed) {
		t.Errorf("expected ErrDestJobFailed, got %v", err)
	}
}

func TestSocket_IsFatalNetworkError(t *testing.T) {
	tests := []struct {
		err      error
		expected bool
	}{
		{syscall.ECONNRESET, true},
		{syscall.EPIPE, true},
		{syscall.ETIMEDOUT, true},
		{errors.New("other"), false},
		{nil, false},
	}

	for _, tt := range tests {
		if isFatalNetworkError(tt.err) != tt.expected {
			t.Errorf("isFatalNetworkError(%v) = %v, want %v", tt.err, !tt.expected, tt.expected)
		}
	}
}

func TestSocket_Heartbeat_EdgeCases(t *testing.T) {
	sock := NewSocket(DefaultTestConfig())
	defer sock.Close()

	t.Run("Start without connection", func(t *testing.T) {
		sock.StartHeartbeat(time.Second)

		if sock.heartbeatActive.Load() {
			t.Error("heartbeat should not be active without connection")
		}
	})

	t.Run("Duplicate start", func(t *testing.T) {
		sock.heartbeatActive.Store(true)
		sock.StartHeartbeat(time.Second)
	})
}

func TestRegisterHelper(t *testing.T) {
	sock := NewSocket(DefaultTestConfig())
	defer sock.Close()

	called := false
	Register[*pb.CMsgClientLogonResponse](sock, enums.EMsg_ClientLogOnResponse,
		func() *pb.CMsgClientLogonResponse { return new(pb.CMsgClientLogonResponse) },
		func(m *pb.CMsgClientLogonResponse) { called = true },
	)

	payload, _ := proto.Marshal(&pb.CMsgClientLogonResponse{Eresult: proto.Int32(1)})
	sock.routePacket(&protocol.Packet{
		EMsg:    enums.EMsg_ClientLogOnResponse,
		Payload: payload,
	})

	if !called {
		t.Error("Register helper handler was not called")
	}
}

func TestSocket_SendErrors(t *testing.T) {
	sock := NewSocket(DefaultTestConfig())

	err := sock.Send(context.Background(), nil)
	if !errors.Is(err, ErrClosed) {
		t.Errorf("expected ErrClosed for nil session, got %v", err)
	}

	mockConn := newMockConnection()
	sock.setSession(session.New(mockConn))
	sock.setState(StateConnected)

	builderErr := errors.New("build fail")

	err = sock.Send(context.Background(), func(sess Session, buf *bytes.Buffer, id uint64, t string) error {
		return builderErr
	})
	if !errors.Is(err, builderErr) {
		t.Errorf("expected builder error, got %v", err)
	}
}

type mockConn struct {
	sentData []byte
	closed   bool
	key      []byte
}

func (m *mockConn) Send(ctx context.Context, data []byte) error {
	m.sentData = append([]byte(nil), data...)
	return nil
}

func (m *mockConn) Name() string { return "MOCK" }

func (m *mockConn) Close() error {
	m.closed = true
	return nil
}

func (m *mockConn) ID() int64 { return 1 }

func (m *mockConn) SetEncryptionKey(key []byte) {
	m.key = append([]byte(nil), key...)
}

func TestLogged_Decorator(t *testing.T) {
	conn := &mockConn{}
	base := session.New(conn)
	logged := newLoggedSession(base, log.Discard)

	logged.SetSteamID(100)

	if logged.SteamID() != 100 {
		t.Error("delegation failed")
	}

	if err := logged.Send(context.Background(), []byte("test")); err != nil {
		t.Fatal(err)
	}

	if string(conn.sentData) != "test" {
		t.Error("Send delegation failed")
	}

	if !logged.SetEncryptionKey([]byte("key")) {
		t.Error("SetEncryptionKey delegation failed")
	}

	_ = logged.Close()

	if !conn.closed {
		t.Error("Close delegation failed")
	}
}
