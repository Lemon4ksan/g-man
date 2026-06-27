// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package live

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lemon4ksan/miyako/bus"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/test/mock"
)

const (
	FriendSteamID = 123456789
	TradeID       = 555
)

func setupOffers(t *testing.T) (*Manager, *mock.InitContext) {
	t.Helper()

	m := New()
	ictx := mock.NewInitContext()

	if err := m.Init(ictx); err != nil {
		t.Fatalf("failed to init offers manager: %v", err)
	}

	t.Cleanup(func() {
		_ = m.Close()
	})

	return m, ictx
}

func awaitEvent[T any](t *testing.T, ch <-chan bus.Event) T {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	t.Cleanup(cancel)

	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("event channel closed")
		}

		val, ok := ev.(T)
		if !ok {
			t.Fatalf("expected event type %T, got %T", val, ev)
		}

		return val

	case <-ctx.Done():
		t.Fatal("timeout waiting for event")

		var zero T

		return zero
	}
}

func TestFrom_NilClient_ReturnsNil(t *testing.T) {
	t.Parallel()

	assert.Nil(t, From(nil))
}

func TestWithModule_ValidOption_ReturnsNonNil(t *testing.T) {
	t.Parallel()

	opt := WithModule()
	assert.NotNil(t, opt)
}

func TestInit_ValidContext_RegistersHandlers(t *testing.T) {
	t.Parallel()

	m := New()
	ictx := mock.NewInitContext()

	if m.Name() != ModuleName {
		t.Errorf("expected module name %q, got %q", ModuleName, m.Name())
	}

	if err := m.Init(ictx); err != nil {
		t.Fatalf("failed to init offers manager: %v", err)
	}

	t.Cleanup(func() {
		_ = m.Close()
	})

	expectedEMsgs := []enums.EMsg{
		enums.EMsg_EconTrading_InitiateTradeProposed,
		enums.EMsg_EconTrading_InitiateTradeResult,
		enums.EMsg_EconTrading_StartSession,
	}

	for _, emsg := range expectedEMsgs {
		ictx.AssertPacketHandlerRegistered(t, emsg)
	}
}

func TestClose_ActiveHandlers_UnregistersHandlers(t *testing.T) {
	t.Parallel()

	m := New()
	ictx := mock.NewInitContext()

	if err := m.Init(ictx); err != nil {
		t.Fatalf("failed to init offers manager: %v", err)
	}

	if err := m.Close(); err != nil {
		t.Fatalf("failed to close offers manager: %v", err)
	}

	expectedEMsgs := []enums.EMsg{
		enums.EMsg_EconTrading_InitiateTradeProposed,
		enums.EMsg_EconTrading_InitiateTradeResult,
		enums.EMsg_EconTrading_StartSession,
	}

	for _, emsg := range expectedEMsgs {
		ictx.AssertPacketHandlerUnregistered(t, emsg)
	}
}

func TestInvite_ValidSteamID_SendsRequest(t *testing.T) {
	t.Parallel()

	m, ictx := setupOffers(t)

	if err := m.Invite(t.Context(), FriendSteamID); err != nil {
		t.Fatalf("Invite failed: %v", err)
	}

	req := &pb.CMsgTrading_InitiateTradeRequest{}
	ictx.MockService().GetLastCall(req)

	if req.GetOtherSteamid() != FriendSteamID {
		t.Errorf("expected target steam ID %d, got %d", FriendSteamID, req.GetOtherSteamid())
	}
}

func TestInvite_TransportError_ReturnsError(t *testing.T) {
	t.Parallel()

	m, ictx := setupOffers(t)
	ictx.MockService().ResponseErrs[enums.EMsg_EconTrading_InitiateTradeRequest.String()] = errors.New("timeout")

	err := m.Invite(t.Context(), FriendSteamID)
	assert.ErrorContains(t, err, "failed to send invitation: timeout")
}

