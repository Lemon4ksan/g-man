// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package live manages real-time Steam live trade invitations via Connection Manager binary packets.
package live

import (
	"context"
	"fmt"

	"github.com/lemon4ksan/foundation/async/log"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/steam/client"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	pb "github.com/lemon4ksan/g-man/protobuf/steam"
)

const ModuleName string = "offers"

// WithModule registers the Manager module in the client.
func WithModule() client.Option {
	return client.WithModule(New())
}

// From retrieves the Manager module instance from the client.
func From(c *client.Client) *Manager {
	return client.GetModule[*Manager](c)
}

type Manager struct {
	module.Base

	events Events
}

func New() *Manager {
	return &Manager{
		Base: module.New(ModuleName),
	}
}

func (m *Manager) Init(init module.InitContext) error {
	if err := m.Base.Init(init); err != nil {
		return err
	}

	m.events = NewEvents(init)

	m.events.OnInitiateTradeProposed(m.handleTradeRequest)
	m.events.OnInitiateTradeResult(m.handleTradeResult)
	m.events.OnStartSession(m.handleTradeStarted)

	return nil
}

func (m *Manager) Close() error {
	if m.events != nil {
		_ = m.events.Close()
	}

	return m.Base.Close()
}

// Invite sends a live trade invitation to another user.
func (m *Manager) Invite(ctx context.Context, otherSteamID uint64) error {
	req := &pb.CMsgTrading_InitiateTradeRequest{
		OtherSteamid: proto.Uint64(otherSteamID),
	}

	m.Logger.Info("Sending trade invitation", log.Uint64("target_steam_id", otherSteamID))

	err := m.events.SendTradeRequest(ctx, req)
	if err != nil {
		return fmt.Errorf("offers: failed to send invitation: %w", err)
	}

	return nil
}

// CancelInvitation cancels an outgoing live trade invitation.
func (m *Manager) CancelInvitation(ctx context.Context, otherSteamID uint64) error {
	req := &pb.CMsgTrading_CancelTradeRequest{
		OtherSteamid: proto.Uint64(otherSteamID),
	}

	m.Logger.Debug("Canceling trade invitation", log.Uint64("target_steam_id", otherSteamID))

	return m.events.CancelTradeRequest(ctx, req)
}

// RespondToInvite approves or declines an incoming live trade invitation.
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

	return m.events.SendTradeResponse(ctx, req)
}

func (m *Manager) handleTradeRequest(msg *pb.CMsgTrading_InitiateTradeRequest) {
	if msg == nil {
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

func (m *Manager) handleTradeResult(msg *pb.CMsgTrading_InitiateTradeResponse) {
	if msg == nil {
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

func (m *Manager) handleTradeStarted(msg *pb.CMsgTrading_StartSession) {
	if msg == nil {
		return
	}

	m.Logger.Info("Trade session started", log.Uint64("other_steam_id", msg.GetOtherSteamid()))

	m.Bus.Publish(&TradeSessionStartedEvent{
		OtherSteamID: msg.GetOtherSteamid(),
	})
}
