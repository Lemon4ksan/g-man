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
)

// RemoveItemName removes a custom name from an item.
func (t *TF2) RemoveItemName(ctx context.Context, itemID uint64) error {
	data := make([]byte, 9)
	binary.LittleEndian.PutUint64(data[0:8], itemID)
	data[8] = 0 // m_bDescription = false (we want to remove the Name)

	return t.gc.SendRaw(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCRemoveItemName), data)
}

// RemoveItemDescription removes a custom description from an item.
func (t *TF2) RemoveItemDescription(ctx context.Context, itemID uint64) error {
	data := make([]byte, 9)
	binary.LittleEndian.PutUint64(data[0:8], itemID)
	data[8] = 1 // m_bDescription = true (we want to remove the Description)

	return t.gc.SendRaw(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCRemoveItemName), data)
}

// RemoveItemPaint removes custom paint from an item.
func (t *TF2) RemoveItemPaint(ctx context.Context, itemID uint64) error {
	req := &pb.CMsgGCRemoveCustomizationAttributeSimple{
		ItemId: proto.Uint64(itemID),
	}

	return t.gc.Send(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCRemoveItemPaint), req)
}

// RemoveMakersMark removes the "Crafted by X" tag from an item.
func (t *TF2) RemoveMakersMark(ctx context.Context, itemID uint64) error {
	req := &pb.CMsgGCRemoveCustomizationAttributeSimple{
		ItemId: proto.Uint64(itemID),
	}

	return t.gc.Send(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCRemoveMakersMark), req)
}

// ResetStrangeScores resets the kill count on a Strange item back to zero.
func (t *TF2) ResetStrangeScores(ctx context.Context, itemID uint64) error {
	req := &pb.CMsgGCResetStrangeScores{
		ItemId: proto.Uint64(itemID),
	}

	return t.gc.Send(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCResetStrangeScores), req)
}

// RemoveKillstreak removes a killstreak kit from an item.
func (t *TF2) RemoveKillstreak(ctx context.Context, itemID uint64) error {
	req := &pb.CMsgGCRemoveCustomizationAttributeSimple{
		ItemId: proto.Uint64(itemID),
	}

	return t.gc.Send(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCRemoveKillStreak), req)
}

// RemoveFestivizer removes a festivizer from an item.
func (t *TF2) RemoveFestivizer(ctx context.Context, itemID uint64) error {
	req := &pb.CMsgGCRemoveCustomizationAttributeSimple{
		ItemId: proto.Uint64(itemID),
	}

	return t.gc.Send(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCRemoveFestivizer), req)
}

// RemoveGiftedBy removes the "Gifted by X" tag from an item.
func (t *TF2) RemoveGiftedBy(ctx context.Context, itemID uint64) error {
	req := &pb.CMsgGCRemoveCustomizationAttributeSimple{
		ItemId: proto.Uint64(itemID),
	}

	return t.gc.Send(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCRemoveGiftedBy), req)
}

// RemoveItemAttribute removes a customization attribute (like a spell or strange part) from an item.
// Note: This uses a known GC "hack" where the attribute ID is used as the message type.
func (t *TF2) RemoveItemAttribute(ctx context.Context, itemID uint64, attributeID uint32) error {
	req := &pb.CMsgGCRemoveCustomizationAttributeSimple{
		ItemId: proto.Uint64(itemID),
	}

	return t.gc.Send(ctx, AppID, attributeID, req)
}

// AcknowledgeItem tells the GC that the user has seen a newly dropped/traded item.
// In TF2, this is done by moving the item from position 0 to a valid backpack position.
func (t *TF2) AcknowledgeItem(ctx context.Context, itemID uint64) error {
	// Move it to the first slot (position 1) to acknowledge it.
	return t.SetItemPosition(ctx, itemID, 1)
}

// NameItem applies a name tag to an item.
func (t *TF2) NameItem(ctx context.Context, toolID, itemID uint64, name string) error {
	return t.nameOrDescribeItem(ctx, toolID, itemID, name, false)
}

