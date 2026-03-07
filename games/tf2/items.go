// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tf2

import (
	"bytes"
	"context"
	"encoding/binary"

	pb "github.com/lemon4ksan/g-man/games/tf2/protocol/protobuf"
	"google.golang.org/protobuf/proto"
)

// SetItemStyle changes the style of a specific item (e.g., Painted or Alt styles).
func (t *TF2) SetItemStyle(ctx context.Context, itemID uint64, style uint32) error {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, itemID)
	binary.Write(buf, binary.LittleEndian, style)
	return t.gc.SendRaw(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCSetItemStyle), buf.Bytes())
}

// SetItemPosition moves an item to a specific slot in the backpack.
func (t *TF2) SetItemPosition(ctx context.Context, itemID uint64, position uint64) error {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, itemID)
	binary.Write(buf, binary.LittleEndian, position)
	return t.gc.SendRaw(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCSetSingleItemPosition), buf.Bytes())
}

// DeleteItem permanently removes an item from your inventory.
func (t *TF2) DeleteItem(ctx context.Context, itemID uint64) error {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, itemID)
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
func (t *TF2) EquipItem(ctx context.Context, itemID uint64, classID uint32, slot uint32) error {
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
	binary.Write(buf, binary.LittleEndian, keyID)
	binary.Write(buf, binary.LittleEndian, crateID)
	return t.gc.SendRaw(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCUnlockCrate), buf.Bytes())
}
