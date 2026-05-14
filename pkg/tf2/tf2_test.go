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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/jobs"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/tf2"
	bm "github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/apps"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/gc"
	"github.com/lemon4ksan/g-man/pkg/tf2/schema"
	"github.com/lemon4ksan/g-man/pkg/tf2/schema/manager"
	"github.com/lemon4ksan/g-man/test/module"
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

type mockSchemaProvider struct {
	schema *schema.Schema
}

func (m *mockSchemaProvider) Init(init bm.InitContext) error {
	return nil
}

func (m *mockSchemaProvider) Name() string {
	return "mockSchemaProvider"
}

func (m *mockSchemaProvider) Start(ctx context.Context) error {
	return nil
}

func (m *mockSchemaProvider) Get() *schema.Schema {
	return m.schema
}

type mockAppsProvider struct{}

func (m *mockAppsProvider) Init(init bm.InitContext) error {
	return nil
}

func (m *mockAppsProvider) Name() string {
	return "mockAppsProvider"
}

func (m *mockAppsProvider) Start(ctx context.Context) error {
	return nil
}

func (m *mockAppsProvider) PlayGames(ctx context.Context, appIDs []uint32, forceKick bool) error {
	return nil
}

func (m *mockCoordinator) Send(ctx context.Context, appID, msgType uint32, msg proto.Message) error {
	m.lastSendMsgType = msgType
	m.lastSendPayload, _ = proto.Marshal(msg)

	return nil
}

func (m *mockCoordinator) SendRaw(ctx context.Context, appID, msgType uint32, payload []byte) error {
	m.lastSendMsgType = msgType
	m.lastSendPayload = payload

	return nil
}

func (m *mockCoordinator) Call(
	ctx context.Context,
	appID, msgType uint32,
	msg proto.Message,
	cb jobs.Callback[*protocol.GCPacket],
) error {
	return nil
}

