// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apps

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"
	pb "github.com/lemon4ksan/g-man/pkg/steam/protobuf"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
	"google.golang.org/protobuf/proto"
)

type mockLegacyRequester struct {
	mu          sync.Mutex
	calls       map[protocol.EMsg]int
	lastRequest proto.Message
	responseMsg proto.Message
	responseErr error
}

func newMockRequester() *mockLegacyRequester {
	return &mockLegacyRequester{
		calls: make(map[protocol.EMsg]int),
	}
}

func (m *mockLegacyRequester) Do(req *tr.Request) (*tr.Response, error) {
	panic("unimplemented")
}

func (m *mockLegacyRequester) CallLegacy(ctx context.Context, eMsg protocol.EMsg, reqMsg, respMsg proto.Message, mods ...api.RequestModifier) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls[eMsg]++
	m.lastRequest = reqMsg

	if m.responseErr != nil {
		return m.responseErr
	}

	if respMsg != nil && m.responseMsg != nil {
		outBytes, _ := proto.Marshal(m.responseMsg)
		_ = proto.Unmarshal(outBytes, respMsg)
	}

	return nil
}

func (m *mockLegacyRequester) getCallCount(emsg protocol.EMsg) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls[emsg]
}

type mockInitContext struct {
	eventBus *bus.Bus
	proto    *mockLegacyRequester
	handlers map[protocol.EMsg]socket.Handler
}

func (m *mockInitContext) Bus() *bus.Bus                 { return m.eventBus }
func (m *mockInitContext) Proto() api.LegacyRequester    { return m.proto }
func (m *mockInitContext) Logger() log.Logger            { return log.Discard }
func (m *mockInitContext) WebAPI() api.WebAPIRequester   { return nil }
func (m *mockInitContext) Config() steam.Config          { return steam.Config{} }
func (m *mockInitContext) Unified() api.UnifiedRequester { return nil }

func (m *mockInitContext) RegisterPacketHandler(e protocol.EMsg, h socket.Handler) {
	m.handlers[e] = h
}
func (m *mockInitContext) UnregisterPacketHandler(e protocol.EMsg) {
	delete(m.handlers, e)
}
func (m *mockInitContext) GetModule(name string) steam.Module {
	panic("unimplemented")
}
func (m *mockInitContext) RegisterServiceHandler(method string, handler socket.Handler) {
	panic("unimplemented")
}
func (m *mockInitContext) UnregisterServiceHandler(method string) {
	panic("unimplemented")
}

func newMockInitContext() *mockInitContext {
	return &mockInitContext{
		eventBus: bus.NewBus(),
		proto:    newMockRequester(),
		handlers: make(map[protocol.EMsg]socket.Handler),
	}
}

func TestApps_InitAndClose(t *testing.T) {
	a := New()
	initCtx := newMockInitContext()

	if a.Name() != ModuleName {
		t.Errorf("expected module name %s, got %s", ModuleName, a.Name())
	}

	err := a.Init(initCtx)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if _, ok := initCtx.handlers[protocol.EMsg_ClientPlayingSessionState]; !ok {
		t.Error("expected EMsg_ClientPlayingSessionState handler to be registered")
	}

	err = a.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if _, ok := initCtx.handlers[protocol.EMsg_ClientPlayingSessionState]; ok {
		t.Error("expected handler to be unregistered after Close")
	}
}

func TestApps_GetPlayerCount(t *testing.T) {
	a := New()
	initCtx := newMockInitContext()
	_ = a.Init(initCtx)

	ctx := context.Background()

	initCtx.proto.responseMsg = &pb.CMsgDPGetNumberOfCurrentPlayersResponse{
		Eresult:     proto.Int32(int32(protocol.EResult_OK)),
		PlayerCount: proto.Int32(100500),
	}

	count, err := a.GetPlayerCount(ctx, 440) // TF2
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 100500 {
		t.Errorf("expected player count 100500, got %d", count)
	}

	initCtx.proto.responseMsg = &pb.CMsgDPGetNumberOfCurrentPlayersResponse{
		Eresult: proto.Int32(int32(protocol.EResult_AccessDenied)),
	}

	_, err = a.GetPlayerCount(ctx, 440)
	if err == nil {
		t.Error("expected error due to AccessDenied EResult, got nil")
	}

	initCtx.proto.responseErr = errors.New("network error")
	_, err = a.GetPlayerCount(ctx, 440)
	if err == nil {
		t.Error("expected network error, got nil")
	}
}

