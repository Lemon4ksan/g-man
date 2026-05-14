// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tf2

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/tf2"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/tf2/schema"
)

func TestSOCache_Lifecycle(t *testing.T) {
	tf, _, _ := setupTF2(t)
	cache := tf.Cache()

	t.Run("Create and GetItem", func(t *testing.T) {
		itemBytes := createItemPayload(100, Item_Scrap)

		msg := &pb.CMsgSOSingleObject{
			TypeId:     proto.Int32(SOTypeEconItem),
			ObjectData: itemBytes,
		}
		payload, _ := proto.Marshal(msg)
		pkt := &protocol.GCPacket{MsgType: uint32(pb.ESOMsg_k_ESOMsg_Create), Payload: payload}

		cache.handleSOUpdate(pkt)

		item, ok := cache.GetItem(100)
		require.True(t, ok)
		assert.Equal(t, uint64(100), item.ID)
		assert.Equal(t, uint32(Item_Scrap), item.DefIndex)
	})

	t.Run("Update", func(t *testing.T) {
		// Update item 100 to defindex Key
		itemBytes := createItemPayload(100, Item_Key)

		msg := &pb.CMsgSOSingleObject{
			TypeId:     proto.Int32(SOTypeEconItem),
			ObjectData: itemBytes,
		}
		payload, _ := proto.Marshal(msg)
		pkt := &protocol.GCPacket{MsgType: uint32(pb.ESOMsg_k_ESOMsg_Update), Payload: payload}

		cache.handleSOUpdate(pkt)

		item, ok := cache.GetItem(100)
		require.True(t, ok)
		assert.Equal(t, uint32(Item_Key), item.DefIndex)
	})

	t.Run("UpdateMultiple", func(t *testing.T) {
		msg := &pb.CMsgSOMultipleObjects{
			Objects: []*pb.CMsgSOMultipleObjects_SingleObject{
				{
					TypeId:     proto.Int32(SOTypeEconItem),
					ObjectData: createItemPayload(100, Item_Scrap), // change back to scrap
				},
				{
					TypeId:     proto.Int32(SOTypeEconItem),
					ObjectData: createItemPayload(200, Item_Key), // add new item
				},
			},
		}
		payload, _ := proto.Marshal(msg)
		pkt := &protocol.GCPacket{MsgType: uint32(pb.ESOMsg_k_ESOMsg_UpdateMultiple), Payload: payload}

		cache.handleSOUpdate(pkt)

		assert.Equal(t, 2, len(cache.GetItems()))
		assert.Equal(t, 1, cache.GetMetalCount(Item_Scrap))
	})

	t.Run("Destroy", func(t *testing.T) {
		msg := &pb.CMsgSOSingleObject{
			TypeId:     proto.Int32(SOTypeEconItem),
			ObjectData: createItemPayload(100, Item_Scrap),
		}
		payload, _ := proto.Marshal(msg)
		pkt := &protocol.GCPacket{MsgType: uint32(pb.ESOMsg_k_ESOMsg_Destroy), Payload: payload}

		cache.handleSOUpdate(pkt)

		_, ok := cache.GetItem(100)
		assert.False(t, ok)
		assert.Equal(t, 1, len(cache.GetItems())) // item 200 should still exist
	})

	t.Run("CacheCheck and UpToDate", func(t *testing.T) {
		// Just to cover the handlers
		cache.handleSOCacheCheck(context.Background(), &protocol.GCPacket{})
		cache.handleUpToDate(&protocol.GCPacket{})
		// We just want to make sure it doesn't panic and is covered.
	})
}

func TestSOCache_Getters(t *testing.T) {
	tf, _, _ := setupTF2(t)
	cache := tf.Cache()

	// Seed cache
	msg := &pb.CMsgSOCacheSubscribed{
		Objects: []*pb.CMsgSOCacheSubscribed_SubscribedType{
			{
				TypeId: proto.Int32(SOTypeEconItem),
				ObjectData: [][]byte{
					createItemPayload(1, Item_Scrap),
					createItemPayload(2, Item_Scrap),
					createItemPayload(3, Item_Key),
				},
			},
		},
	}
	payload, _ := proto.Marshal(msg)
	cache.handleSubscribed(&protocol.GCPacket{Payload: payload})

	t.Run("GetMetalCount", func(t *testing.T) {
		assert.Equal(t, 2, cache.GetMetalCount(Item_Scrap))
		assert.Equal(t, 0, cache.GetMetalCount(5001)) // Reclaimed
	})

	t.Run("FindCraftableItems", func(t *testing.T) {
		items := cache.FindCraftableItems(Item_Scrap, 2)
		assert.Equal(t, 2, len(items))

		items = cache.FindCraftableItems(Item_Scrap, 5)
		assert.Equal(t, 2, len(items)) // only 2 available
	})

	t.Run("FindWeaponsByClass", func(t *testing.T) {
		// Mock schema in reality, but without it it will return 0.
		// So we just cover the function call.
		weapons := cache.FindWeaponsByClass("Scout")
		assert.Equal(t, 0, len(weapons))
	})
}

func TestSOCache_ExtraGetters(t *testing.T) {
	tf, _, _ := setupTF2(t)
	cache := tf.Cache()

	// Seed cache
	msg := &pb.CMsgSOCacheSubscribed{
		Objects: []*pb.CMsgSOCacheSubscribed_SubscribedType{
			{
				TypeId: proto.Int32(SOTypeEconItem),
				ObjectData: [][]byte{
					createItemPayload(1, Item_Scrap),
					createItemPayload(2, Item_Key),
				},
			},
		},
	}
	cache.handleSubscribed(createPacket(pb.ESOMsg_k_ESOMsg_CacheSubscribed, msg))

	t.Run("GetMetal", func(t *testing.T) {
		metal := cache.GetMetal(Item_Scrap, 10)
		assert.Equal(t, 1, len(metal))

		if len(metal) > 0 {
			assert.Equal(t, uint64(1), metal[0])
		}
	})

	t.Run("GetMaxSlots", func(t *testing.T) {
		assert.Equal(t, 0, cache.GetMaxSlots()) // Default from empty subscribed msg
	})

	t.Run("GetAssetIDsBySKU", func(t *testing.T) {
		// This depends on Schema mapping which we mock as empty,
		// so it won't find anything, but we cover the code.
		ids := cache.GetAssetIDsBySKU("5000;6", 10)
		assert.Equal(t, 0, len(ids))
	})

	t.Run("IsWeapon", func(t *testing.T) {
		item, _ := cache.GetItem(1)
		// Since schema is empty, it will be false, but won't panic
		assert.False(t, item.IsWeapon(&schema.Schema{}))
	})
}

func createPacket(msgType pb.ESOMsg, msg proto.Message) *protocol.GCPacket {
	b, _ := proto.Marshal(msg)

	return &protocol.GCPacket{
		MsgType: uint32(msgType),
		Payload: b,
	}
}
