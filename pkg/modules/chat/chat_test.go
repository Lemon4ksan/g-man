// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package chat

import (
	"context"
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

type mockUnifiedRequester struct {
	mu          sync.Mutex
	calls       []string
	lastReqMsg  proto.Message
	responseMsg proto.Message
	responseErr error
}

func (m *mockUnifiedRequester) CallUnified(ctx context.Context, httpMethod, iface, method string, version int, reqMsg, respMsg any, mods ...api.RequestModifier) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls = append(m.calls, iface+"."+method)

	if msg, ok := reqMsg.(proto.Message); ok {
		m.lastReqMsg = msg
	}

	if m.responseErr != nil {
		return m.responseErr
	}

	if respMsg != nil && m.responseMsg != nil {
		outBytes, _ := proto.Marshal(m.responseMsg)
		_ = proto.Unmarshal(outBytes, respMsg.(proto.Message))
	}

	return nil
}

func (m *mockUnifiedRequester) Do(*tr.Request) (*tr.Response, error) {
	panic("Not implemented")
}

type mockInitContext struct {
	eventBus        *bus.Bus
	unified         *mockUnifiedRequester
	serviceHandlers map[string]socket.Handler
}

func newMockInitContext() *mockInitContext {
	return &mockInitContext{
		eventBus:        bus.NewBus(),
		unified:         &mockUnifiedRequester{},
		serviceHandlers: make(map[string]socket.Handler),
	}
}

func (m *mockInitContext) Bus() *bus.Bus                 { return m.eventBus }
func (m *mockInitContext) Proto() api.LegacyRequester    { return nil }
func (m *mockInitContext) Unified() api.UnifiedRequester { return m.unified }
func (m *mockInitContext) Logger() log.Logger            { return log.Discard }
func (m *mockInitContext) WebAPI() api.WebAPIRequester   { return nil }
func (m *mockInitContext) Config() steam.Config          { return steam.Config{} }

func (m *mockInitContext) RegisterServiceHandler(method string, handler socket.Handler) {
	m.serviceHandlers[method] = handler
}
func (m *mockInitContext) UnregisterServiceHandler(method string) {
	delete(m.serviceHandlers, method)
}
func (m *mockInitContext) RegisterPacketHandler(e protocol.EMsg, h socket.Handler) {}
func (m *mockInitContext) UnregisterPacketHandler(e protocol.EMsg)                 {}
func (m *mockInitContext) GetModule(name string) steam.Module                      { return nil }

type mockAuthContext struct {
	steamID uint64
}

func (m *mockAuthContext) Community() api.CommunityRequester { return nil }
func (m *mockAuthContext) SteamID() uint64                   { return m.steamID }

func TestChatManager_InitAndClose(t *testing.T) {
	m := New()
	initCtx := newMockInitContext()

	if m.Name() != ModuleName {
		t.Errorf("expected module name %s, got %s", ModuleName, m.Name())
	}

	err := m.Init(initCtx)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if _, ok := initCtx.serviceHandlers["FriendMessagesClient.IncomingMessage#1"]; !ok {
		t.Error("expected FriendMessagesClient.IncomingMessage#1 to be registered")
	}

	err = m.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if _, ok := initCtx.serviceHandlers["FriendMessagesClient.IncomingMessage#1"]; ok {
		t.Error("expected handlers to be unregistered after Close")
	}
}

func TestChatManager_SendMessage(t *testing.T) {
	m := New()
	initCtx := newMockInitContext()
	_ = m.Init(initCtx)

	ctx := context.Background()
	targetID := uint64(76561197960287930)
	text := "Hello World!"

	err := m.SendMessage(ctx, targetID, text)
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	if len(initCtx.unified.calls) != 1 || initCtx.unified.calls[0] != "FriendMessages.SendMessage" {
		t.Fatalf("expected FriendMessages.SendMessage call, got %v", initCtx.unified.calls)
	}

	req := initCtx.unified.lastReqMsg.(*pb.CFriendMessages_SendMessage_Request)
	if req.GetSteamid() != targetID {
		t.Errorf("expected target %d, got %d", targetID, req.GetSteamid())
	}
	if req.GetMessage() != text {
		t.Errorf("expected message %q, got %q", text, req.GetMessage())
	}
	if req.GetChatEntryType() != ChatEntryTypeChatMsg {
		t.Errorf("expected chat entry type %d", ChatEntryTypeChatMsg)
	}
}

func TestChatManager_GetRecentMessages(t *testing.T) {
	m := New()
	initCtx := newMockInitContext()
	_ = m.Init(initCtx)
	_ = m.StartAuthed(context.Background(), &mockAuthContext{steamID: 11111})

	expectedMsgs := []*pb.CFriendMessages_GetRecentMessages_Response_FriendMessage{
		{Message: proto.String("msg1")},
		{Message: proto.String("msg2")},
	}
	initCtx.unified.responseMsg = &pb.CFriendMessages_GetRecentMessages_Response{
		Messages: expectedMsgs,
	}

	ctx := context.Background()
	targetID := uint64(22222)

	msgs, err := m.GetRecentMessages(ctx, targetID, 5)
	if err != nil {
		t.Fatalf("GetRecentMessages failed: %v", err)
	}

	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	req := initCtx.unified.lastReqMsg.(*pb.CFriendMessages_GetRecentMessages_Request)
	if req.GetSteamid1() != 11111 || req.GetSteamid2() != targetID || req.GetCount() != 5 {
		t.Errorf("invalid request parameters: %+v", req)
	}
}