func TestApps_PlayGames(t *testing.T) {
	a := New()
	initCtx := newMockInitContext()
	_ = a.Init(initCtx)

	var launchedEvents []uint32
	var quitEvents []uint32

	subLaunched := initCtx.eventBus.Subscribe(&AppLaunchedEvent{})
	subQuit := initCtx.eventBus.Subscribe(&AppQuitEvent{})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		timeout := time.After(1 * time.Second)
		for {
			select {
			case ev := <-subLaunched.C():
				launchedEvents = append(launchedEvents, ev.(*AppLaunchedEvent).AppID)
			case ev := <-subQuit.C():
				quitEvents = append(quitEvents, ev.(*AppQuitEvent).AppID)
			case <-timeout:
				return
			}
		}
	}()

	ctx := context.Background()

	err := a.PlayGames(ctx, []uint32{440}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = a.PlayGames(ctx, []uint32{440, 730}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = a.PlayGames(ctx, []uint32{730}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = a.StopPlaying(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	count := initCtx.proto.getCallCount(protocol.EMsg_ClientGamesPlayedWithDataBlob)
	if count != 4 {
		t.Errorf("expected 4 calls to play games, got %d", count)
	}

	wg.Wait()

	expectedLaunched := []uint32{440, 730}
	if !reflect.DeepEqual(launchedEvents, expectedLaunched) {
		t.Errorf("expected launched events %v, got %v", expectedLaunched, launchedEvents)
	}

	expectedQuit := []uint32{440, 730}
	if !reflect.DeepEqual(quitEvents, expectedQuit) {
		t.Errorf("expected quit events %v, got %v", expectedQuit, quitEvents)
	}
}

func TestApps_PlayCustomGames(t *testing.T) {
	a := New()
	initCtx := newMockInitContext()
	_ = a.Init(initCtx)

	ctx := context.Background()

	err := a.PlayCustomGames(ctx, []string{"G-man Bot", "Trading"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if initCtx.proto.getCallCount(protocol.EMsg_ClientGamesPlayedWithDataBlob) != 1 {
		t.Error("expected 1 call to play custom games")
	}

	req := initCtx.proto.lastRequest.(*pb.CMsgClientGamesPlayed)
	if len(req.GamesPlayed) != 2 {
		t.Fatalf("expected 2 custom games, got %d", len(req.GamesPlayed))
	}

	if req.GamesPlayed[0].GetGameId() != nonSteamGameID || req.GamesPlayed[0].GetGameExtraInfo() != "G-man Bot" {
		t.Errorf("invalid first custom game setup")
	}
}

func TestApps_KickPlayingSession(t *testing.T) {
	a := New()
	initCtx := newMockInitContext()
	_ = a.Init(initCtx)

	ctx := context.Background()

	err := a.KickPlayingSession(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if initCtx.proto.getCallCount(protocol.EMsg_ClientKickPlayingSession) != 1 {
		t.Error("expected 1 call to KickPlayingSession")
	}

	a.mu.Lock()
	a.playingBlocked = true
	a.mu.Unlock()

	err = a.PlayGames(ctx, []uint32{440}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if initCtx.proto.getCallCount(protocol.EMsg_ClientKickPlayingSession) != 2 {
		t.Error("expected 2 calls to KickPlayingSession due to forceKick")
	}
}

func TestApps_HandlePlayingSessionState(t *testing.T) {
	a := New()
	initCtx := newMockInitContext()
	_ = a.Init(initCtx)

	subState := initCtx.eventBus.Subscribe(&PlayingStateEvent{})

	msg := &pb.CMsgClientPlayingSessionState{
		PlayingBlocked: proto.Bool(true),
		PlayingApp:     proto.Uint32(730),
	}
	payload, _ := proto.Marshal(msg)

	packet := &protocol.Packet{
		Payload: payload,
	}

	handler := initCtx.handlers[protocol.EMsg_ClientPlayingSessionState]
	handler(packet)

	a.mu.RLock()
	blocked := a.playingBlocked
	a.mu.RUnlock()

	if !blocked {
		t.Error("expected playingBlocked to be true")
	}

	select {
	case ev := <-subState.C():
		stateEvent := ev.(*PlayingStateEvent)
		if !stateEvent.Blocked || stateEvent.PlayingApp != 730 {
			t.Errorf("unexpected event data: %+v", stateEvent)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timed out waiting for PlayingStateEvent")
	}
}

func TestApps_HandlePlayingSessionState_InvalidPayload(t *testing.T) {
	a := New()
	initCtx := newMockInitContext()
	_ = a.Init(initCtx)

	packet := &protocol.Packet{
		Payload: []byte("invalid data"),
	}

	handler := initCtx.handlers[protocol.EMsg_ClientPlayingSessionState]
	handler(packet)
}
