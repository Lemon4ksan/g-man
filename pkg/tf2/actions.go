// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tf2

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/tf2"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
)

// RemoveItemName removes a custom name from an item.
func (t *TF2) RemoveItemName(ctx context.Context, itemID uint64) error {
	return t.sendSimpleItemAction(ctx, pb.EGCItemMsg_k_EMsgGCRemoveItemName, itemID)
}

// RemoveItemPaint removes custom paint from an item.
func (t *TF2) RemoveItemPaint(ctx context.Context, itemID uint64) error {
	return t.sendSimpleItemAction(ctx, pb.EGCItemMsg_k_EMsgGCRemoveItemPaint, itemID)
}

// RemoveMakersMark removes the "Crafted by X" tag from an item.
func (t *TF2) RemoveMakersMark(ctx context.Context, itemID uint64) error {
	return t.sendSimpleItemAction(ctx, pb.EGCItemMsg_k_EMsgGCRemoveMakersMark, itemID)
}

// ResetStrangeScores resets the kill count on a Strange item back to zero.
func (t *TF2) ResetStrangeScores(ctx context.Context, itemID uint64) error {
	return t.sendSimpleItemAction(ctx, pb.EGCItemMsg_k_EMsgGCResetStrangeScores, itemID)
}

// AcknowledgeItem tells the GC that the user has seen a newly dropped/traded item.
// This clears the "You have new items!" alert in the main menu.
func (t *TF2) AcknowledgeItem(ctx context.Context, itemID uint64) error {
	return t.sendSimpleItemAction(ctx, pb.EGCItemMsg_k_EMsgGCItemAcknowledged, itemID)
}

func (t *TF2) sendSimpleItemAction(ctx context.Context, msgType pb.EGCItemMsg, itemID uint64) error {
	data := make([]byte, 8)
	binary.LittleEndian.PutUint64(data, itemID)

	return t.gc.SendRaw(ctx, AppID, uint32(msgType), data)
}

func (t *TF2) AcknowledgeAll(ctx context.Context) error {
	items := t.cache.GetItems()

	var unacked []uint64
	for _, it := range items {
		if (it.Inventory>>30)&1 != 0 {
			unacked = append(unacked, it.ID)
		}
	}

	var err error
	for _, id := range unacked {
		if err = t.AcknowledgeItem(ctx, id); err != nil {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	return err
}

// SetItemStyle changes the style of a specific item (e.g., Painted or Alt styles).
func (t *TF2) SetItemStyle(ctx context.Context, itemID uint64, style uint32) error {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, itemID)
	_ = binary.Write(buf, binary.LittleEndian, style)

	return t.gc.SendRaw(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCSetItemStyle), buf.Bytes())
}

// SetItemPosition moves an item to a specific slot in the backpack.
func (t *TF2) SetItemPosition(ctx context.Context, itemID, position uint64) error {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, itemID)
	_ = binary.Write(buf, binary.LittleEndian, position)

	return t.gc.SendRaw(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCSetSingleItemPosition), buf.Bytes())
}

// DeleteItem permanently removes an item from your inventory.
func (t *TF2) DeleteItem(ctx context.Context, itemID uint64) error {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, itemID)

	return t.gc.SendRaw(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCDelete), buf.Bytes())
}

// UseItem triggers an action for an item (e.g., opening a badge or using a tool).
func (t *TF2) UseItem(ctx context.Context, itemID uint64) error {
	req := &pb.CMsgUseItem{
		ItemId: proto.Uint64(itemID),
	}

	return t.gc.Send(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCUseItemRequest), req)
}

// SortBackpack sorts the inventory based on a specific type (e.g., by rarity, type).
func (t *TF2) SortBackpack(ctx context.Context, sortType uint32) error {
	req := &pb.CMsgSortItems{
		SortType: proto.Uint32(sortType),
	}

	return t.gc.Send(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCSortItems), req)
}