// DescribeItem applies a description tag to an item.
func (t *TF2) DescribeItem(ctx context.Context, toolID, itemID uint64, description string) error {
	return t.nameOrDescribeItem(ctx, toolID, itemID, description, true)
}

func (t *TF2) nameOrDescribeItem(ctx context.Context, toolID, itemID uint64, text string, isDescription bool) error {
	// Structure: [ToolID(8)] [ItemID(8)] [IsDescription(1)] [Text(var)]
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, toolID)
	_ = binary.Write(buf, binary.LittleEndian, itemID)

	if isDescription {
		buf.WriteByte(1)
	} else {
		buf.WriteByte(0)
	}

	buf.WriteString(text)
	buf.WriteByte(0) // Null terminator

	return t.gc.SendRaw(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCNameItem), buf.Bytes())
}

// AcknowledgeAll acknowledges all unacknowledged items by moving them to the first available slots.
func (t *TF2) AcknowledgeAll(ctx context.Context) error {
	items := t.cache.GetItems()

	var toMove []ItemPos

	// In TF2, unacknowledged items have position 0 or bit 30 set.
	nextSlot := uint32(1)

	for _, it := range items {
		isNew := (it.Inventory >> 30) & 1
		if it.Position() == 0 || isNew == 1 {
			toMove = append(toMove, ItemPos{Id: it.ID, Position: nextSlot})
			nextSlot++
		}
	}

	if len(toMove) == 0 {
		return nil
	}

	return t.MoveItems(ctx, toMove)
}

// SetItemStyle changes the style of a specific item (e.g., Painted or Alt styles).
// Based on Source SDK: struct MsgGCSetItemStyle_t { uint64 itemID; uint8 style; }
func (t *TF2) SetItemStyle(ctx context.Context, itemID uint64, style uint8) error {
	data := make([]byte, 9)
	binary.LittleEndian.PutUint64(data[0:8], itemID)
	data[8] = style

	return t.gc.SendRaw(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCSetItemStyle), data)
}

// SetItemPosition moves an item to a specific slot in the backpack.
// Based on Source SDK: struct MsgGCSetItemPosition_t { uint64 itemID; uint32 position; }
func (t *TF2) SetItemPosition(ctx context.Context, itemID uint64, position uint32) error {
	data := make([]byte, 12)
	binary.LittleEndian.PutUint64(data[0:8], itemID)
	binary.LittleEndian.PutUint32(data[8:12], position)

	return t.gc.SendRaw(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCSetSingleItemPosition), data)
}

// DeleteItem permanently removes an item from your inventory.
func (t *TF2) DeleteItem(ctx context.Context, itemID uint64) error {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, itemID)

	return t.gc.SendRaw(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCDelete), buf.Bytes())
}

// SetUnusualEffectOffset adjusts the vertical offset of an Unusual effect on an item.
func (t *TF2) SetUnusualEffectOffset(ctx context.Context, itemID uint64, offset float32) error {
	req := &pb.CMsgSetItemEffectVerticalOffset{
		ItemId: proto.Uint64(itemID),
		Offset: proto.Float32(offset),
	}

	return t.gc.Send(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCSetItemEffectVerticalOffset), req)
}

// TransferStrangeCount uses a Strange Count Transfer Tool to move stats between two Strange items.
func (t *TF2) TransferStrangeCount(ctx context.Context, toolID, srcID, destID uint64) error {
	req := &pb.CMsgApplyStrangeCountTransfer{
		ToolItemId:     proto.Uint64(toolID),
		ItemSrcItemId:  proto.Uint64(srcID),
		ItemDestItemId: proto.Uint64(destID),
	}

	return t.gc.Send(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCApplyStrangeCountTransfer), req)
}

// ShuffleCrate uses a shuffle tool (like Summer Adventure) to randomize crate contents.
func (t *TF2) ShuffleCrate(ctx context.Context, itemID uint64, userCode string) error {
	req := &pb.CMsgGCShuffleCrateContents{
		CrateItemId:    proto.Uint64(itemID),
		UserCodeString: proto.String(userCode),
	}

	return t.gc.Send(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCShuffleCrateContents), req)
}

