// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package offers handles Steam trade invitations and real-time trading sessions.
package offers

import (
	"context"
	"fmt"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/modules"
	pb "github.com/lemon4ksan/g-man/pkg/steam/protobuf"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"google.golang.org/protobuf/proto"
)

const ModuleName string = "offers"

// Manager handles trade invitations (proposing, accepting, canceling).
// It embeds modules.BaseModule for lifecycle and concurrency management.
type Manager struct {
	modules.BaseModule

	// client is used to send Legacy Protobuf messages (EMsgs) to Steam.
	client service.Requester

	mu         sync.Mutex
	unregFuncs []func()
}

// New creates a new instance of the trade offers manager.
func New() *Manager {
	return &Manager{
		BaseModule: modules.NewBase(ModuleName),
	}
}

// Init registers network handlers for trade events.
func (m *Manager) Init(init modules.InitContext) error {
	if err := m.BaseModule.Init(init); err != nil {
		return err
	}

	m.client = init.Service()

	init.RegisterPacketHandler(protocol.EMsg_EconTrading_InitiateTradeProposed, m.handleTradeRequest)
	init.RegisterPacketHandler(protocol.EMsg_EconTrading_InitiateTradeResult, m.handleTradeResult)
	init.RegisterPacketHandler(protocol.EMsg_EconTrading_StartSession, m.handleTradeStarted)

	m.unregFuncs = append(m.unregFuncs, func() {
		init.UnregisterPacketHandler(protocol.EMsg_EconTrading_InitiateTradeProposed)
		init.UnregisterPacketHandler(protocol.EMsg_EconTrading_InitiateTradeResult)
		init.UnregisterPacketHandler(protocol.EMsg_EconTrading_StartSession)
	})

	return nil
}

// Close ensures all packet handlers are removed and background tasks are stopped.
func (m *Manager) Close() error {
	m.mu.Lock()
	for _, unreg := range m.unregFuncs {
		unreg()
	}
	m.unregFuncs = nil
	m.mu.Unlock()

	return m.BaseModule.Close()
}

// Invite sends a trade invitation to another Steam user.
func (m *Manager) Invite(ctx context.Context, otherSteamID uint64) error {
	req := &pb.CMsgTrading_InitiateTradeRequest{
		OtherSteamid: proto.Uint64(otherSteamID),
	}

	m.Logger.Info("Sending trade invitation", log.Uint64("target_steam_id", otherSteamID))

	_, err := service.Legacy[any](ctx, m.client, protocol.EMsg_EconTrading_InitiateTradeRequest, req)
	if err != nil {
		return fmt.Errorf("offers: failed to send invitation: %w", err)
	}
	return nil
}

// CancelInvitation revokes a pending trade invitation sent to another user.
func (m *Manager) CancelInvitation(ctx context.Context, otherSteamID uint64) error {
	req := &pb.CMsgTrading_CancelTradeRequest{
		OtherSteamid: proto.Uint64(otherSteamID),
	}

	m.Logger.Debug("Canceling trade invitation", log.Uint64("target_steam_id", otherSteamID))

	_, err := service.Legacy[any](ctx, m.client, protocol.EMsg_EconTrading_CancelTradeRequest, req)
	return err
}

// RespondToInvite approves or declines an incoming trade invitation.
func (m *Manager) RespondToInvite(ctx context.Context, tradeID uint32, accept bool) error {
	responseCode := protocol.EEconTradeResponse_Declined
	if accept {
		responseCode = protocol.EEconTradeResponse_Accepted
	}

	req := &pb.CMsgTrading_InitiateTradeResponse{
		TradeRequestId: proto.Uint32(tradeID),
		Response:       proto.Uint32(uint32(responseCode)),
	}

	m.Logger.Info("Responding to trade invitation",
		log.Uint32("trade_id", tradeID),
		log.Bool("accept", accept),
	)

	_, err := service.Legacy[any](ctx, m.client, protocol.EMsg_EconTrading_InitiateTradeResponse, req)
	return err
}

// --- Handlers ---

func (m *Manager) handleTradeRequest(p *protocol.Packet) {
	msg := &pb.CMsgTrading_InitiateTradeRequest{}
	if err := proto.Unmarshal(p.Payload, msg); err != nil {
		m.Logger.Error("Failed to unmarshal trade request", log.Err(err))
		return
	}

	otherID := msg.GetOtherSteamid()
	tradeID := msg.GetTradeRequestId()

	m.Bus.Publish(&TradeProposedEvent{
		OtherSteamID: otherID,
		TradeID:      tradeID,
		Respond: func(accept bool) {
			m.RespondToInvite(m.Ctx, tradeID, accept)
		},
	})
}

func (m *Manager) handleTradeResult(p *protocol.Packet) {
	msg := &pb.CMsgTrading_InitiateTradeResponse{}
	if err := proto.Unmarshal(p.Payload, msg); err != nil {
		m.Logger.Error("Failed to unmarshal trade result", log.Err(err))
		return
	}

	res := protocol.EEconTradeResponse(msg.GetResponse())

	m.Logger.Debug("Trade invitation result",
		log.Uint64("other_steam_id", msg.GetOtherSteamid()),
		log.String("result", res.String()),
	)

	m.Bus.Publish(&TradeResultEvent{
		OtherSteamID:           msg.GetOtherSteamid(),
		Response:               res,
		SteamGuardRequiredDays: msg.GetSteamguardRequiredDays(),
		NewDeviceCooldownDays:  msg.GetNewDeviceCooldownDays(),
	})
}

func (m *Manager) handleTradeStarted(p *protocol.Packet) {
	msg := &pb.CMsgTrading_StartSession{}
	if err := proto.Unmarshal(p.Payload, msg); err != nil {
		m.Logger.Error("Failed to unmarshal trade session start", log.Err(err))
		return
	}

	m.Logger.Info("Trade session started", log.Uint64("other_steam_id", msg.GetOtherSteamid()))

	m.Bus.Publish(&TradeSessionStartedEvent{
		OtherSteamID: msg.GetOtherSteamid(),
	})
}