// EquipItem assigns an item to a specific class and slot.
func (t *TF2) EquipItem(ctx context.Context, itemID uint64, classID, slot uint32) error {
	req := &pb.CMsgAdjustItemEquippedState{
		ItemId:   proto.Uint64(itemID),
		NewClass: proto.Uint32(classID),
		NewSlot:  proto.Uint32(slot),
	}

	return t.gc.Send(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCAdjustItemEquippedState), req)
}

// UnlockCrate uses a key to open a crate.
func (t *TF2) UnlockCrate(ctx context.Context, keyID, crateID uint64) error {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, keyID)
	_ = binary.Write(buf, binary.LittleEndian, crateID)

	return t.gc.SendRaw(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCUnlockCrate), buf.Bytes())
}

// WrapItem uses a gift wrap on an item.
func (t *TF2) WrapItem(ctx context.Context, wrapID, itemID uint64) error {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, wrapID)
	_ = binary.Write(buf, binary.LittleEndian, itemID)

	return t.gc.SendRaw(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCGiftWrapItem), buf.Bytes())
}

// DeliverGift sends a wrapped gift to another player.
func (t *TF2) DeliverGift(ctx context.Context, giftID, targetSteamID uint64) error {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, giftID)
	_ = binary.Write(buf, binary.LittleEndian, targetSteamID)

	return t.gc.SendRaw(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCDeliverGift), buf.Bytes())
}

// InviteToTrade invites another player to a live trade session.
func (t *TF2) InviteToTrade(ctx context.Context, steamID uint64) error {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, uint32(0)) // Unknown/Header
	_ = binary.Write(buf, binary.LittleEndian, steamID)

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
	_ = binary.Write(buf, binary.LittleEndian, resp)
	_ = binary.Write(buf, binary.LittleEndian, tradeID)

	return t.gc.SendRaw(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCTrading_InitiateTradeResponse), buf.Bytes())
}

// CancelTradeRequest cancels any active live trade invitation.
func (t *TF2) CancelTradeRequest(ctx context.Context) error {
	return t.gc.SendRaw(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCTrading_CancelSession), nil)
}

// Craft sends a crafting request.
func (t *TF2) Craft(ctx context.Context, items []uint64, recipe int16) ([]uint64, error) {
	// Format: [Recipe(int16)] [Count(int16)] [ItemID(uint64)]...
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, recipe)
	_ = binary.Write(buf, binary.LittleEndian, int16(len(items)))

	for _, id := range items {
		_ = binary.Write(buf, binary.LittleEndian, id)
	}

	resCh := make(chan []uint64, 1)
	errCh := make(chan error, 1)

	err := t.gc.CallRaw(
		ctx,
		AppID,
		uint32(pb.EGCItemMsg_k_EMsgGCCraft),
		buf.Bytes(),
		func(pkt *protocol.GCPacket, err error) {
			if err != nil {
				errCh <- err
				return
			}

			newItems := parseCraftResponse(pkt.Payload)
			resCh <- newItems
		},
	)
	if err != nil {
		return nil, err
	}

	select {
	case items := <-resCh:
		return items, nil
	case err := <-errCh:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type ItemPos struct {
	Id       uint64
	Position uint32
}

func (t *TF2) MoveItems(ctx context.Context, items []ItemPos) error {
	const maxBatchSize = 50

	for i := 0; i < len(items); i += maxBatchSize {
		end := min(i+maxBatchSize, len(items))
		batch := items[i:end]
		req := &pb.CMsgSetItemPositions{}

		for _, item := range batch {
			req.ItemPositions = append(req.ItemPositions, &pb.CMsgSetItemPositions_ItemPosition{
				ItemId:   proto.Uint64(item.Id),
				Position: proto.Uint32(item.Position),
			})
		}

		err := t.gc.Send(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCSetItemPositions), req)
		if err != nil {
			return fmt.Errorf("failed to send batch %d-%d: %w", i, end, err)
		}

		if end < len(items) {
			time.Sleep(200 * time.Millisecond)
		}
	}

	return nil
}