func TestCancelInvitation_ValidSteamID_SendsRequest(t *testing.T) {
	t.Parallel()

	m, ictx := setupOffers(t)

	if err := m.CancelInvitation(t.Context(), FriendSteamID); err != nil {
		t.Fatalf("CancelInvitation failed: %v", err)
	}

	req := &pb.CMsgTrading_CancelTradeRequest{}
	ictx.MockService().GetLastCall(req)

	if req.GetOtherSteamid() != FriendSteamID {
		t.Errorf("expected target steam ID %d, got %d", FriendSteamID, req.GetOtherSteamid())
	}
}

func TestCancelInvitation_TransportError_ReturnsError(t *testing.T) {
	t.Parallel()

	m, ictx := setupOffers(t)
	ictx.MockService().ResponseErrs[enums.EMsg_EconTrading_CancelTradeRequest.String()] = errors.New("timeout")

	err := m.CancelInvitation(t.Context(), FriendSteamID)
	assert.ErrorContains(t, err, "timeout")
}

func TestRespondToInvite_VariousResponses_SendsExpectedResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		accept   bool
		expected enums.EEconTradeResponse
	}{
		{
			name:     "accept",
			accept:   true,
			expected: enums.EEconTradeResponse_Accepted,
		},
		{
			name:     "decline",
			accept:   false,
			expected: enums.EEconTradeResponse_Declined,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m, ictx := setupOffers(t)

			err := m.RespondToInvite(t.Context(), TradeID, tt.accept)
			if err != nil {
				t.Fatalf("RespondToInvite failed: %v", err)
			}

			req := &pb.CMsgTrading_InitiateTradeResponse{}
			ictx.MockService().GetLastCall(req)

			if req.GetTradeRequestId() != TradeID {
				t.Errorf("expected trade ID %d, got %d", TradeID, req.GetTradeRequestId())
			}

			if req.GetResponse() != uint32(tt.expected) {
				t.Errorf("expected response %v, got %v", tt.expected, req.GetResponse())
			}
		})
	}
}

func TestRespondToInvite_TransportError_ReturnsError(t *testing.T) {
	t.Parallel()

	m, ictx := setupOffers(t)
	ictx.MockService().ResponseErrs[enums.EMsg_EconTrading_InitiateTradeResponse.String()] = errors.New("timeout")

	err := m.RespondToInvite(t.Context(), TradeID, true)
	assert.ErrorContains(t, err, "timeout")
}

func TestHandleTradeProposed_ValidPacket_PublishesEventAndResponds(t *testing.T) {
	t.Parallel()

	_, ictx := setupOffers(t)
	sub := ictx.Bus().Subscribe(&TradeProposedEvent{})

	ictx.EmitPacket(t, enums.EMsg_EconTrading_InitiateTradeProposed, &pb.CMsgTrading_InitiateTradeRequest{
		OtherSteamid:   proto.Uint64(FriendSteamID),
		TradeRequestId: proto.Uint32(TradeID),
	})

	tradeEv := awaitEvent[*TradeProposedEvent](t, sub.C())

	if tradeEv.OtherSteamID != FriendSteamID {
		t.Errorf("expected other steam ID %d, got %d", FriendSteamID, tradeEv.OtherSteamID)
	}

	if tradeEv.TradeID != TradeID {
		t.Errorf("expected trade ID %d, got %d", TradeID, tradeEv.TradeID)
	}

	if tradeEv.Respond == nil {
		t.Fatal("Respond callback in event is nil")
	}

	tradeEv.Respond(true)

	req := &pb.CMsgTrading_InitiateTradeResponse{}
	ictx.MockService().GetLastCall(req)

	if req.GetResponse() != uint32(enums.EEconTradeResponse_Accepted) {
		t.Errorf("expected response %v, got %v", enums.EEconTradeResponse_Accepted, req.GetResponse())
	}
}

