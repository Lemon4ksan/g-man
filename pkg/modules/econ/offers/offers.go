// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package offers

import (
	"context"
	"errors"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"
	pb "github.com/lemon4ksan/g-man/pkg/steam/protobuf"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"google.golang.org/protobuf/proto"
)

const ModuleName string = "offers"

type Manager struct {
	bus       *bus.Bus
	client    api.LegacyRequester
	logger    log.Logger
	closeFunc func()
}

func New() *Manager {
	return &Manager{
		logger: log.Discard,
	}
}

func (m *Manager) Name() string { return ModuleName }

func (m *Manager) Init(init steam.InitContext) error {
	m.bus = init.Bus()
	if m.bus == nil {
		return errors.New("nil bus")
	}
	m.client = init.Proto()
	if m.client == nil {
		return errors.New("nil proto client")
	}
	m.logger = init.Logger().WithModule(ModuleName)

	init.RegisterPacketHandler(protocol.EMsg_EconTrading_InitiateTradeProposed, m.handleTradeRequest)
	init.RegisterPacketHandler(protocol.EMsg_EconTrading_InitiateTradeResult, m.handleTradeResult)
	init.RegisterPacketHandler(protocol.EMsg_EconTrading_StartSession, m.handleTradeStarted)

	m.closeFunc = func() {
		init.UnregisterPacketHandler(protocol.EMsg_EconTrading_InitiateTradeProposed)
		init.UnregisterPacketHandler(protocol.EMsg_EconTrading_InitiateTradeResult)
		init.UnregisterPacketHandler(protocol.EMsg_EconTrading_StartSession)
	}
	return nil
}

func (m *Manager) Start(ctx context.Context) error {
	return nil
}

func (m *Manager) Close() error {
	if m.closeFunc != nil {
		m.closeFunc()
		m.closeFunc = nil
	}
	return nil
}

// Invite sends a trade invitation to another user.
func (m *Manager) Invite(ctx context.Context, otherSteamID uint64) error {
	req := &pb.CMsgTrading_InitiateTradeRequest{
		OtherSteamid: &otherSteamID,
	}
	m.logger.Info("Sending trade invitation", log.Uint64("target", otherSteamID))
	return m.client.CallLegacy(ctx, protocol.EMsg_EconTrading_InitiateTradeRequest, req, nil)
}

// CancelInvitation cancels a pending trade invitation.
func (m *Manager) CancelInvitation(ctx context.Context, otherSteamID uint64) error {
	req := &pb.CMsgTrading_CancelTradeRequest{
		OtherSteamid: &otherSteamID,
	}
	return m.client.CallLegacy(ctx, protocol.EMsg_EconTrading_CancelTradeRequest, req, nil)
}

// RespondToInvite is a helper to accept or decline a trade.
func (m *Manager) RespondToInvite(ctx context.Context, tradeID uint32, accept bool) error {
	res := protocol.EEconTradeResponse_Declined
	if accept {
		res = protocol.EEconTradeResponse_Accepted
	}

	req := &pb.CMsgTrading_InitiateTradeResponse{
		TradeRequestId: &tradeID,
		Response:       proto.Uint32((uint32)(res)),
	}

	m.logger.Info("Responding to trade invitation", log.Uint32("id", tradeID), log.Bool("accept", accept))
	return m.client.CallLegacy(ctx, protocol.EMsg_EconTrading_InitiateTradeResponse, req, nil)
}

func (m *Manager) handleTradeRequest(p *protocol.Packet) {
	msg := &pb.CMsgTrading_InitiateTradeRequest{}
	if err := proto.Unmarshal(p.Payload, msg); err != nil {
		return
	}

	otherID := msg.GetOtherSteamid()
	tradeID := msg.GetTradeRequestId()

	m.bus.Publish(&TradeProposedEvent{
		OtherSteamID: otherID,
		TradeID:      tradeID,
		Respond: func(accept bool) {
			// Using Background context here because this is usually triggered
			// by an external UI action or a long-lived goroutine.
			m.RespondToInvite(context.Background(), tradeID, accept)
		},
	})
}

func (m *Manager) handleTradeResult(p *protocol.Packet) {
	msg := &pb.CMsgTrading_InitiateTradeResponse{}
	if err := proto.Unmarshal(p.Payload, msg); err != nil {
		return
	}

	res := protocol.EEconTradeResponse(msg.GetResponse())
	m.logger.Debug("Trade result received",
		log.Uint64("other", msg.GetOtherSteamid()),
		log.String("result", res.String()),
	)

	m.bus.Publish(&TradeResultEvent{
		OtherSteamID:           msg.GetOtherSteamid(),
		Response:               res,
		SteamGuardRequiredDays: msg.GetSteamguardRequiredDays(),
		NewDeviceCooldownDays:  msg.GetNewDeviceCooldownDays(),
	})
}

func (m *Manager) handleTradeStarted(p *protocol.Packet) {
	msg := &pb.CMsgTrading_StartSession{}
	if err := proto.Unmarshal(p.Payload, msg); err != nil {
		m.logger.Error("Failed to unmarshal StartSession", log.Err(err))
		return
	}

	m.logger.Info("Trade session started", log.Uint64("other", msg.GetOtherSteamid()))
	m.bus.Publish(&TradeSessionStartedEvent{
		OtherSteamID: msg.GetOtherSteamid(),
	})
}
