// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package offers

import (
	"context"

	"github.com/lemon4ksan/g-man/log"
	"github.com/lemon4ksan/g-man/steam"
	"github.com/lemon4ksan/g-man/steam/protocol"
	pb "github.com/lemon4ksan/g-man/steam/protocol/protobuf"
	"google.golang.org/protobuf/proto"
)

const ModuleName string = "offers"

type Manager struct {
	client *steam.Client
	logger log.Logger
}

func (t *Manager) Name() string { return ModuleName }

func (t *Manager) Init(c *steam.Client) error {
	t.client = c

	c.RegisterPacketHandler(protocol.EMsg_EconTrading_InitiateTradeProposed, t.handleTradeRequest)
	c.RegisterPacketHandler(protocol.EMsg_EconTrading_InitiateTradeResult, t.handleTradeResult)
	c.RegisterPacketHandler(protocol.EMsg_EconTrading_StartSession, t.handleTradeStarted)

	return nil
}

func (t *Manager) Start(ctx context.Context) error {
	return nil // No background loop needed
}

// Invite sends a trade invitation to another user.
func (t *Manager) Invite(ctx context.Context, otherSteamID uint64) error {
	req := &pb.CMsgTrading_InitiateTradeRequest{
		OtherSteamid: &otherSteamID,
	}
	t.logger.Info("Sending trade invitation", log.Uint64("target", otherSteamID))
	return t.client.Proto().CallLegacy(ctx, protocol.EMsg_EconTrading_InitiateTradeRequest, req, nil)
}

// CancelInvitation cancels a pending trade invitation.
func (t *Manager) CancelInvitation(ctx context.Context, otherSteamID uint64) error {
	req := &pb.CMsgTrading_CancelTradeRequest{
		OtherSteamid: &otherSteamID,
	}
	return t.client.Proto().CallLegacy(ctx, protocol.EMsg_EconTrading_CancelTradeRequest, req, nil)
}

// RespondToInvite is a helper to accept or decline a trade.
func (t *Manager) RespondToInvite(ctx context.Context, tradeID uint32, accept bool) error {
	res := protocol.EEconTradeResponse_Declined
	if accept {
		res = protocol.EEconTradeResponse_Accepted
	}

	req := &pb.CMsgTrading_InitiateTradeResponse{
		TradeRequestId: &tradeID,
		Response:       proto.Uint32((uint32)(res)),
	}

	t.logger.Info("Responding to trade invitation", log.Uint32("id", tradeID), log.Bool("accept", accept))
	return t.client.Proto().CallLegacy(ctx, protocol.EMsg_EconTrading_InitiateTradeResponse, req, nil)
}

func (t *Manager) handleTradeRequest(p *protocol.Packet) {
	msg := &pb.CMsgTrading_InitiateTradeRequest{}
	if err := proto.Unmarshal(p.Payload, msg); err != nil {
		return
	}

	otherID := msg.GetOtherSteamid()
	tradeID := msg.GetTradeRequestId()

	t.client.Bus().Publish(&TradeProposedEvent{
		OtherSteamID: otherID,
		TradeID:      tradeID,
		Respond: func(accept bool) {
			// Using Background context here because this is usually triggered
			// by an external UI action or a long-lived goroutine.
			t.RespondToInvite(context.Background(), tradeID, accept)
		},
	})
}

func (t *Manager) handleTradeResult(p *protocol.Packet) {
	msg := &pb.CMsgTrading_InitiateTradeResponse{}
	if err := proto.Unmarshal(p.Payload, msg); err != nil {
		return
	}

	res := protocol.EEconTradeResponse(msg.GetResponse())
	t.logger.Debug("Trade result received",
		log.Uint64("other", msg.GetOtherSteamid()),
		log.String("result", res.String()),
	)

	t.client.Bus().Publish(&TradeResultEvent{
		OtherSteamID:           msg.GetOtherSteamid(),
		Response:               res,
		SteamGuardRequiredDays: msg.GetSteamguardRequiredDays(),
		NewDeviceCooldownDays:  msg.GetNewDeviceCooldownDays(),
	})
}

func (t *Manager) handleTradeStarted(p *protocol.Packet) {
	msg := &pb.CMsgTrading_StartSession{}
	t.logger.Info("Trade session started", log.Uint64("other", msg.GetOtherSteamid()))
	t.client.Bus().Publish(&TradeSessionStartedEvent{
		OtherSteamID: msg.GetOtherSteamid(),
	})
}
