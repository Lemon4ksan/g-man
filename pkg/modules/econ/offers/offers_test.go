// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package offers

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
	pb "github.com/lemon4ksan/g-man/pkg/steam/protocol/protobuf"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
	"google.golang.org/protobuf/proto"
)

type mockLegacyRequester struct {
	mu         sync.Mutex
	calls      map[protocol.EMsg]int
	lastReqMsg proto.Message
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

func TestManager_InitAndClose(t *testing.T) {
	m := New()
	initCtx := newMockInitContext()

	if m.Name() != ModuleName {
		t.Errorf("expected module name %s, got %s", ModuleName, m.Name())
	}

	err := m.Init(initCtx)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	expectedHandlers := []protocol.EMsg{
		protocol.EMsg_EconTrading_InitiateTradeProposed,
		protocol.EMsg_EconTrading_InitiateTradeResult,
		protocol.EMsg_EconTrading_StartSession,
	}

	for _, emsg := range expectedHandlers {
		if _, ok := initCtx.packetHandlers[emsg]; !ok {
			t.Errorf("expected handler for EMsg %v to be registered", emsg)
		}
	}

	err = m.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	for _, emsg := range expectedHandlers {
		if _, ok := initCtx.packetHandlers[emsg]; ok {
			t.Errorf("expected handler for EMsg %v to be unregistered after Close", emsg)
		}
	}
}

func TestManager_Invite(t *testing.T) {
	m := New()
	initCtx := newMockInitContext()
	_ = m.Init(initCtx)

	ctx := context.Background()
	otherID := uint64(123456789)

	err := m.Invite(ctx, otherID)
	if err != nil {
		t.Fatalf("Invite failed: %v", err)
	}

	if initCtx.proto.getCallCount(protocol.EMsg_EconTrading_InitiateTradeRequest) != 1 {
		t.Error("expected 1 call to InitiateTradeRequest")
	}

	req := initCtx.proto.lastReqMsg.(*pb.CMsgTrading_InitiateTradeRequest)
	if req.GetOtherSteamid() != otherID {
		t.Errorf("expected target ID %d, got %d", otherID, req.GetOtherSteamid())
	}
}

func TestManager_CancelInvitation(t *testing.T) {
	m := New()
	initCtx := newMockInitContext()
	_ = m.Init(initCtx)

	ctx := context.Background()
	otherID := uint64(987654321)

	err := m.CancelInvitation(ctx, otherID)
	if err != nil {
		t.Fatalf("CancelInvitation failed: %v", err)
	}

	if initCtx.proto.getCallCount(protocol.EMsg_EconTrading_CancelTradeRequest) != 1 {
		t.Error("expected 1 call to CancelTradeRequest")
	}

	req := initCtx.proto.lastReqMsg.(*pb.CMsgTrading_CancelTradeRequest)
	if req.GetOtherSteamid() != otherID {
		t.Errorf("expected target ID %d, got %d", otherID, req.GetOtherSteamid())
	}
}

func TestManager_RespondToInvite(t *testing.T) {
	m := New()
	initCtx := newMockInitContext()
	_ = m.Init(initCtx)

	ctx := context.Background()
	tradeID := uint32(555)

	err := m.RespondToInvite(ctx, tradeID, true)
	if err != nil {
		t.Fatalf("RespondToInvite (accept) failed: %v", err)
	}

	req := initCtx.proto.lastReqMsg.(*pb.CMsgTrading_InitiateTradeResponse)
	if req.GetTradeRequestId() != tradeID {
		t.Errorf("expected trade ID %d, got %d", tradeID, req.GetTradeRequestId())
	}
	if req.GetResponse() != uint32(protocol.EEconTradeResponse_Accepted) {
		t.Errorf("expected response 'Accepted' (%d), got %d", protocol.EEconTradeResponse_Accepted, req.GetResponse())
	}

	err = m.RespondToInvite(ctx, tradeID, false)
	if err != nil {
		t.Fatalf("RespondToInvite (decline) failed: %v", err)
	}

	req = initCtx.proto.lastReqMsg.(*pb.CMsgTrading_InitiateTradeResponse)
	if req.GetResponse() != uint32(protocol.EEconTradeResponse_Declined) {
		t.Errorf("expected response 'Declined' (%d), got %d", protocol.EEconTradeResponse_Declined, req.GetResponse())
	}
}

func TestManager_HandleTradeRequest(t *testing.T) {
	m := New()
	initCtx := newMockInitContext()
	_ = m.Init(initCtx)

	sub := initCtx.eventBus.Subscribe(&TradeProposedEvent{})
	handler := initCtx.packetHandlers[protocol.EMsg_EconTrading_InitiateTradeProposed]

	msg := &pb.CMsgTrading_InitiateTradeRequest{
		OtherSteamid:   proto.Uint64(111),
		TradeRequestId: proto.Uint32(222),
	}
	payload, _ := proto.Marshal(msg)
	packet := &protocol.Packet{Payload: payload}

	handler(packet)

	select {
	case ev := <-sub.C():
		tradeEv := ev.(*TradeProposedEvent)
		if tradeEv.OtherSteamID != 111 || tradeEv.TradeID != 222 {
			t.Errorf("invalid TradeProposedEvent data: %+v", tradeEv)
		}

		if tradeEv.Respond == nil {
			t.Fatal("expected Respond function to be assigned")
		}

		tradeEv.Respond(true)
		
		if initCtx.proto.getCallCount(protocol.EMsg_EconTrading_InitiateTradeResponse) != 1 {
			t.Error("expected Respond() to call InitiateTradeResponse")
		}
		
		req := initCtx.proto.lastReqMsg.(*pb.CMsgTrading_InitiateTradeResponse)
		if req.GetResponse() != uint32(protocol.EEconTradeResponse_Accepted) {
			t.Error("expected Respond(true) to send Accepted response")
		}

	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for TradeProposedEvent")
	}
}

func TestManager_HandleTradeResult(t *testing.T) {
	m := New()
	initCtx := newMockInitContext()
	_ = m.Init(initCtx)

	sub := initCtx.eventBus.Subscribe(&TradeResultEvent{})
	handler := initCtx.packetHandlers[protocol.EMsg_EconTrading_InitiateTradeResult]

	msg := &pb.CMsgTrading_InitiateTradeResponse{
		OtherSteamid:            proto.Uint64(333),
		Response:                proto.Uint32(uint32(protocol.EEconTradeResponse_TooSoon)),
		SteamguardRequiredDays:  proto.Uint32(15),
		NewDeviceCooldownDays:   proto.Uint32(7),
	}
	payload, _ := proto.Marshal(msg)
	packet := &protocol.Packet{Payload: payload}

	handler(packet)

	select {
	case ev := <-sub.C():
		resultEv := ev.(*TradeResultEvent)
		if resultEv.OtherSteamID != 333 {
			t.Errorf("expected other ID 333, got %d", resultEv.OtherSteamID)
		}
		if resultEv.Response != protocol.EEconTradeResponse_TooSoon {
			t.Errorf("expected response TooSoon, got %s", resultEv.Response.String())
		}
		if resultEv.SteamGuardRequiredDays != 15 || resultEv.NewDeviceCooldownDays != 7 {
			t.Errorf("invalid cooldown info: SG %d, Device %d", resultEv.SteamGuardRequiredDays, resultEv.NewDeviceCooldownDays)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for TradeResultEvent")
	}
}

func TestManager_HandleTradeStarted(t *testing.T) {
	m := New()
	initCtx := newMockInitContext()
	_ = m.Init(initCtx)

	sub := initCtx.eventBus.Subscribe(&TradeSessionStartedEvent{})
	handler := initCtx.packetHandlers[protocol.EMsg_EconTrading_StartSession]

	msg := &pb.CMsgTrading_StartSession{
		OtherSteamid: proto.Uint64(999),
	}
	payload, _ := proto.Marshal(msg)
	packet := &protocol.Packet{Payload: payload}

	handler(packet)

	select {
	case ev := <-sub.C():
		startEv := ev.(*TradeSessionStartedEvent)
		if startEv.OtherSteamID != 999 {
			t.Errorf("expected OtherSteamID 999, got %d", startEv.OtherSteamID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for TradeSessionStartedEvent")
	}
}