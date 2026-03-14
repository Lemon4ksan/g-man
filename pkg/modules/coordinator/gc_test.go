// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package coordinator

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/gc"
	pb "github.com/lemon4ksan/g-man/pkg/steam/protocol/protobuf"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
	"google.golang.org/protobuf/proto"
)

type mockLegacyRequester struct {
	mu          sync.Mutex
	calls       map[protocol.EMsg]int
	lastReqMsg  proto.Message
	responseErr error
}

func newMockRequester() *mockLegacyRequester {
	return &mockLegacyRequester{
		calls: make(map[protocol.EMsg]int),
	}
}

func (m *mockLegacyRequester) CallLegacy(ctx context.Context, eMsg protocol.EMsg, reqMsg, respMsg proto.Message, mods ...api.RequestModifier) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls[eMsg]++
	m.lastReqMsg = reqMsg

	if m.responseErr != nil {
		return m.responseErr
	}
	return nil
}

func (m *mockLegacyRequester) Do(*tr.Request) (*tr.Response, error) {
	panic("Not implemented")
}

func (m *mockLegacyRequester) getCallCount(emsg protocol.EMsg) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls[emsg]
}

type mockInitContext struct {
	eventBus       *bus.Bus
	proto          *mockLegacyRequester
	packetHandlers map[protocol.EMsg]socket.Handler
}

func newMockInitContext() *mockInitContext {
	return &mockInitContext{
		eventBus:       bus.NewBus(),
		proto:          newMockRequester(),
		packetHandlers: make(map[protocol.EMsg]socket.Handler),
	}
}

func (m *mockInitContext) Bus() *bus.Bus                 { return m.eventBus }
func (m *mockInitContext) Proto() api.LegacyRequester    { return m.proto }
func (m *mockInitContext) Unified() api.UnifiedRequester { return nil }
func (m *mockInitContext) Logger() log.Logger            { return log.Discard }
func (m *mockInitContext) WebAPI() api.WebAPIRequester   { return nil }
func (m *mockInitContext) Config() steam.Config          { return steam.Config{} }

func (m *mockInitContext) RegisterPacketHandler(e protocol.EMsg, h socket.Handler) {
	m.packetHandlers[e] = h
}
func (m *mockInitContext) UnregisterPacketHandler(e protocol.EMsg) {
	delete(m.packetHandlers, e)
}
func (m *mockInitContext) RegisterServiceHandler(method string, handler socket.Handler) {}
func (m *mockInitContext) UnregisterServiceHandler(method string)                       {}
func (m *mockInitContext) GetModule(name string) steam.Module                           { return nil }

type dummyProto struct {
	pb.CMsgGCClient
}

func TestCoordinator_InitAndClose(t *testing.T) {
	c := New()
	initCtx := newMockInitContext()

	if c.Name() != ModuleName {
		t.Errorf("expected %s, got %s", ModuleName, c.Name())
	}

	err := c.Init(initCtx)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if _, ok := initCtx.packetHandlers[protocol.EMsg_ClientFromGC]; !ok {
		t.Error("expected EMsg_ClientFromGC handler to be registered")
	}

	err = c.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if _, ok := initCtx.packetHandlers[protocol.EMsg_ClientFromGC]; ok {
		t.Error("expected handler to be unregistered after Close")
	}
}

func TestCoordinator_SendRaw(t *testing.T) {
	c := New()
	initCtx := newMockInitContext()
	_ = c.Init(initCtx)

	ctx := context.Background()
	appID := uint32(440)
	msgType := uint32(1005)
	payload := []byte{0x01, 0x02, 0x03}

	err := c.SendRaw(ctx, appID, msgType, payload)
	if err != nil {
		t.Fatalf("SendRaw failed: %v", err)
	}

	if initCtx.proto.getCallCount(protocol.EMsg_ClientToGC) != 1 {
		t.Error("expected 1 call to ClientToGC")
	}

	req := initCtx.proto.lastReqMsg.(*pb.CMsgGCClient)
	if req.GetAppid() != appID {
		t.Errorf("expected AppID %d, got %d", appID, req.GetAppid())
	}

	if req.GetMsgtype() != (msgType | gc.ProtoMask) {
		t.Errorf("expected MsgType %d, got %d", msgType|gc.ProtoMask, req.GetMsgtype())
	}

	if c.jobManager.Count() != 0 {
		t.Error("expected 0 jobs in jobManager for SendRaw")
	}
}

