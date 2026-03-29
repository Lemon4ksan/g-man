// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tf2

import (
	"context"
	"encoding/binary"

	pb "github.com/lemon4ksan/g-man/pkg/tf2/protobuf"
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