func (m *mockCoordinator) CallRaw(
	ctx context.Context,
	appID, msgType uint32,
	payload []byte,
	cb jobs.Callback[*protocol.GCPacket],
) error {
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
	ictx.SetModule(gc.ModuleName, mCoord)
	ictx.SetModule(apps.ModuleName, &mockAppsProvider{})

	mSchema := &mockSchemaProvider{schema: &schema.Schema{}}
	ictx.SetModule(manager.ModuleName, mSchema)

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
	_, ictx, _ := setupTF2(t)

	subLoaded := ictx.Bus().Subscribe(&BackpackLoadedEvent{})
	subAcquired := ictx.Bus().Subscribe(&ItemAcquiredEvent{})

	t.Run("Initial Load via Bus", func(t *testing.T) {
		msg := &pb.CMsgSOCacheSubscribed{
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
		ictx.Bus().Publish(&gc.MessageEvent{
			Packet: &protocol.GCPacket{
				AppID:   AppID,
				MsgType: uint32(pb.ESOMsg_k_ESOMsg_CacheSubscribed),
				Payload: payload,
			},
		})

		select {
		case ev := <-subLoaded.C():
			loadedEv := ev.(*BackpackLoadedEvent)
			assert.Equal(t, 2, loadedEv.Count)
		case <-time.After(1 * time.Second):
			t.Fatal("BackpackLoadedEvent not received")
		}
	})

	t.Run("Item Acquired via Bus", func(t *testing.T) {
		msg := &pb.CMsgSOSingleObject{
			TypeId:     proto.Int32(SOTypeEconItem),
			ObjectData: createItemPayload(300, Item_Scrap),
		}

		payload, _ := proto.Marshal(msg)
		ictx.Bus().Publish(&gc.MessageEvent{
			Packet: &protocol.GCPacket{
				AppID:   AppID,
				MsgType: uint32(pb.ESOMsg_k_ESOMsg_Create),
				Payload: payload,
			},
		})

		select {
		case ev := <-subAcquired.C():
			acqEv := ev.(*ItemAcquiredEvent)
			assert.Equal(t, uint64(300), acqEv.Item.ID)
		case <-time.After(1 * time.Second):
			t.Fatal("ItemAcquiredEvent not received")
		}
	})
}

func TestTF2_Lifecycle(t *testing.T) {
	tf, ictx, mCoord := setupTF2(t)
	subConn := ictx.Bus().Subscribe(&ConnectedEvent{})
	subDisc := ictx.Bus().Subscribe(&DisconnectedEvent{})

	t.Run("StartAuthed and Hello", func(t *testing.T) {
		err := tf.StartAuthed(context.Background(), nil)
		require.NoError(t, err)

		// Should have sent Hello (it's in a goroutine, so wait a bit)
		assert.Eventually(t, func() bool {
			return mCoord.lastSendMsgType == uint32(pb.EGCBaseClientMsg_k_EMsgGCClientHello)
		}, 1*time.Second, 10*time.Millisecond)
	})

	t.Run("Handle Welcome", func(t *testing.T) {
		msg := &pb.CMsgClientWelcome{Version: proto.Uint32(1)}
		payload, _ := proto.Marshal(msg)

		ictx.Bus().Publish(&gc.MessageEvent{
			Packet: &protocol.GCPacket{
				AppID:   AppID,
				MsgType: uint32(pb.EGCBaseClientMsg_k_EMsgGCClientWelcome),
				Payload: payload,
			},
		})

		select {
		case <-subConn.C():
			assert.Equal(t, int32(Connected), tf.state.Load())
		case <-time.After(1 * time.Second):
			t.Fatal("GCConnectedEvent not received")
		}
	})

	t.Run("Handle Goodbye", func(t *testing.T) {
		ictx.Bus().Publish(&gc.MessageEvent{
			Packet: &protocol.GCPacket{
				AppID:   AppID,
				MsgType: uint32(pb.EGCBaseClientMsg_k_EMsgGCClientGoodbye),
			},
		})

		select {
		case <-subDisc.C():
			assert.Equal(t, int32(Connecting), tf.state.Load())
		case <-time.After(1 * time.Second):
			t.Fatal("GCDisconnectedEvent not received")
		}
	})

	t.Run("Close", func(t *testing.T) {
		err := tf.Close()
		require.NoError(t, err)
		assert.Equal(t, int32(Disconnected), tf.state.Load())
	})
}

func TestTF2_AcknowledgeAll(t *testing.T) {
	tf, _, mCoord := setupTF2(t)

	// Seed 2 unacknowledged items (Position 0 or Inventory bit 30 set)
	msg := &pb.CMsgSOCacheSubscribed{
		Objects: []*pb.CMsgSOCacheSubscribed_SubscribedType{
			{
				TypeId: proto.Int32(SOTypeEconItem),
				ObjectData: [][]byte{
					createItemPayload_Full(1, Item_Scrap, 1<<30), // New bit set
					createItemPayload_Full(2, Item_Scrap, 0),     // Pos 0
					createItemPayload_Full(3, Item_Scrap, 5),     // Normal
				},
			},
		},
	}
	payload, _ := proto.Marshal(msg)
	tf.cache.handleSubscribed(&protocol.GCPacket{Payload: payload})

	err := tf.AcknowledgeAll(context.Background())
	require.NoError(t, err)

	// Should have sent 1 MoveItems call (batch)
	assert.Equal(t, uint32(pb.EGCItemMsg_k_EMsgGCSetItemPositions), mCoord.lastSendMsgType)
}

func TestTF2_AdvancedActions(t *testing.T) {
	tf, _, mCoord := setupTF2(t)
	ctx := context.Background()

	t.Run("SetUnusualEffectOffset", func(t *testing.T) {
		err := tf.SetUnusualEffectOffset(ctx, 123, 1.5)
		require.NoError(t, err)
		assert.Equal(t, uint32(pb.EGCItemMsg_k_EMsgGCSetItemEffectVerticalOffset), mCoord.lastSendMsgType)
	})

	t.Run("TransferStrangeCount", func(t *testing.T) {
		err := tf.TransferStrangeCount(ctx, 1, 2, 3)
		require.NoError(t, err)
		assert.Equal(t, uint32(pb.EGCItemMsg_k_EMsgGCApplyStrangeCountTransfer), mCoord.lastSendMsgType)
	})

	t.Run("RemoveKillstreak", func(t *testing.T) {
		err := tf.RemoveKillstreak(ctx, 456)
		require.NoError(t, err)
		assert.Equal(t, uint32(pb.EGCItemMsg_k_EMsgGCRemoveKillStreak), mCoord.lastSendMsgType)
	})

	t.Run("ReportPlayer", func(t *testing.T) {
		reason := pb.CMsgGC_ReportPlayer_kReason_CHEATING
		err := tf.ReportPlayer(ctx, 777, &reason)
		require.NoError(t, err)
		assert.Equal(t, uint32(pb.ETFGCMsg_k_EMsgGC_ReportPlayer), mCoord.lastSendMsgType)
	})
}

func TestTF2_SOCache_Metadata(t *testing.T) {
	tf, _, _ := setupTF2(t)

	t.Run("Account Metadata Update", func(t *testing.T) {
		accMsg := &pb.CSOEconGameAccountClient{
			AdditionalBackpackSlots: proto.Uint32(100),
			TrialAccount:            proto.Bool(false),
			CompetitiveAccess:       proto.Bool(true),
			TradeBanExpiration:      proto.Uint32(123456),
		}
		data, _ := proto.Marshal(accMsg)

		tf.cache.processObject(SOTypeEconGameAccountClient, data, false, nil)

		assert.True(t, tf.cache.IsPremium())
		assert.Equal(t, 400, tf.cache.GetMaxSlots()) // 300 base + 100 additional
		assert.True(t, tf.cache.HasCompetitiveAccess())
		assert.Equal(t, uint32(123456), tf.cache.GetTradeBanExpiration())
	})

	t.Run("MMR Update", func(t *testing.T) {
		ratingMsg := &pb.CSOTFRatingData{
			RatingType:    proto.Int32(2), // 6v6
			RatingPrimary: proto.Uint32(1500),
		}
		data, _ := proto.Marshal(ratingMsg)

		tf.cache.processObject(SOTypeTFRatingData, data, false, nil)

		assert.Equal(t, uint32(1500), tf.cache.GetMMR(2))
	})
}

func createItemPayload_Full(id uint64, defIndex, inventory uint32) []byte {
	b, _ := proto.Marshal(&pb.CSOEconItem{
		Id:        proto.Uint64(id),
		DefIndex:  proto.Uint32(defIndex),
		Inventory: proto.Uint32(inventory),
	})

	return b
}

func TestTF2_Crafting(t *testing.T) {
	tf, _, mCoord := setupTF2(t)

	mCoord.onCallRaw = func(msgType uint32, p []byte) (*protocol.GCPacket, error) {
		if msgType != uint32(pb.EGCItemMsg_k_EMsgGCCraft) {
			return nil, errors.New("unexpected msg type")
		}

		resp := new(bytes.Buffer)
		_ = binary.Write(resp, binary.LittleEndian, int16(-1))   // Blueprint (Custom)
		_ = binary.Write(resp, binary.LittleEndian, uint32(0))   // Unknown
		_ = binary.Write(resp, binary.LittleEndian, uint16(1))   // Count (1 new item)
		_ = binary.Write(resp, binary.LittleEndian, uint64(777)) // New Item ID

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

		var (
			recipe int16
			count  int16
		)

		_ = binary.Read(reader, binary.LittleEndian, &recipe)
		_ = binary.Read(reader, binary.LittleEndian, &count)

		if recipe != -1 || count != 3 {
			t.Errorf("invalid binary header sent to GC: recipe=%d, count=%d", recipe, count)
		}
	})
}