func TestChatManager_HandleIncomingMessage_Chat(t *testing.T) {
	m := New()
	initCtx := newMockInitContext()
	_ = m.Init(initCtx)

	subMsg := initCtx.eventBus.Subscribe(&MessageEvent{})
	subTyping := initCtx.eventBus.Subscribe(&TypingEvent{})

	handler := initCtx.serviceHandlers["FriendMessagesClient.IncomingMessage#1"]

	timestamp := uint32(time.Now().Unix())
	msg := &pb.CFriendMessages_IncomingMessage_Notification{
		SteamidFriend:          proto.Uint64(123456789),
		ChatEntryType:          proto.Int32(ChatEntryTypeChatMsg),
		Message:                proto.String("Test message"),
		Rtime32ServerTimestamp: proto.Uint32(timestamp),
		LocalEcho:              proto.Bool(false),
	}
	payload, _ := proto.Marshal(msg)

	handler(&protocol.Packet{Payload: payload})

	select {
	case ev := <-subMsg.C():
		me := ev.(*MessageEvent)
		if me.SenderID != 123456789 || me.Message != "Test message" || me.Timestamp.Unix() != int64(timestamp) {
			t.Errorf("invalid MessageEvent data: %+v", me)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for MessageEvent")
	}

	select {
	case <-subTyping.C():
		t.Fatal("unexpected TypingEvent")
	default:
	}
}

func TestChatManager_HandleIncomingMessage_Typing(t *testing.T) {
	m := New()
	initCtx := newMockInitContext()
	_ = m.Init(initCtx)

	subTyping := initCtx.eventBus.Subscribe(&TypingEvent{})
	handler := initCtx.serviceHandlers["FriendMessagesClient.IncomingMessage#1"]

	msg := &pb.CFriendMessages_IncomingMessage_Notification{
		SteamidFriend: proto.Uint64(987654321),
		ChatEntryType: proto.Int32(ChatEntryTypeTyping),
		LocalEcho:     proto.Bool(false),
	}
	payload, _ := proto.Marshal(msg)

	handler(&protocol.Packet{Payload: payload})

	select {
	case ev := <-subTyping.C():
		te := ev.(*TypingEvent)
		if te.SenderID != 987654321 {
			t.Errorf("invalid TypingEvent data: %+v", te)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for TypingEvent")
	}
}

func TestChatManager_HandleIncomingMessage_LocalEcho(t *testing.T) {
	m := New()
	initCtx := newMockInitContext()
	_ = m.Init(initCtx)

	subMsg := initCtx.eventBus.Subscribe(&MessageEvent{})
	handler := initCtx.serviceHandlers["FriendMessagesClient.IncomingMessage#1"]

	// LocalEcho = true (это сообщение, которое мы отправили с телефона/другого ПК)
	msg := &pb.CFriendMessages_IncomingMessage_Notification{
		SteamidFriend: proto.Uint64(123),
		ChatEntryType: proto.Int32(ChatEntryTypeChatMsg),
		Message:       proto.String("This should be ignored"),
		LocalEcho:     proto.Bool(true),
	}
	payload, _ := proto.Marshal(msg)

	handler(&protocol.Packet{Payload: payload})

	select {
	case <-subMsg.C():
		t.Fatal("expected LocalEcho message to be ignored, but received event")
	case <-time.After(50 * time.Millisecond):
		// Success
	}
}

func TestChatManager_HandleGroupMessage(t *testing.T) {
	m := New()
	initCtx := newMockInitContext()
	_ = m.Init(initCtx)

	subGroup := initCtx.eventBus.Subscribe(&GroupMessageEvent{})
	handler := initCtx.serviceHandlers["ChatRoomClient.NotifyIncomingChatMessage#1"]

	timestamp := uint32(time.Now().Unix())
	msg := &pb.CChatRoom_IncomingChatMessage_Notification{
		ChatGroupId:   proto.Uint64(111),
		ChatId:        proto.Uint64(222),
		SteamidSender: proto.Uint64(333),
		Message:       proto.String("Group hello"),
		Timestamp:     proto.Uint32(timestamp),
	}
	payload, _ := proto.Marshal(msg)

	handler(&protocol.Packet{Payload: payload})

	select {
	case ev := <-subGroup.C():
		ge := ev.(*GroupMessageEvent)
		if ge.ChatGroupID != 111 || ge.ChatID != 222 || ge.SenderID != 333 || ge.Message != "Group hello" {
			t.Errorf("invalid GroupMessageEvent data: %+v", ge)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for GroupMessageEvent")
	}
}
