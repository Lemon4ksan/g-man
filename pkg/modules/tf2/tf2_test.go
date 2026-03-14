// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tf2

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/g-man/pkg/jobs"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/modules/coordinator"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/gc"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	tf2pb "github.com/lemon4ksan/g-man/pkg/tf2/protobuf"
	"google.golang.org/protobuf/proto"
)

type mockCoordinator struct {
	mu           sync.Mutex
	sendCalls    map[uint32]proto.Message
	sendRawCalls map[uint32][]byte
	callRawCalls map[uint32]struct {
		Payload  []byte
		Callback jobs.Callback[*gc.Packet]
	}
}

func (m *mockCoordinator) Init(init steam.InitContext) error {
	panic("unimplemented")
}
func (m *mockCoordinator) Name() string {
	panic("unimplemented")
}
func (m *mockCoordinator) Start(ctx context.Context) error {
	panic("unimplemented")
}

func newMockCoordinator() *mockCoordinator {
	return &mockCoordinator{
		sendCalls:    make(map[uint32]proto.Message),
		sendRawCalls: make(map[uint32][]byte),
		callRawCalls: make(map[uint32]struct {
			Payload  []byte
			Callback jobs.Callback[*gc.Packet]
		}),
	}
}

func (m *mockCoordinator) Send(ctx context.Context, appID uint32, msgType uint32, msg proto.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendCalls[msgType] = msg
	return nil
}

func (m *mockCoordinator) SendRaw(ctx context.Context, appID uint32, msgType uint32, payload []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendRawCalls[msgType] = payload
	return nil
}

func (m *mockCoordinator) Call(ctx context.Context, appID uint32, msgType uint32, msg proto.Message, cb jobs.Callback[*gc.Packet]) error {
	return errors.New("unimplemented")
}

func (m *mockCoordinator) CallRaw(ctx context.Context, appID uint32, msgType uint32, payload []byte, cb jobs.Callback[*gc.Packet]) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callRawCalls[msgType] = struct {
		Payload  []byte
		Callback jobs.Callback[*gc.Packet]
	}{Payload: payload, Callback: cb}

	if cb != nil {
		mockResponse := &gc.Packet{
			Payload: []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		}
		go cb(mockResponse, nil)
	}

	return nil
}

type mockSteamClient struct {
	b     *bus.Bus
	gcMod *mockCoordinator
}

func (m *mockSteamClient) Bus() *bus.Bus   { return m.b }
func (m *mockSteamClient) SteamID() uint64 { return 12345 }
func (m *mockSteamClient) GetModule(name string) steam.Module {
	if name == coordinator.ModuleName {
		return m.gcMod
	}
	return nil
}

func (m *mockSteamClient) Logger() log.Logger {
	panic("unimplemented")
}
func (m *mockSteamClient) Proto() api.LegacyRequester {
	panic("unimplemented")
}
func (m *mockSteamClient) RegisterPacketHandler(eMsg protocol.EMsg, handler socket.Handler) {
	panic("unimplemented")
}
func (m *mockSteamClient) RegisterServiceHandler(method string, handler socket.Handler) {
	panic("unimplemented")
}
func (m *mockSteamClient) Unified() api.UnifiedRequester {
	panic("unimplemented")
}
func (m *mockSteamClient) UnregisterPacketHandler(eMsg protocol.EMsg) {
	panic("unimplemented")
}
func (m *mockSteamClient) UnregisterServiceHandler(method string) {
	panic("unimplemented")
}
func (m *mockSteamClient) WebAPI() api.WebAPIRequester {
	panic("unimplemented")
}

func createSOItem(id uint64, defIndex uint32) []byte {
	item := &tf2pb.CSOEconItem{
		Id:       proto.Uint64(id),
		DefIndex: proto.Uint32(defIndex),
	}
	data, _ := proto.Marshal(item)
	return data
}

func TestTF2_GCConnection(t *testing.T) {
	mockGC := newMockCoordinator()
	client := &mockSteamClient{b: bus.NewBus(), gcMod: mockGC}

	tf2 := New(log.Discard)
	tf2.bus = bus.NewBus()
	_ = tf2.Init(client)
	_ = tf2.Start(context.Background())

	tf2.PlayGame(context.Background())

	time.Sleep(10 * time.Millisecond)

	if tf2.state.Load() != int32(GCConnecting) {
		t.Errorf("expected state Connecting, got %d", tf2.state.Load())
	}

	mockGC.mu.Lock()
	if _, ok := mockGC.sendCalls[uint32(tf2pb.EGCBaseClientMsg_k_EMsgGCClientHello)]; !ok {
		t.Error("expected ClientHello to be sent")
	}
	mockGC.mu.Unlock()

	tf2.handleWelcome(nil)

	if tf2.state.Load() != int32(GCConnected) {
		t.Errorf("expected state Connected, got %d", tf2.state.Load())
	}

	tf2.handleGoodbye(nil)

	if tf2.state.Load() != int32(GCDisconnected) {
		t.Errorf("expected state Disconnected, got %d", tf2.state.Load())
	}
}