func TestCoordinator_Call(t *testing.T) {
	c := New()
	initCtx := newMockInitContext()
	_ = c.Init(initCtx)

	ctx := context.Background()
	appID := uint32(730)
	msgType := uint32(4004)

	var receivedPacket *gc.Packet
	var receivedErr error
	var wg sync.WaitGroup
	wg.Add(1)

	cb := func(p *gc.Packet, err error) {
		receivedPacket = p
		receivedErr = err
		wg.Done()
	}

	msg := &dummyProto{}
	err := c.Call(ctx, appID, msgType, msg, cb)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if c.jobManager.Count() != 1 {
		t.Errorf("expected 1 job in jobManager, got %d", c.jobManager.Count())
	}

	jobID := uint64(1)

	replyPacket := &gc.Packet{AppID: appID, MsgType: 4005, TargetJobID: jobID}
	c.jobManager.Resolve(jobID, replyPacket, nil)

	wg.Wait()

	if receivedErr != nil {
		t.Errorf("unexpected error in callback: %v", receivedErr)
	}
	if receivedPacket == nil || receivedPacket.TargetJobID != jobID {
		t.Errorf("invalid packet received in callback")
	}

	if c.jobManager.Count() != 0 {
		t.Errorf("expected job to be removed, count is %d", c.jobManager.Count())
	}
}

func TestCoordinator_HandleClientFromGC_BusRouting(t *testing.T) {
	c := New()
	initCtx := newMockInitContext()
	_ = c.Init(initCtx)

	sub := initCtx.eventBus.Subscribe(&GCMessageEvent{})
	handler := initCtx.packetHandlers[protocol.EMsg_ClientFromGC]

	innerPacket := &gc.Packet{
		AppID:       440,
		MsgType:     2002,
		TargetJobID: protocol.NoJob,
		Payload:     []byte{0xFF},
	}

	gcData, _ := innerPacket.Serialize()
	if gcData == nil {
		gcData = []byte{0x00}
	}

	wrapper := &pb.CMsgGCClient{
		Appid:   proto.Uint32(440),
		Msgtype: proto.Uint32(2002),
		Payload: gcData,
	}

	wrapperBytes, _ := proto.Marshal(wrapper)
	packet := &protocol.Packet{Payload: wrapperBytes}

	handler(packet)

	select {
	case ev := <-sub.C():
		gcEv := ev.(*GCMessageEvent)
		if gcEv.Packet == nil {
			t.Error("expected non-nil inner GC packet in event")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for GCMessageEvent on bus")
	}
}

func TestCoordinator_HandleClientFromGC_JobRouting(t *testing.T) {
	c := New()
	initCtx := newMockInitContext()
	_ = c.Init(initCtx)

	handler := initCtx.packetHandlers[protocol.EMsg_ClientFromGC]

	var callbackHit bool
	var wg sync.WaitGroup
	wg.Add(1)

	jobID := uint64(123)
	_ = c.jobManager.Add(jobID, func(p *gc.Packet, err error) {
		callbackHit = true
		wg.Done()
	})

	innerPacket := &gc.Packet{
		AppID:       440,
		MsgType:     2002,
		TargetJobID: jobID,
	}

	gcData, _ := innerPacket.Serialize()
	wrapper := &pb.CMsgGCClient{
		Appid:   proto.Uint32(440),
		Msgtype: proto.Uint32(2002),
		Payload: gcData,
	}
	wrapperBytes, _ := proto.Marshal(wrapper)

	sub := initCtx.eventBus.Subscribe(&GCMessageEvent{})
	handler(&protocol.Packet{Payload: wrapperBytes})
	wg.Wait()

	if !callbackHit {
		t.Error("expected job callback to be executed")
	}

	select {
	case <-sub.C():
		t.Error("event should NOT be published to bus if job handled it")
	default:
	}
}