func TestTF2_CraftResponse(t *testing.T) {
	tf, ictx, _ := setupTF2(t)
	sub := ictx.Bus().Subscribe(&CraftResponseEvent{})

	t.Run("Handle Successful Response", func(t *testing.T) {
		resp := new(bytes.Buffer)
		_ = binary.Write(resp, binary.LittleEndian, int16(3))    // Blueprint SmeltWeapons
		_ = binary.Write(resp, binary.LittleEndian, uint32(0))   // Unknown
		_ = binary.Write(resp, binary.LittleEndian, uint16(1))   // Count
		_ = binary.Write(resp, binary.LittleEndian, uint64(555)) // New Item

		tf.handleCraftResponse(&protocol.GCPacket{Payload: resp.Bytes()})

		select {
		case ev := <-sub.C():
			craftEv := ev.(*CraftResponseEvent)
			assert.Equal(t, uint16(3), craftEv.BlueprintID)
			assert.Equal(t, []uint64{555}, craftEv.CreatedItems)
		case <-time.After(1 * time.Second):
			t.Fatal("CraftResponseEvent not received")
		}
	})

	t.Run("Empty Response", func(t *testing.T) {
		tf.handleCraftResponse(&protocol.GCPacket{Payload: []byte{}})
		// Should not panic and not publish event
		select {
		case <-sub.C():
			t.Error("Did not expect event for empty payload")
		default:
		}
	})
}

func TestTF2_ParseCraftResponse_EdgeCases(t *testing.T) {
	t.Run("Short Payload", func(t *testing.T) {
		res := parseCraftResponse([]byte{1, 2, 3})
		assert.Nil(t, res)
	})

	t.Run("Incomplete Item List", func(t *testing.T) {
		resp := new(bytes.Buffer)
		_ = binary.Write(resp, binary.LittleEndian, int16(3))
		_ = binary.Write(resp, binary.LittleEndian, uint32(0))
		_ = binary.Write(resp, binary.LittleEndian, uint16(5))   // Claims 5 items
		_ = binary.Write(resp, binary.LittleEndian, uint64(111)) // Only provides 1

		res := parseCraftResponse(resp.Bytes())
		assert.Equal(t, 1, len(res))
		assert.Equal(t, uint64(111), res[0])
	})
}

func TestTF2_HandleSchemaUpdate(t *testing.T) {
	tf, ictx, _ := setupTF2(t)
	sub := ictx.Bus().Subscribe(&schema.UpdateRequestedEvent{})

	msg := &pb.CMsgUpdateItemSchema{
		ItemSchemaVersion: proto.Uint32(1234),
		ItemsGameUrl:      proto.String("http://example.com/items_game.txt"),
	}
	payload, _ := proto.Marshal(msg)

	tf.handleSchemaUpdate(&protocol.GCPacket{
		MsgType: uint32(pb.EGCItemMsg_k_EMsgGCUpdateItemSchema),
		Payload: payload,
	})

	select {
	case ev := <-sub.C():
		updateEv := ev.(*schema.UpdateRequestedEvent)
		assert.Equal(t, uint32(1234), updateEv.Version)
		assert.Equal(t, "http://example.com/items_game.txt", updateEv.ItemsGameURL)
	case <-time.After(1 * time.Second):
		t.Fatal("UpdateRequestedEvent not received")
	}
}