func TestTF2_BackpackHandling(t *testing.T) {
	mockGC := newMockCoordinator()
	b := bus.NewBus()
	client := &mockSteamClient{b: b, gcMod: mockGC}

	tf2 := New(log.Discard)
	tf2.ctx = t.Context()
	_ = tf2.Init(client)

	subLoaded := b.Subscribe(&BackpackLoadedEvent{})
	subAcquired := b.Subscribe(&ItemAcquiredEvent{})
	subRemoved := b.Subscribe(&ItemRemovedEvent{})

	// 1. Загрузка инвентаря
	subscribedMsg := &tf2pb.CMsgSOCacheSubscribed{
		Objects: []*tf2pb.CMsgSOCacheSubscribed_SubscribedType{
			{
				TypeId: proto.Int32(1), // Item
				ObjectData: [][]byte{
					createSOItem(100, 5021), // Key
					createSOItem(200, 5002), // Refined
				},
			},
		},
	}
	payload, _ := proto.Marshal(subscribedMsg)
	tf2.backpack.HandleSubscribed(&gc.Packet{Payload: payload})

	select {
	case <-subLoaded.C():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for BackpackLoadedEvent")
	}

	if !tf2.backpack.IsLoaded() || len(tf2.backpack.items) != 2 {
		t.Errorf("expected backpack to be loaded with 2 items, got %d", len(tf2.backpack.items))
	}

	// 2. Добавление предмета
	createMsg := &tf2pb.CMsgSOSingleObject{
		TypeId:     proto.Int32(1),
		ObjectData: createSOItem(300, 5001), // Reclaimed
	}
	payload, _ = proto.Marshal(createMsg)
	tf2.backpack.HandleCreate(&gc.Packet{Payload: payload})

	select {
	case ev := <-subAcquired.C():
		if ev.(*ItemAcquiredEvent).Item.ID != 300 {
			t.Error("invalid item in ItemAcquiredEvent")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for ItemAcquiredEvent")
	}

	if len(tf2.backpack.items) != 3 {
		t.Errorf("expected 3 items in backpack, got %d", len(tf2.backpack.items))
	}

	// 3. Удаление предмета
	destroyMsg := &tf2pb.CMsgSOSingleObject{
		TypeId:     proto.Int32(1),
		ObjectData: createSOItem(100, 5021), // Удаляем ключ
	}
	payload, _ = proto.Marshal(destroyMsg)
	tf2.backpack.HandleDestroy(&gc.Packet{Payload: payload})

	select {
	case ev := <-subRemoved.C():
		if ev.(*ItemRemovedEvent).ItemID != 100 {
			t.Error("invalid item ID in ItemRemovedEvent")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for ItemRemovedEvent")
	}

	if len(tf2.backpack.items) != 2 {
		t.Errorf("expected 2 items in backpack after removal, got %d", len(tf2.backpack.items))
	}
}

func TestTF2_Craft(t *testing.T) {
	mockGC := newMockCoordinator()
	client := &mockSteamClient{b: bus.NewBus(), gcMod: mockGC}
	tf2 := New(log.Discard)
	tf2.bus = bus.NewBus()
	tf2.ctx = t.Context()
	_ = tf2.Init(client)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// 1. Отправляем запрос на крафт
	go func() {
		_, _ = tf2.Craft(ctx, []uint64{100, 200}, -1)
	}()

	// Ждем, пока вызов дойдет до мока
	time.Sleep(20 * time.Millisecond)

	// 2. Проверяем CallRaw
	mockGC.mu.Lock()
	call, ok := mockGC.callRawCalls[uint32(tf2pb.EGCItemMsg_k_EMsgGCCraft)]
	mockGC.mu.Unlock()

	if !ok {
		t.Fatal("Craft did not call gc.CallRaw")
	}

	// Проверяем payload: [recipe(2b)] [count(2b)] [id1(8b)] [id2(8b)]
	expectedSize := 4 + 8*2
	if len(call.Payload) != expectedSize {
		t.Fatalf("expected payload size %d, got %d", expectedSize, len(call.Payload))
	}

	// 3. Имитируем ответ от GC, вызывая коллбэк
	if call.Callback == nil {
		t.Fatal("expected a non-nil callback")
	}

	// Формируем фейковый ответ от GC
	respPayload := new(bytes.Buffer)
	binary.Write(respPayload, binary.LittleEndian, int16(-1))   // Blueprint
	binary.Write(respPayload, binary.LittleEndian, uint32(0))   // Unknown
	binary.Write(respPayload, binary.LittleEndian, uint16(1))   // Count
	binary.Write(respPayload, binary.LittleEndian, uint64(300)) // New Item ID

	call.Callback(&gc.Packet{Payload: respPayload.Bytes()}, nil)

	// Теперь метод Craft должен разблокироваться и вернуть результат
	// Можно добавить проверку возвращаемого значения, если оно вам важно
}

func TestTF2_JobWorker_CraftMetal(t *testing.T) {
	mockGC := newMockCoordinator()
	client := &mockSteamClient{b: bus.NewBus(), gcMod: mockGC}
	tf2 := New(log.Discard)
	tf2.bus = bus.NewBus()
	tf2.ctx = t.Context()
	_ = tf2.Init(client)
	_ = tf2.Start(context.Background())

	tf2.backpack.items[1] = &Item{ID: 1, DefIndex: 5000} // Scrap
	tf2.backpack.items[2] = &Item{ID: 2, DefIndex: 5000}
	tf2.backpack.items[3] = &Item{ID: 3, DefIndex: 5000}

	tf2.state.Store(int32(GCConnected))
	resultCh := tf2.EnqueueCombineMetal(5000)

	err := <-resultCh
	if err != nil {
		t.Fatalf("EnqueueCombineMetal failed: %v", err)
	}

	mockGC.mu.Lock()
	call, ok := mockGC.callRawCalls[uint32(tf2pb.EGCItemMsg_k_EMsgGCCraft)]
	mockGC.mu.Unlock()

	if !ok {
		t.Fatal("JobWorker did not call Craft")
	}

	payload := call.Payload
	if !bytes.Contains(payload, []byte{1, 0, 0, 0, 0, 0, 0, 0}) || !bytes.Contains(payload, []byte{2, 0, 0, 0, 0, 0, 0, 0}) || !bytes.Contains(payload, []byte{3, 0, 0, 0, 0, 0, 0, 0}) {
		t.Error("Craft payload does not contain expected item IDs")
	}
}
