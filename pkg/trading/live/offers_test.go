// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package live

import (
	"testing"
	"time"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/protocol"
	"github.com/lemon4ksan/g-man/test/module"
	"google.golang.org/protobuf/proto"
)

const (
	FriendSteamID = 123456789
	TradeID       = 555
)

func setupOffers(t *testing.T) (*Manager, *module.InitContext) {
	t.Helper()
	m := New()
	ictx := module.NewInitContext()

	if err := m.Init(ictx); err != nil {
		t.Fatalf("failed to init offers manager: %v", err)
	}

	t.Cleanup(func() {
		_ = m.Close()
	})

	return m, ictx
}

func TestManager_InitAndClose(t *testing.T) {
	m := New()
	ictx := module.NewInitContext()

	if m.Name() != ModuleName {
		t.Errorf("expected %s, got %s", ModuleName, m.Name())
	}

	expectedEMsgs := []protocol.EMsg{
		protocol.EMsg_EconTrading_InitiateTradeProposed,
		protocol.EMsg_EconTrading_InitiateTradeResult,
		protocol.EMsg_EconTrading_StartSession,
	}

	t.Run("Init", func(t *testing.T) {
		_ = m.Init(ictx)
		for _, emsg := range expectedEMsgs {
			ictx.AssertPacketHandlerRegistered(t, emsg)
		}
	})

	t.Run("Close", func(t *testing.T) {
		_ = m.Close()
		for _, emsg := range expectedEMsgs {
			ictx.AssertPacketHandlerUnregistered(t, emsg)
		}
	})
}

func TestManager_Invitations(t *testing.T) {
	m, ictx := setupOffers(t)

	t.Run("Invite", func(t *testing.T) {
		if err := m.Invite(t.Context(), FriendSteamID); err != nil {
			t.Fatalf("Invite failed: %v", err)
		}

		req := &pb.CMsgTrading_InitiateTradeRequest{}
		ictx.MockServiceAccessor().GetLastCall(req)
		if req.GetOtherSteamid() != FriendSteamID {
			t.Errorf("expected target %d, got %d", FriendSteamID, req.GetOtherSteamid())
		}
	})

	t.Run("Cancel", func(t *testing.T) {
		if err := m.CancelInvitation(t.Context(), FriendSteamID); err != nil {
			t.Fatalf("Cancel failed: %v", err)
		}

		req := &pb.CMsgTrading_CancelTradeRequest{}
		ictx.MockServiceAccessor().GetLastCall(req)
		if req.GetOtherSteamid() != FriendSteamID {
			t.Errorf("expected target %d, got %d", FriendSteamID, req.GetOtherSteamid())
		}
	})
}

func TestManager_RespondToInvite(t *testing.T) {
	m, ictx := setupOffers(t)

	tests := []struct {
		accept   bool
		expected protocol.EEconTradeResponse
	}{
		{accept: true, expected: protocol.EEconTradeResponse_Accepted},
		{accept: false, expected: protocol.EEconTradeResponse_Declined},
	}

	for _, tt := range tests {
		name := "Decline"
		if tt.accept {
			name = "Accept"
		}

		t.Run(name, func(t *testing.T) {
			err := m.RespondToInvite(t.Context(), TradeID, tt.accept)
			if err != nil {
				t.Fatalf("RespondToInvite failed: %v", err)
			}

			req := &pb.CMsgTrading_InitiateTradeResponse{}
			ictx.MockServiceAccessor().GetLastCall(req)

			if req.GetTradeRequestId() != TradeID {
				t.Errorf("expected trade ID %d, got %d", TradeID, req.GetTradeRequestId())
			}
			if req.GetResponse() != uint32(tt.expected) {
				t.Errorf("expected response %v, got %v", tt.expected, req.GetResponse())
			}
		})
	}
}

func TestManager_HandleTradeProposed(t *testing.T) {
	_, ictx := setupOffers(t)
	sub := ictx.Bus().Subscribe(&TradeProposedEvent{})

	ictx.EmitPacket(t, protocol.EMsg_EconTrading_InitiateTradeProposed, &pb.CMsgTrading_InitiateTradeRequest{
		OtherSteamid:   proto.Uint64(FriendSteamID),
		TradeRequestId: proto.Uint32(TradeID),
	})

	select {
	case ev := <-sub.C():
		tradeEv := ev.(*TradeProposedEvent)
		if tradeEv.OtherSteamID != FriendSteamID || tradeEv.TradeID != TradeID {
			t.Errorf("invalid event data: %+v", tradeEv)
		}

		if tradeEv.Respond == nil {
			t.Fatal("Respond function in event is nil")
		}

		tradeEv.Respond(true)

		req := &pb.CMsgTrading_InitiateTradeResponse{}
		ictx.MockServiceAccessor().GetLastCall(req)
		if req.GetResponse() != uint32(protocol.EEconTradeResponse_Accepted) {
			t.Error("Respond(true) should send Accepted response")
		}

	case <-time.After(500 * time.Millisecond):
		t.Fatal("TradeProposedEvent not received")
	}
}

func TestManager_HandleTradeResult(t *testing.T) {
	_, ictx := setupOffers(t)
	sub := ictx.Bus().Subscribe(&TradeResultEvent{})

	ictx.EmitPacket(t, protocol.EMsg_EconTrading_InitiateTradeResult, &pb.CMsgTrading_InitiateTradeResponse{
		OtherSteamid:           proto.Uint64(FriendSteamID),
		Response:               proto.Uint32(uint32(protocol.EEconTradeResponse_TooSoon)),
		SteamguardRequiredDays: proto.Uint32(15),
		NewDeviceCooldownDays:  proto.Uint32(7),
	})

	select {
	case ev := <-sub.C():
		res := ev.(*TradeResultEvent)
		if res.OtherSteamID != FriendSteamID || res.Response != protocol.EEconTradeResponse_TooSoon {
			t.Errorf("unexpected event: %+v", res)
		}
		if res.SteamGuardRequiredDays != 15 || res.NewDeviceCooldownDays != 7 {
			t.Errorf("invalid cooldown info in event: SG=%d, Dev=%d", res.SteamGuardRequiredDays, res.NewDeviceCooldownDays)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("TradeResultEvent not received")
	}
}

func TestManager_HandleTradeStarted(t *testing.T) {
	_, ictx := setupOffers(t)
	sub := ictx.Bus().Subscribe(&TradeSessionStartedEvent{})

	ictx.EmitPacket(t, protocol.EMsg_EconTrading_StartSession, &pb.CMsgTrading_StartSession{
		OtherSteamid: proto.Uint64(FriendSteamID),
	})

	select {
	case ev := <-sub.C():
		startEv := ev.(*TradeSessionStartedEvent)
		if startEv.OtherSteamID != FriendSteamID {
			t.Errorf("expected other ID %d, got %d", FriendSteamID, startEv.OtherSteamID)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("TradeSessionStartedEvent not received")
	}
}
