package tf2

import (
	"bytes"
	"context"
	"encoding/binary"

	pb "github.com/lemon4ksan/g-man/pkg/tf2/protobuf"
)

// InviteToTrade invites another player to a live trade session.
func (t *TF2) InviteToTrade(ctx context.Context, steamID uint64) error {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint32(0)) // Unknown/Header
	binary.Write(buf, binary.LittleEndian, steamID)
	return t.gc.SendRaw(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCTrading_InitiateTradeRequest), buf.Bytes())
}

// RespondToTrade handles an incoming live trade request.
func (t *TF2) RespondToTrade(ctx context.Context, tradeID uint32, accept bool) error {
	const (
		ResponseAccepted = 0
		ResponseDeclined = 1
	)

	resp := uint32(ResponseDeclined)
	if accept {
		resp = ResponseAccepted
	}

	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, resp)
	binary.Write(buf, binary.LittleEndian, tradeID)
	return t.gc.SendRaw(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCTrading_InitiateTradeResponse), buf.Bytes())
}

// CancelTradeRequest cancels any active live trade invitation.
func (t *TF2) CancelTradeRequest(ctx context.Context) error {
	return t.gc.SendRaw(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCTrading_CancelSession), nil)
}

// WrapItem uses a gift wrap on an item.
func (t *TF2) WrapItem(ctx context.Context, wrapID, itemID uint64) error {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, wrapID)
	binary.Write(buf, binary.LittleEndian, itemID)
	return t.gc.SendRaw(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCGiftWrapItem), buf.Bytes())
}

// DeliverGift sends a wrapped gift to another player.
func (t *TF2) DeliverGift(ctx context.Context, giftID, targetSteamID uint64) error {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, giftID)
	binary.Write(buf, binary.LittleEndian, targetSteamID)
	return t.gc.SendRaw(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCDeliverGift), buf.Bytes())
}