// ApplyAutograph applies an autograph tool to an item.
func (t *TF2) ApplyAutograph(ctx context.Context, toolID, itemID uint64) error {
	req := &pb.CMsgApplyAutograph{
		AutographItemId: proto.Uint64(toolID),
		ItemItemId:      proto.Uint64(itemID),
	}

	return t.gc.Send(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCApplyAutograph), req)
}

// RequestMarketData requests official Steam Market data (prices) for items from the GC.
func (t *TF2) RequestMarketData(ctx context.Context, currency uint32) error {
	req := &pb.CMsgGCClientMarketDataRequest{
		UserCurrency: proto.Uint32(currency),
	}

	return t.gc.Send(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCClientRequestMarketData), req)
}

// ReportPlayer sends a formal report against a player to the GC.
// matchID can be 0 if not in a match. Reason IDs can be found in the game files (e.g., 1=Cheating).
func (t *TF2) ReportPlayer(ctx context.Context, accountID uint32, reason *pb.CMsgGC_ReportPlayer_EReason) error {
	req := &pb.CMsgGC_ReportPlayer{
		AccountIdTarget: proto.Uint32(accountID),
		Reason:          reason,
	}

	return t.gc.Send(ctx, AppID, uint32(pb.ETFGCMsg_k_EMsgGC_ReportPlayer), req)
}

// RequestFriends requests a list of TF2-related data for the given account IDs.
func (t *TF2) RequestFriends(ctx context.Context, accountIDs []uint32) error {
	req := &pb.CMsgTFRequestTF2Friends{
		AccountIds: accountIDs,
	}

	return t.gc.Send(ctx, AppID, uint32(pb.ETFGCMsg_k_EMsgGCRequestTF2Friends), req)
}

// UseItem triggers an action for an item (e.g., opening a badge or using a tool).
func (t *TF2) UseItem(ctx context.Context, itemID uint64) error {
	req := &pb.CMsgUseItem{
		ItemId: proto.Uint64(itemID),
	}

	return t.gc.Send(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCUseItemRequest), req)
}

// ApplyStrangePart applies a strange part to a strange item.
func (t *TF2) ApplyStrangePart(ctx context.Context, itemID, partID uint64) error {
	req := &pb.CMsgApplyStrangePart{
		ItemItemId:        proto.Uint64(itemID),
		StrangePartItemId: proto.Uint64(partID),
	}

	return t.gc.Send(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCApplyStrangePart), req)
}

// ApplyStrangifier applies a strangifier or unusualifier to an item.
// Based on Source SDK: uses k_EMsgGCApplyXifier (1082) with CMsgApplyToolToItem
func (t *TF2) ApplyStrangifier(ctx context.Context, itemID, toolID uint64) error {
	req := &pb.CMsgApplyToolToItem{
		ToolItemId:    proto.Uint64(toolID),
		SubjectItemId: proto.Uint64(itemID),
	}

	return t.gc.Send(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCApplyXifier), req)
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

// ApplyPaint applies a paint can tool to an item.
// Based on Source SDK: struct MsgGCPaintItem_t { uint64 toolID; uint64 subjectID; }
func (t *TF2) ApplyPaint(ctx context.Context, toolID, itemID uint64) error {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, toolID)
	_ = binary.Write(buf, binary.LittleEndian, itemID)

	return t.gc.SendRaw(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCPaintItem), buf.Bytes())
}

// UnwrapGift unpacks a gift in your inventory.
// Based on Source SDK: struct MsgGCUnwrapGiftRequest_t { uint64 itemID; }
func (t *TF2) UnwrapGift(ctx context.Context, itemID uint64) error {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, itemID)

	return t.gc.SendRaw(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCUnwrapGiftRequest), buf.Bytes())
}

// CancelTradeRequest cancels any active live trade invitation.
func (t *TF2) CancelTradeRequest(ctx context.Context) error {
	return t.gc.SendRaw(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCTrading_CancelSession), nil)
}

// ItemPos represents an item position.
type ItemPos struct {
	Id       uint64
	Position uint32
}

// MoveItems moves items to specific positions in the backpack.
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