func TestHandleTradeProposed_InvalidProto_HandlesGracefully(t *testing.T) {
	t.Parallel()

	m, ictx := setupOffers(t)

	sub := ictx.Bus().Subscribe(&TradeProposedEvent{})
	defer sub.Unsubscribe()

	assert.NotPanics(t, func() {
		m.handleTradeRequest(&protocol.Packet{
			Payload: []byte{0xFF, 0xFF},
		})
	})

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	t.Cleanup(cancel)

	select {
	case ev := <-sub.C():
		t.Fatalf("unexpected event published: %v", ev)
	case <-ctx.Done():
		// Success, no event published
	}
}

func TestHandleTradeResult_ValidPacket_PublishesEvent(t *testing.T) {
	t.Parallel()

	_, ictx := setupOffers(t)
	sub := ictx.Bus().Subscribe(&TradeResultEvent{})

	ictx.EmitPacket(t, enums.EMsg_EconTrading_InitiateTradeResult, &pb.CMsgTrading_InitiateTradeResponse{
		OtherSteamid:           proto.Uint64(FriendSteamID),
		Response:               proto.Uint32(uint32(enums.EEconTradeResponse_TooSoon)),
		SteamguardRequiredDays: proto.Uint32(15),
		NewDeviceCooldownDays:  proto.Uint32(7),
	})

	res := awaitEvent[*TradeResultEvent](t, sub.C())

	if res.OtherSteamID != FriendSteamID {
		t.Errorf("expected other steam ID %d, got %d", FriendSteamID, res.OtherSteamID)
	}

	if res.Response != enums.EEconTradeResponse_TooSoon {
		t.Errorf("expected response %v, got %v", enums.EEconTradeResponse_TooSoon, res.Response)
	}

	if res.SteamGuardRequiredDays != 15 {
		t.Errorf("expected Steam Guard required days 15, got %d", res.SteamGuardRequiredDays)
	}

	if res.NewDeviceCooldownDays != 7 {
		t.Errorf("expected new device cooldown days 7, got %d", res.NewDeviceCooldownDays)
	}
}

func TestHandleTradeResult_InvalidProto_HandlesGracefully(t *testing.T) {
	t.Parallel()

	m, ictx := setupOffers(t)

	sub := ictx.Bus().Subscribe(&TradeResultEvent{})
	defer sub.Unsubscribe()

	assert.NotPanics(t, func() {
		m.handleTradeResult(&protocol.Packet{
			Payload: []byte{0xFF, 0xFF},
		})
	})

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	t.Cleanup(cancel)

	select {
	case ev := <-sub.C():
		t.Fatalf("unexpected event published: %v", ev)
	case <-ctx.Done():
		// Success, no event published
	}
}

func TestHandleTradeStarted_ValidPacket_PublishesEvent(t *testing.T) {
	t.Parallel()

	_, ictx := setupOffers(t)
	sub := ictx.Bus().Subscribe(&TradeSessionStartedEvent{})

	ictx.EmitPacket(t, enums.EMsg_EconTrading_StartSession, &pb.CMsgTrading_StartSession{
		OtherSteamid: proto.Uint64(FriendSteamID),
	})

	startEv := awaitEvent[*TradeSessionStartedEvent](t, sub.C())

	if startEv.OtherSteamID != FriendSteamID {
		t.Errorf("expected other steam ID %d, got %d", FriendSteamID, startEv.OtherSteamID)
	}
}

func TestHandleTradeStarted_InvalidProto_HandlesGracefully(t *testing.T) {
	t.Parallel()

	m, ictx := setupOffers(t)

	sub := ictx.Bus().Subscribe(&TradeSessionStartedEvent{})
	defer sub.Unsubscribe()

	assert.NotPanics(t, func() {
		m.handleTradeStarted(&protocol.Packet{
			Payload: []byte{0xFF, 0xFF},
		})
	})

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	t.Cleanup(cancel)

	select {
	case ev := <-sub.C():
		t.Fatalf("unexpected event published: %v", ev)
	case <-ctx.Done():
		// Success, no event published
	}
}
