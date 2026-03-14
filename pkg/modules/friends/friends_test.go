// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package friends

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
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
	mu         sync.Mutex
	calls      map[protocol.EMsg]int
	lastReqMsg proto.Message
}

func newMockLegacy() *mockLegacyRequester {
	return &mockLegacyRequester{calls: make(map[protocol.EMsg]int)}
}

func (m *mockLegacyRequester) CallLegacy(ctx context.Context, eMsg protocol.EMsg, reqMsg, respMsg proto.Message, mods ...api.RequestModifier) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls[eMsg]++
	m.lastReqMsg = reqMsg
	return nil
}

func (m *mockLegacyRequester) Do(*tr.Request) (*tr.Response, error) {
	panic("Not implemented")
}

func (m *mockLegacyRequester) getCallCount(emsg protocol.EMsg) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls[emsg]
}

type mockWebAPIRequester struct {
	response any
	err      error
}

func (m *mockWebAPIRequester) CallWebAPI(ctx context.Context, httpMethod, iface, method string, version int, respMsg any, mods ...api.RequestModifier) error {
	if m.err != nil {
		return m.err
	}
	jsonBytes, _ := json.Marshal(m.response)
	return json.Unmarshal(jsonBytes, respMsg)
}

func (m *mockWebAPIRequester) Do(*tr.Request) (*tr.Response, error) {
	panic("Not implemented")
}

type mockCommunityClient struct {
	lastPostPath string
	lastPostData url.Values
	err          error
}

func (m *mockCommunityClient) SessionID(url string) string {
	panic("unimplemented")
}
func (m *mockCommunityClient) Do(req *tr.Request) (*tr.Response, error) {
	panic("unimplemented")
}
func (m *mockCommunityClient) Get(ctx context.Context, path string, mods ...api.RequestModifier) (*tr.Response, error) {
	panic("unimplemented")
}
func (m *mockCommunityClient) GetJSON(ctx context.Context, path string, params url.Values, target any, mods ...api.RequestModifier) error {
	panic("unimplemented")
}
func (m *mockCommunityClient) PostJSON(ctx context.Context, path string, payload any, mods ...api.RequestModifier) (*tr.Response, error) {
	panic("unimplemented")
}

func (m *mockCommunityClient) PostForm(ctx context.Context, path string, data url.Values, mods ...api.RequestModifier) (*tr.Response, error) {
	m.lastPostPath = path
	m.lastPostData = data
	return nil, m.err
}

type mockInitContext struct {
	eventBus       *bus.Bus
	proto          *mockLegacyRequester
	web            *mockWebAPIRequester
	packetHandlers map[protocol.EMsg]socket.Handler
}

func newMockInitContext() *mockInitContext {
	return &mockInitContext{
		eventBus:       bus.NewBus(),
		proto:          newMockLegacy(),
		web:            &mockWebAPIRequester{},
		packetHandlers: make(map[protocol.EMsg]socket.Handler),
	}
}

func (m *mockInitContext) Bus() *bus.Bus                 { return m.eventBus }
func (m *mockInitContext) Proto() api.LegacyRequester    { return m.proto }
func (m *mockInitContext) WebAPI() api.WebAPIRequester   { return m.web }
func (m *mockInitContext) Unified() api.UnifiedRequester { return nil }
func (m *mockInitContext) Logger() log.Logger            { return log.Discard }
func (m *mockInitContext) Config() steam.Config          { return steam.Config{} }

func (m *mockInitContext) RegisterPacketHandler(e protocol.EMsg, h socket.Handler) {
	m.packetHandlers[e] = h
}
func (m *mockInitContext) UnregisterPacketHandler(e protocol.EMsg) {
	delete(m.packetHandlers, e)
}
func (m *mockInitContext) RegisterServiceHandler(method string, handler socket.Handler) {}
func (m *mockInitContext) UnregisterServiceHandler(method string)                       {}
func (m *mockInitContext) GetModule(name string) steam.Module                           { return nil }

