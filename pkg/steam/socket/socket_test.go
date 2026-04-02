// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socket

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"
	"github.com/lemon4ksan/g-man/pkg/steam/network"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/steam/protobuf"
)

type mockConnection struct {
	network.BaseConnection
	sendFunc  func(ctx context.Context, data []byte) error
	closeFunc func() error
}

func newMockConnection() *mockConnection {
	return &mockConnection{
		BaseConnection: network.NewBaseConnection("mock"),
		sendFunc:       func(ctx context.Context, data []byte) error { return nil },
		closeFunc:      func() error { return nil },
	}
}

func (m *mockConnection) Name() string                                { return "MOCK" }
func (m *mockConnection) Send(ctx context.Context, data []byte) error { return m.sendFunc(ctx, data) }
func (m *mockConnection) Close() error                                { return m.closeFunc() }

func packProto(eMsg protocol.EMsg, jobId uint64, payload []byte) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint32(eMsg)|0x80000000)
	hdr := &pb.CMsgProtoBufHeader{JobidTarget: proto.Uint64(jobId)}
	hdrBytes, _ := proto.Marshal(hdr)
	binary.Write(buf, binary.LittleEndian, uint32(len(hdrBytes)))
	buf.Write(hdrBytes)
	buf.Write(payload)
	return buf.Bytes()
}

func packExtended(eMsg protocol.EMsg, steamID uint64, sessionID int32, payload []byte) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint32(eMsg))           // EMsg
	buf.WriteByte(36)                                              // HeaderSize
	binary.Write(buf, binary.LittleEndian, uint16(2))              // HeaderVer
	binary.Write(buf, binary.LittleEndian, uint64(math.MaxUint64)) // TargetJobID
	binary.Write(buf, binary.LittleEndian, uint64(math.MaxUint64)) // SourceJobID
	buf.WriteByte(239)                                             // Canary
	binary.Write(buf, binary.LittleEndian, steamID)                // SteamID
	binary.Write(buf, binary.LittleEndian, sessionID)              // SessionID
	buf.Write(payload)
	return buf.Bytes()
}

func packBasic(eMsg protocol.EMsg, targetJob, sourceJob uint64, payload []byte) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint32(eMsg))
	binary.Write(buf, binary.LittleEndian, targetJob)
	binary.Write(buf, binary.LittleEndian, sourceJob)
	buf.Write(payload)
	return buf.Bytes()
}

func TestSocket_Initialization(t *testing.T) {
	sock := NewSocket(DefaultConfig())
	defer sock.Close()

	if sock.State() != StateDisconnected {
		t.Errorf("Expected initial state to be Disconnected, got %v", sock.State())
	}
	if sock.Bus() == nil {
		t.Error("Expected event bus to be initialized")
	}
}

func TestSocket_HandlersManagement(t *testing.T) {
	sock := NewSocket(DefaultConfig())
	defer sock.Close()

	var called atomic.Bool
	sock.RegisterMsgHandler(protocol.EMsg_ClientLogon, func(p *protocol.Packet) {
		called.Store(true)
	})

	sock.handlersMu.RLock()
	_, exists := sock.handlers[protocol.EMsg_ClientLogon]
	sock.handlersMu.RUnlock()

	if !exists {
		t.Error("Handler was not registered")
	}

	sock.ClearHandlers()

	sock.handlersMu.RLock()
	_, exists = sock.handlers[protocol.EMsg_ClientLogon]
	sock.handlersMu.RUnlock()

	if exists {
		t.Error("Handler should be removed after ClearHandlers")
	}
}

