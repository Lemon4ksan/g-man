// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tf2

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/modules/coordinator"
	gc "github.com/lemon4ksan/g-man/pkg/modules/coordinator/protocol"
	tf2pb "github.com/lemon4ksan/g-man/pkg/tf2/protobuf"
	"github.com/lemon4ksan/g-man/test"
	"google.golang.org/protobuf/proto"
)

const (
	Item_Scrap = 5000
	Item_Key   = 5021
)

func setupTF2(t *testing.T) (*TF2, *test.MockInitContext, *test.MockCoordinator) {
	t.Helper()
	ictx := test.NewMockInitContext()
	mCoord := test.NewMockCoordinator()
	ictx.SetModule(coordinator.ModuleName, mCoord)

	tf := New(log.Discard)
	if err := tf.Init(ictx); err != nil {
		t.Fatalf("failed to init TF2: %v", err)
	}

	if err := tf.Start(t.Context()); err != nil {
		t.Fatalf("failed to start TF2: %v", err)
	}

	return tf, ictx, mCoord
}

func createItemPayload(id uint64, defIndex uint32) []byte {
	b, _ := proto.Marshal(&tf2pb.CSOEconItem{
		Id:       proto.Uint64(id),
		DefIndex: proto.Uint32(defIndex),
	})
	return b
}

func TestTF2_BackpackEvents(t *testing.T) {
	tf, ictx, _ := setupTF2(t)

	subLoaded := ictx.Bus().Subscribe(&BackpackLoadedEvent{})
	subAcquired := ictx.Bus().Subscribe(&ItemAcquiredEvent{})

	t.Run("Initial Load", func(t *testing.T) {
		msg := &tf2pb.CMsgSOCacheSubscribed{
			Objects: []*tf2pb.CMsgSOCacheSubscribed_SubscribedType{
				{
					TypeId: proto.Int32(1), // Type EconItem
					ObjectData: [][]byte{
						createItemPayload(100, Item_Key),
						createItemPayload(200, Item_Scrap),
					},
				},
			},
		}

		payload, _ := proto.Marshal(msg)
		tf.backpack.HandleSubscribed(&gc.Packet{Payload: payload})

		select {
		case <-subLoaded.C():
			if len(tf.Backpack().Items()) != 2 {
				t.Errorf("expected 2 items, got %d", len(tf.Backpack().Items()))
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("BackpackLoadedEvent not received")
		}
	})

	t.Run("Item Acquired", func(t *testing.T) {
		msg := &tf2pb.CMsgSOSingleObject{
			TypeId:     proto.Int32(1),
			ObjectData: createItemPayload(300, Item_Scrap),
		}
		payload, _ := proto.Marshal(msg)
		tf.backpack.HandleCreate(&gc.Packet{Payload: payload})

		select {
		case ev := <-subAcquired.C():
			if ev.(*ItemAcquiredEvent).Item.ID != 300 {
				t.Error("wrong item ID in event")
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("ItemAcquiredEvent not received")
		}
	})
}

func TestTF2_Crafting_BinaryProtocol(t *testing.T) {
	tf, _, mCoord := setupTF2(t)

	mCoord.OnCallRaw(uint32(tf2pb.EGCItemMsg_k_EMsgGCCraft), func(p []byte) ([]byte, error) {
		resp := new(bytes.Buffer)
		binary.Write(resp, binary.LittleEndian, int16(-1))   // Blueprint
		binary.Write(resp, binary.LittleEndian, uint32(0))   // Unknown
		binary.Write(resp, binary.LittleEndian, uint16(1))   // Count
		binary.Write(resp, binary.LittleEndian, uint64(777)) // New Item ID
		return resp.Bytes(), nil
	})

	t.Run("Successful Craft", func(t *testing.T) {
		items := []uint64{100, 200, 300}
		result, err := tf.Craft(t.Context(), items, -1)

		if err != nil {
			t.Fatalf("Craft failed: %v", err)
		}
		if len(result) != 1 || result[0] != 777 {
			t.Errorf("expected new item 777, got %v", result)
		}

		sentBody := mCoord.GetLastRawCall(uint32(tf2pb.EGCItemMsg_k_EMsgGCCraft))

		var recipe int16
		var count uint16
		reader := bytes.NewReader(sentBody)
		binary.Read(reader, binary.LittleEndian, &recipe)
		binary.Read(reader, binary.LittleEndian, &count)

		if recipe != -1 || count != 3 {
			t.Errorf("invalid binary header: recipe=%d, count=%d", recipe, count)
		}
	})
}

func TestTF2_JobWorker_CombineMetal(t *testing.T) {
	tf, _, mCoord := setupTF2(t)

	tf.backpack.items[1] = &Item{ID: 1, DefIndex: Item_Scrap}
	tf.backpack.items[2] = &Item{ID: 2, DefIndex: Item_Scrap}
	tf.backpack.items[3] = &Item{ID: 3, DefIndex: Item_Scrap}

	tf.state.Store(int32(GCConnected))

	mCoord.OnCallRaw(uint32(tf2pb.EGCItemMsg_k_EMsgGCCraft), func(p []byte) ([]byte, error) {
		resp := make([]byte, 16)
		binary.LittleEndian.PutUint16(resp[0:2], uint16(0xFFFF)) // -1
		binary.LittleEndian.PutUint32(resp[2:6], 0)              // unknown
		binary.LittleEndian.PutUint16(resp[6:8], 1)              // count 1
		binary.LittleEndian.PutUint64(resp[8:16], 777)           // new item ID

		return resp, nil
	})

	t.Run("Queue Metal Combine", func(t *testing.T) {
		done := tf.EnqueueCombineMetal(Item_Scrap)

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("worker failed: %v", err)
			}
		case <-time.After(1 * time.Second):
			t.Fatal("JobWorker did not process metal in time")
		}

		sentPayload := mCoord.GetLastRawCall(uint32(tf2pb.EGCItemMsg_k_EMsgGCCraft))
		if sentPayload == nil {
			t.Fatal("Craft request was never sent to GC")
		}

		if len(sentPayload) != 28 {
			t.Errorf("expected payload length 28, got %d", len(sentPayload))
		}
	})

	t.Run("Not Enough Metal", func(t *testing.T) {
		delete(tf.backpack.items, 3)

		done := tf.EnqueueCombineMetal(Item_Scrap)

		select {
		case err := <-done:
			if err == nil {
				t.Fatal("expected error due to insufficient metal, got nil")
			}
			if err.Error() != "not enough items in backpack to craft" {
				t.Errorf("unexpected error message: %v", err)
			}
		case <-time.After(1 * time.Second):
			t.Fatal("JobWorker did not process failing job in time")
		}
	})
}
