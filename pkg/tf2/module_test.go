// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tf2

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/lemon4ksan/g-man/pkg/jobs"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/tf2"
	bm "github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/apps"
	"github.com/lemon4ksan/g-man/test/module"
	"google.golang.org/protobuf/proto"
)

const (
	Item_Scrap = 5000
	Item_Key   = 5021
)

type mockCoordinator struct {
	bm.Base
	lastSendMsgType uint32
	lastSendPayload []byte

	onCallRaw func(msgType uint32, payload []byte) (*protocol.GCPacket, error)
}

func (m *mockCoordinator) Send(ctx context.Context, appID uint32, msgType uint32, msg proto.Message) error {
	m.lastSendMsgType = msgType
	m.lastSendPayload, _ = proto.Marshal(msg)
	return nil
}

func (m *mockCoordinator) SendRaw(ctx context.Context, appID uint32, msgType uint32, payload []byte) error {
	m.lastSendMsgType = msgType
	m.lastSendPayload = payload
	return nil
}

func (m *mockCoordinator) Call(ctx context.Context, appID uint32, msgType uint32, msg proto.Message, cb jobs.Callback[*protocol.GCPacket]) error {
	return nil
}

func (m *mockCoordinator) CallRaw(ctx context.Context, appID uint32, msgType uint32, payload []byte, cb jobs.Callback[*protocol.GCPacket]) error {
	m.lastSendMsgType = msgType
	m.lastSendPayload = payload

	if m.onCallRaw != nil {
		resp, err := m.onCallRaw(msgType, payload)
		go cb(resp, err)
		return nil
	}
	return errors.New("onCallRaw not configured")
}

func setupTF2(t *testing.T) (*TF2, *module.InitContext, *mockCoordinator) {
	t.Helper()
	ictx := module.NewInitContext()

	mCoord := &mockCoordinator{}
	ictx.SetModule("gc", mCoord)
	ictx.SetModule("apps", apps.New())

	tf := New()
	if err := tf.Init(ictx); err != nil {
		t.Fatalf("failed to init TF2: %v", err)
	}

	return tf, ictx, mCoord
}

func createItemPayload(id uint64, defIndex uint32) []byte {
	b, _ := proto.Marshal(&pb.CSOEconItem{
		Id:       proto.Uint64(id),
		DefIndex: proto.Uint32(defIndex),
	})
	return b
}

func TestTF2_SOCacheEvents(t *testing.T) {
	tf, ictx, _ := setupTF2(t)

	subLoaded := ictx.Bus().Subscribe(&BackpackLoadedEvent{})
	subAcquired := ictx.Bus().Subscribe(&ItemAcquiredEvent{})

	t.Run("Initial Load (CacheSubscribed)", func(t *testing.T) {
		msg := &pb.CMsgSOCacheSubscribed{
			Version: proto.Uint64(100),
			Objects: []*pb.CMsgSOCacheSubscribed_SubscribedType{
				{
					TypeId: proto.Int32(SOTypeEconItem),
					ObjectData: [][]byte{
						createItemPayload(100, Item_Key),
						createItemPayload(200, Item_Scrap),
					},
				},
			},
		}

		payload, _ := proto.Marshal(msg)
		tf.cache.HandleSubscribed(&protocol.GCPacket{Payload: payload}, tf.Logger, tf.Bus)

		select {
		case ev := <-subLoaded.C():
			loadedEv := ev.(*BackpackLoadedEvent)
			if loadedEv.Count != 2 {
				t.Errorf("expected 2 items, got %d", loadedEv.Count)
			}
			if len(tf.cache.GetItems()) != 2 {
				t.Errorf("cache size mismatch")
			}
		case <-time.After(1 * time.Second):
			t.Fatal("BackpackLoadedEvent not received")
		}
	})

	t.Run("Item Acquired (SOUpdate Create)", func(t *testing.T) {
		msg := &pb.CMsgSOSingleObject{
			TypeId:     proto.Int32(SOTypeEconItem),
			ObjectData: createItemPayload(300, Item_Scrap),
			Version:    proto.Uint64(101),
		}

		payload, _ := proto.Marshal(msg)

		pkt := &protocol.GCPacket{
			MsgType: uint32(pb.ESOMsg_k_ESOMsg_Create),
			Payload: payload,
		}

		tf.cache.HandleSOUpdate(pkt, tf.Logger, tf.Bus)

		select {
		case ev := <-subAcquired.C():
			acqEv := ev.(*ItemAcquiredEvent)
			if acqEv.Item.ID != 300 {
				t.Errorf("expected item ID 300, got %d", acqEv.Item.ID)
			}
		case <-time.After(1 * time.Second):
			t.Fatal("ItemAcquiredEvent not received")
		}
	})
}

func TestTF2_Crafting(t *testing.T) {
	tf, _, mCoord := setupTF2(t)

	mCoord.onCallRaw = func(msgType uint32, p []byte) (*protocol.GCPacket, error) {
		if msgType != uint32(pb.EGCItemMsg_k_EMsgGCCraft) {
			return nil, errors.New("unexpected msg type")
		}

		resp := new(bytes.Buffer)
		binary.Write(resp, binary.LittleEndian, int16(-1))   // Blueprint (Custom)
		binary.Write(resp, binary.LittleEndian, uint32(0))   // Unknown
		binary.Write(resp, binary.LittleEndian, uint16(1))   // Count (1 new item)
		binary.Write(resp, binary.LittleEndian, uint64(777)) // New Item ID

		return &protocol.GCPacket{Payload: resp.Bytes()}, nil
	}

	t.Run("Successful Craft (Synchronous)", func(t *testing.T) {
		items := []uint64{100, 200, 300}

		result, err := tf.Craft(context.Background(), items, -1)
		if err != nil {
			t.Fatalf("Craft failed: %v", err)
		}

		if len(result) != 1 || result[0] != 777 {
			t.Errorf("expected new item [777], got %v", result)
		}

		sentBody := mCoord.lastSendPayload
		reader := bytes.NewReader(sentBody)

		var recipe int16
		var count int16
		binary.Read(reader, binary.LittleEndian, &recipe)
		binary.Read(reader, binary.LittleEndian, &count)

		if recipe != -1 || count != 3 {
			t.Errorf("invalid binary header sent to GC: recipe=%d, count=%d", recipe, count)
		}
	})
}

func TestTF2_Actions_SendRaw(t *testing.T) {
	tf, _, mCoord := setupTF2(t)

	t.Run("RemoveItemName", func(t *testing.T) {
		err := tf.RemoveItemName(context.Background(), 999)
		if err != nil {
			t.Fatalf("RemoveItemName failed: %v", err)
		}

		if mCoord.lastSendMsgType != uint32(pb.EGCItemMsg_k_EMsgGCRemoveItemName) {
			t.Errorf("expected msg type %d, got %d", pb.EGCItemMsg_k_EMsgGCRemoveItemName, mCoord.lastSendMsgType)
		}

		var sentID uint64
		binary.Read(bytes.NewReader(mCoord.lastSendPayload), binary.LittleEndian, &sentID)

		if sentID != 999 {
			t.Errorf("expected to send item ID 999, sent %d", sentID)
		}
	})
}