type mockAuthContext struct {
	community *mockCommunityClient
	steamID   uint64
}

func (m *mockAuthContext) Community() api.CommunityRequester { return m.community }
func (m *mockAuthContext) SteamID() uint64                   { return m.steamID }

func TestManager_InitAndClose(t *testing.T) {
	m := New()
	initCtx := newMockInitContext()

	err := m.Init(initCtx)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if _, ok := initCtx.packetHandlers[protocol.EMsg_ClientFriendsList]; !ok {
		t.Error("expected ClientFriendsList handler to be registered")
	}

	err = m.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if _, ok := initCtx.packetHandlers[protocol.EMsg_ClientFriendsList]; ok {
		t.Error("expected handler to be unregistered")
	}
}

func TestManager_FriendCache(t *testing.T) {
	m := New()
	m.relationships[101] = protocol.EFriendRelationship_Friend
	m.relationships[102] = protocol.EFriendRelationship_RequestRecipient
	m.users[101] = &PersonaState{PlayerName: "Friend"}

	if !m.IsFriend(101) {
		t.Error("expected IsFriend(101) to be true")
	}
	if m.IsFriend(102) {
		t.Error("expected IsFriend(102) to be false")
	}
	if m.GetFriend(101).PlayerName != "Friend" {
		t.Error("expected GetFriend to return correct persona state")
	}
	if m.GetFriend(999) != nil {
		t.Error("expected GetFriend for unknown user to return nil")
	}

	friends := m.GetFriends()
	if len(friends) != 1 || friends[0] != 101 {
		t.Errorf("expected GetFriends to return [101], got %v", friends)
	}
}

func TestManager_GetMaxFriends(t *testing.T) {
	m := New()
	initCtx := newMockInitContext()
	_ = m.Init(initCtx)
	_ = m.StartAuthed(context.Background(), &mockAuthContext{steamID: 123})

	initCtx.web.response = GetBadgesResponse{
		Response: struct {
			PlayerLevel int `json:"player_level"`
		}{PlayerLevel: 100},
	}

	max, err := m.GetMaxFriends(context.Background())
	if err != nil {
		t.Fatalf("GetMaxFriends failed: %v", err)
	}

	// 250 + (100 * 5) = 750
	if max != 750 {
		t.Errorf("expected max friends 750, got %d", max)
	}

	initCtx.web.response = nil
	max, err = m.GetMaxFriends(context.Background())
	if err != nil || max != 750 {
		t.Errorf("expected cached max friends 750, got %d", max)
	}
}

func TestManager_AddAndRemoveFriend(t *testing.T) {
	m := New()
	initCtx := newMockInitContext()
	_ = m.Init(initCtx)

	ctx := context.Background()
	friendID := uint64(111)

	// Add
	err := m.AddFriend(ctx, friendID)
	if err != nil {
		t.Fatalf("AddFriend failed: %v", err)
	}
	if initCtx.proto.getCallCount(protocol.EMsg_ClientAddFriend) != 1 {
		t.Error("expected 1 call to AddFriend")
	}
	if initCtx.proto.lastReqMsg.(*pb.CMsgClientAddFriend).GetSteamidToAdd() != friendID {
		t.Error("invalid steamid in AddFriend request")
	}

	// Remove
	err = m.RemoveFriend(ctx, friendID)
	if err != nil {
		t.Fatalf("RemoveFriend failed: %v", err)
	}
	if initCtx.proto.getCallCount(protocol.EMsg_ClientRemoveFriend) != 1 {
		t.Error("expected 1 call to RemoveFriend")
	}
	if initCtx.proto.lastReqMsg.(*pb.CMsgClientRemoveFriend).GetFriendid() != friendID {
		t.Error("invalid steamid in RemoveFriend request")
	}
}

