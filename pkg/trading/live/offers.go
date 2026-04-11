// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package live manages real-time trades invitations via the Steam Connection Manager (CM).

Unlike the `web` variant which polls for asynchronous trade offers, this
module handles the immediate, pop-up style trade requests that occur when
two users are online and agree to trade live.

# Key Features:

  - Send live trade invitations to other users (`Invite`).
  - Listen for incoming trade proposals (`TradeProposedEvent`).
  - Programmatically accept or decline incoming requests.
  - Publishes events for each stage of the live trade lifecycle
    (`TradeProposedEvent`, `TradeResultEvent`, `TradeSessionStartedEvent`).
*/
package live

import (
	"context"
	"fmt"
	"sync"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
)

const ModuleName string = "offers"

func WithModule() steam.Option {
	return func(c *steam.Client) {
		c.RegisterModule(New())
	}
}

// Manager handles trade invitations (proposing, accepting, canceling).
// It embeds modules.BaseModule for lifecycle and concurrency management.
type Manager struct {
	module.Base

	// client is used to send Legacy Protobuf messages (EMsgs) to Steam.
	client service.Doer

	mu         sync.Mutex
	unregFuncs []func()
}

// New creates a new instance of the trade offers manager.
func New() *Manager {
	return &Manager{
		Base: module.New(ModuleName),
	}
}

// Init registers network handlers for trade events.
func (m *Manager) Init(init module.InitContext) error {
	if err := m.Base.Init(init); err != nil {
		return err
	}

	m.client = init.Service()

	init.RegisterPacketHandler(enums.EMsg_EconTrading_InitiateTradeProposed, m.handleTradeRequest)
	init.RegisterPacketHandler(enums.EMsg_EconTrading_InitiateTradeResult, m.handleTradeResult)
	init.RegisterPacketHandler(enums.EMsg_EconTrading_StartSession, m.handleTradeStarted)

	m.unregFuncs = append(m.unregFuncs, func() {
		init.UnregisterPacketHandler(enums.EMsg_EconTrading_InitiateTradeProposed)
		init.UnregisterPacketHandler(enums.EMsg_EconTrading_InitiateTradeResult)
		init.UnregisterPacketHandler(enums.EMsg_EconTrading_StartSession)
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

	return m.Base.Close()
}

// Invite sends a trade invitation to another Steam user.
func (m *Manager) Invite(ctx context.Context, otherSteamID uint64) error {
	req := &pb.CMsgTrading_InitiateTradeRequest{
		OtherSteamid: proto.Uint64(otherSteamID),
	}

	m.Logger.Info("Sending trade invitation", log.Uint64("target_steam_id", otherSteamID))

	_, err := service.Legacy[service.NoResponse](ctx, m.client, enums.EMsg_EconTrading_InitiateTradeRequest, req)
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

	_, err := service.Legacy[service.NoResponse](ctx, m.client, enums.EMsg_EconTrading_CancelTradeRequest, req)

	return err
}

// RespondToInvite approves or declines an incoming trade invitation.
func (m *Manager) RespondToInvite(ctx context.Context, tradeID uint32, accept bool) error {
	responseCode := enums.EEconTradeResponse_Declined
	if accept {
		responseCode = enums.EEconTradeResponse_Accepted
	}

	req := &pb.CMsgTrading_InitiateTradeResponse{
		TradeRequestId: proto.Uint32(tradeID),
		Response:       proto.Uint32(uint32(responseCode)),
	}

	m.Logger.Info("Responding to trade invitation",
		log.Uint32("trade_id", tradeID),
		log.Bool("accept", accept),
	)

	_, err := service.Legacy[service.NoResponse](ctx, m.client, enums.EMsg_EconTrading_InitiateTradeResponse, req)

	return err
}

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
			_ = m.RespondToInvite(m.Ctx, tradeID, accept)
		},
	})
}

func (m *Manager) handleTradeResult(p *protocol.Packet) {
	msg := &pb.CMsgTrading_InitiateTradeResponse{}
	if err := proto.Unmarshal(p.Payload, msg); err != nil {
		m.Logger.Error("Failed to unmarshal trade result", log.Err(err))
		return
	}

	res := enums.EEconTradeResponse(msg.GetResponse())

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
