// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tf2

import (
	"context"
	"encoding/binary"

	pb "github.com/lemon4ksan/g-man/pkg/tf2/protobuf"
	gc "github.com/lemon4ksan/g-man/pkg/modules/coordinator/protocol"
)

// Craft sends a crafting request.
func (t *TF2) Craft(ctx context.Context, items []uint64, recipe int16) ([]uint64, error) {
	// Format: [Recipe(int16)] [Count(int16)] [ItemID(uint64)]...
	payload := make([]byte, 4+(8*len(items)))
	binary.LittleEndian.PutUint16(payload[0:], uint16(recipe))
	binary.LittleEndian.PutUint16(payload[2:], uint16(len(items)))

	for i, id := range items {
		offset := 4 + (i * 8)
		binary.LittleEndian.PutUint64(payload[offset:], id)
	}

	resCh := make(chan []uint64, 1)
	errCh := make(chan error, 1)

	err := t.gc.CallRaw(ctx, AppID, uint32(pb.EGCItemMsg_k_EMsgGCCraft), payload, func(pkt *gc.Packet, err error) {
		if err != nil {
			errCh <- err
			return
		}
		newItems := parseCraftResponse(pkt.Payload)
		resCh <- newItems
	})

	if err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-errCh:
		return nil, err
	case items := <-resCh:
		return items, nil
	}
}

func parseCraftResponse(payload []byte) []uint64 {
	// [BlueprintID(int16)] [Unknown(uint32)] [Count(uint16)] [IDs(uint64...)]
	if len(payload) < 8 {
		return nil
	}

	count := int(binary.LittleEndian.Uint16(payload[6:]))
	items := make([]uint64, 0, count)

	for i := range count {
		offset := 8 + (i * 8)
		if len(payload) < offset+8 {
			break
		}
		items = append(items, binary.LittleEndian.Uint64(payload[offset:]))
	}
	return items
}

func (t *TF2) handleCraftResponse(pkt *gc.Packet) {
	// Broadcast event for listeners (logs, analytics)
	// The specific job callback handles the logic flow.
	items := parseCraftResponse(pkt.Payload)
	if len(items) > 0 || len(pkt.Payload) >= 2 {
		blueprint := int16(binary.LittleEndian.Uint16(pkt.Payload[0:]))
		t.bus.Publish(&CraftResponseEvent{
			BlueprintID:  blueprint,
			CreatedItems: items,
		})
	}
}