func TestSocket_ConnectAndDisconnect(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Dialers = map[string]ConnectionDialer{
		"mock": func(nh network.Handler, l log.Logger, s string) (network.Connection, error) {
			return newMockConnection(), nil
		},
	}

	eventBus := bus.NewBus()
	sock := NewSocket(cfg, WithBus(eventBus))
	defer sock.Close()

	sub := eventBus.Subscribe(ConnectedEvent{}, DisconnectedEvent{})
	defer sub.Unsubscribe()

	err := sock.Connect(t.Context(), CMServer{Type: "mock", Endpoint: "localhost:1234"})
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
	sock := NewSocket(DefaultConfig())
	defer sock.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sock.startWorkers(ctx, 1)

	wg := sync.WaitGroup{}
	wg.Add(1)

	sock.RegisterMsgHandler(protocol.EMsg_ClientLogOnResponse, func(p *protocol.Packet) {
		if string(p.Payload) != "test_payload" {
			t.Errorf("Unexpected payload: %s", string(p.Payload))
		}
		wg.Done()
	})

	packet := &protocol.Packet{
		EMsg:    protocol.EMsg_ClientLogOnResponse,
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
	cfg := DefaultConfig()

	conn := newMockConnection()
	cfg.Dialers = map[string]ConnectionDialer{
		"mock": func(nh network.Handler, l log.Logger, s string) (network.Connection, error) {
			return conn, nil
		},
	}

	sock := NewSocket(cfg)
	defer sock.Close()

	if err := sock.Connect(t.Context(), CMServer{Type: "mock"}); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	wg := sync.WaitGroup{}
	wg.Add(1)

	var receivedErr error
	var receivedResp *protocol.Packet
	var capturedJobID uint64

	builder := func(sess Session, buf *bytes.Buffer, sourceJobID uint64, token string) error {
		capturedJobID = sourceJobID
		return Raw(protocol.EMsg_ClientGamesPlayed, []byte("data"))(sess, buf, sourceJobID, token)
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
		EMsg: protocol.EMsg_ClientGamesPlayed,
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
	sess := NewBaseSession(mockConn)

	sock := NewSocket(DefaultConfig(), WithSession(sess))
	defer sock.Close()

	packet := &protocol.Packet{
		EMsg: protocol.EMsg_ClientLogOnResponse,
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
	sock := NewSocket(DefaultConfig())
	sock.startWorkers(t.Context(), 1)
	defer sock.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	sock.RegisterMsgHandler(protocol.EMsg_ClientLogOnResponse, func(p *protocol.Packet) {
		if string(p.Payload) != "sub_payload_1" {
			t.Errorf("Wrong payload 1: %s", string(p.Payload))
		}
		wg.Done()
	})
	sock.RegisterMsgHandler(protocol.EMsg_ClientPersonaState, func(p *protocol.Packet) {
		if string(p.Payload) != "sub_payload_2" {
			t.Errorf("Wrong payload 2: %s", string(p.Payload))
		}
		wg.Done()
	})

	subPkt1 := packProto(protocol.EMsg_ClientLogOnResponse, 0, []byte("sub_payload_1"))
	subPkt2 := packProto(protocol.EMsg_ClientPersonaState, 0, []byte("sub_payload_2"))

	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint32(len(subPkt1)))
	buf.Write(subPkt1)
	binary.Write(buf, binary.LittleEndian, uint32(len(subPkt2)))
	buf.Write(subPkt2)

	var gzipBuf bytes.Buffer
	gw := gzip.NewWriter(&gzipBuf)
	gw.Write(buf.Bytes())
	gw.Close()

	multiMsg := &pb.CMsgMulti{
		SizeUnzipped: proto.Uint32(uint32(buf.Len())),
		MessageBody:  gzipBuf.Bytes(),
	}
	multiPayload, _ := proto.Marshal(multiMsg)

	packet := &protocol.Packet{
		EMsg:    protocol.EMsg_Multi,
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
	sock := NewSocket(DefaultConfig())
	defer sock.Close()

	method := "Player.GetOwnedGames#1"
	called := make(chan bool, 1)

	sock.RegisterServiceHandler(method, func(p *protocol.Packet) {
		called <- true
	})

	packet := &protocol.Packet{
		EMsg: protocol.EMsg_ServiceMethodResponse,
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
	eventBus := bus.NewBus()
	sock := NewSocket(DefaultConfig(), WithBus(eventBus))
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
	sock := NewSocket(DefaultConfig())
	sock.startWorkers(t.Context(), 1)
	defer sock.Close()

	resCh := make(chan *protocol.Packet, 1)
	sock.RegisterMsgHandler(protocol.EMsg_ClientLogOnResponse, func(p *protocol.Packet) {
		resCh <- p
	})

	raw := packProto(protocol.EMsg_ClientLogOnResponse, 999, []byte("hello_proto"))

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
	sess := NewBaseSession(mockConn)
	sock := NewSocket(DefaultConfig(), WithSession(sess))
	sock.startWorkers(t.Context(), 1)
	defer sock.Close()

	resCh := make(chan *protocol.Packet, 1)
	sock.RegisterMsgHandler(protocol.EMsg_ClientPersonaState, func(p *protocol.Packet) {
		resCh <- p
	})

	raw := packExtended(protocol.EMsg_ClientPersonaState, 777, 123, []byte("legacy_data"))

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
	sock := NewSocket(DefaultConfig())
	sock.startWorkers(t.Context(), 1)
	defer sock.Close()

	resCh := make(chan *protocol.Packet, 1)
	sock.RegisterMsgHandler(protocol.EMsg_ChannelEncryptRequest, func(p *protocol.Packet) {
		resCh <- p
	})

	raw := packBasic(protocol.EMsg_ChannelEncryptRequest, 111, 222, []byte("crypto_key_here"))

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
	sock := NewSocket(DefaultConfig())
	defer sock.Close()

	invalid := new(bytes.Buffer)
	binary.Write(invalid, binary.LittleEndian, uint32(protocol.EMsg_ClientLogon)|0x80000000)

	sock.processSingle(invalid)
}