func TestManager_InviteToGroups(t *testing.T) {
	m := New()
	initCtx := newMockInitContext()
	mockComm := &mockCommunityClient{}
	_ = m.Init(initCtx)
	_ = m.StartAuthed(context.Background(), &mockAuthContext{steamID: 123, community: mockComm})

	m.relationships[456] = protocol.EFriendRelationship_Friend

	ctx := context.Background()

	mockComm.err = nil
	m.InviteToGroups(ctx, 456, []uint64{789})
	if mockComm.lastPostPath != "actions/GroupInvite" || mockComm.lastPostData.Get("invitee") != "456" {
		t.Error("successful invite failed")
	}

	mockComm.err = errors.New("mock: status 400")
	m.InviteToGroups(ctx, 456, []uint64{789})

	mockComm.err = errors.New("mock: network error")
	m.InviteToGroups(ctx, 456, []uint64{789})

	mockComm.lastPostPath = ""
	m.InviteToGroups(ctx, 999, []uint64{789})
	if mockComm.lastPostPath != "" {
		t.Error("expected non-friend invite to be skipped")
	}
}

func TestManager_HandleFriendsList(t *testing.T) {
	m := New()
	initCtx := newMockInitContext()
	_ = m.Init(initCtx)

	sub := initCtx.eventBus.Subscribe(&RelationshipChangedEvent{})
	handler := initCtx.packetHandlers[protocol.EMsg_ClientFriendsList]

	m.relationships[101] = protocol.EFriendRelationship_Friend

	list := &pb.CMsgClientFriendsList{
		Friends: []*pb.CMsgClientFriendsList_Friend{
			{Ulfriendid: proto.Uint64(101), Efriendrelationship: proto.Uint32(uint32(protocol.EFriendRelationship_None))},
			{Ulfriendid: proto.Uint64(102), Efriendrelationship: proto.Uint32(uint32(protocol.EFriendRelationship_Friend))},
		},
	}
	payload, _ := proto.Marshal(list)

	handler(&protocol.Packet{Payload: payload})

	events := make(map[uint64]*RelationshipChangedEvent)
	timeout := time.After(100 * time.Millisecond)

	for range 2 {
		select {
		case ev := <-sub.C():
			rev := ev.(*RelationshipChangedEvent)
			events[rev.SteamID] = rev
		case <-timeout:
			t.Fatalf("timed out waiting for RelationshipChangedEvent")
		}
	}

	ev101, ok := events[101]
	if !ok || ev101.Old != protocol.EFriendRelationship_Friend || ev101.New != protocol.EFriendRelationship_None {
		t.Errorf("invalid event for friend removal: %+v", ev101)
	}

	ev102, ok := events[102]
	if !ok || ev102.Old != protocol.EFriendRelationship_None || ev102.New != protocol.EFriendRelationship_Friend {
		t.Errorf("invalid event for friend addition: %+v", ev102)
	}

	if m.IsFriend(101) || !m.IsFriend(102) {
		t.Error("internal relationships map was not updated correctly")
	}
}

func TestManager_HandlePersonaState(t *testing.T) {
	m := New()
	initCtx := newMockInitContext()
	_ = m.Init(initCtx)

	sub := initCtx.eventBus.Subscribe(&PersonaStateUpdatedEvent{})
	handler := initCtx.packetHandlers[protocol.EMsg_ClientPersonaState]

	state := &pb.CMsgClientPersonaState{
		Friends: []*pb.CMsgClientPersonaState_Friend{
			{Friendid: proto.Uint64(201), PlayerName: proto.String("NewName")},
		},
	}
	payload, _ := proto.Marshal(state)

	handler(&protocol.Packet{Payload: payload})

	select {
	case ev := <-sub.C():
		pev := ev.(*PersonaStateUpdatedEvent)
		if pev.SteamID != 201 || pev.State.PlayerName != "NewName" {
			t.Errorf("invalid PersonaStateUpdatedEvent: %+v", pev)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("timed out waiting for PersonaStateUpdatedEvent")
	}

	user := m.GetFriend(201)
	if user == nil || user.PlayerName != "NewName" {
		t.Error("user was not added to internal cache")
	}
}
