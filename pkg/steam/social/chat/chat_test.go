// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package chat

import (
	"testing"
	"time"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/protocol"
	"github.com/lemon4ksan/g-man/test/module"
	"google.golang.org/protobuf/proto"
)

const (
	BotSteamID    = 111111111
	FriendSteamID = 76561197960287930
	ChatGroupID   = 999
	ChatID        = 888
)

func setupChat(t *testing.T) (*Manager, *module.InitContext) {
	t.Helper()
	m := New()
	ictx := module.NewInitContext()

	if err := m.Init(ictx); err != nil {
		t.Fatalf("failed to init chat: %v", err)
	}

	t.Cleanup(func() {
		_ = m.Close()
	})

	return m, ictx
}

func invokeService(t *testing.T, ictx *module.InitContext, method string, msg proto.Message) {
	t.Helper()
	handler, ok := ictx.GetServiceHandler(method)

	if !ok {
		t.Fatalf("service handler %s not registered", method)
	}

	payload, _ := proto.Marshal(msg)
	handler(&protocol.Packet{Payload: payload})
}

func TestChatManager_InitAndClose(t *testing.T) {
	m := New()
	ictx := module.NewInitContext()

	t.Run("Name", func(t *testing.T) {
		if m.Name() != ModuleName {
			t.Errorf("expected %s, got %s", ModuleName, m.Name())
		}
	})

	t.Run("Registration", func(t *testing.T) {
		_ = m.Init(ictx)
		if _, ok := ictx.GetServiceHandler("FriendMessagesClient.IncomingMessage#1"); !ok {
			t.Error("FriendMessagesClient handler not registered")
		}
		if _, ok := ictx.GetServiceHandler("ChatRoomClient.NotifyIncomingChatMessage#1"); !ok {
			t.Error("ChatRoomClient handler not registered")
		}
	})

	t.Run("Cleanup", func(t *testing.T) {
		_ = m.Close()
		if _, ok := ictx.GetServiceHandler("FriendMessagesClient.IncomingMessage#1"); ok {
			t.Error("handlers should be removed after Close")
		}
	})
}

func TestChatManager_SendMessage(t *testing.T) {
	m, ictx := setupChat(t)
	text := "Hello, G-man!"

	err := m.SendMessage(t.Context(), FriendSteamID, text)
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	req := &pb.CFriendMessages_SendMessage_Request{}
	ictx.MockService().GetLastCall(req)

	if req.GetSteamid() != FriendSteamID || req.GetMessage() != text {
		t.Errorf("unexpected request data: %+v", req)
	}
	if req.GetChatEntryType() != ChatEntryTypeChatMsg {
		t.Error("should use ChatEntryTypeChatMsg by default")
	}
}

func TestChatManager_GetRecentMessages(t *testing.T) {
	m, ictx := setupChat(t)
	_ = m.StartAuthed(t.Context(), module.NewAuthContext(BotSteamID))

	mockMsgs := []*pb.CFriendMessages_GetRecentMessages_Response_FriendMessage{
		{Message: proto.String("hi")},
		{Message: proto.String("how are you?")},
	}
	ictx.MockService().SetProtoResponse("FriendMessages", "GetRecentMessages", &pb.CFriendMessages_GetRecentMessages_Response{
		Messages: mockMsgs,
	})

	msgs, err := m.GetRecentMessages(t.Context(), FriendSteamID, 2)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}

	if len(msgs) != 2 || msgs[0].GetMessage() != "hi" {
		t.Errorf("unexpected messages response: %v", msgs)
	}

	req := &pb.CFriendMessages_GetRecentMessages_Request{}
	ictx.MockService().GetLastCall(req)
	if req.GetSteamid1() != BotSteamID || req.GetSteamid2() != FriendSteamID {
		t.Error("request should include both bot and friend IDs")
	}
}

func TestChatManager_HandleIncomingMessage(t *testing.T) {
	_, ictx := setupChat(t)
	subMsg := ictx.Bus().Subscribe(&MessageEvent{})
	subTyping := ictx.Bus().Subscribe(&TypingEvent{})

	ts := uint32(time.Now().Unix())

	t.Run("Normal Message", func(t *testing.T) {
		invokeService(t, ictx, "FriendMessagesClient.IncomingMessage#1", &pb.CFriendMessages_IncomingMessage_Notification{
			SteamidFriend:          proto.Uint64(FriendSteamID),
			ChatEntryType:          proto.Int32(ChatEntryTypeChatMsg),
			Message:                proto.String("Test!"),
			Rtime32ServerTimestamp: proto.Uint32(ts),
		})

		select {
		case ev := <-subMsg.C():
			me := ev.(*MessageEvent)
			if me.SenderID != FriendSteamID || me.Message != "Test!" {
				t.Errorf("invalid MessageEvent: %+v", me)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("MessageEvent not received")
		}
	})

	t.Run("Typing Notification", func(t *testing.T) {
		invokeService(t, ictx, "FriendMessagesClient.IncomingMessage#1", &pb.CFriendMessages_IncomingMessage_Notification{
			SteamidFriend: proto.Uint64(FriendSteamID),
			ChatEntryType: proto.Int32(ChatEntryTypeTyping),
		})

		select {
		case ev := <-subTyping.C():
			if ev.(*TypingEvent).SenderID != FriendSteamID {
				t.Error("invalid TypingEvent sender")
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("TypingEvent not received")
		}
	})

	t.Run("Ignore LocalEcho", func(t *testing.T) {
		invokeService(t, ictx, "FriendMessagesClient.IncomingMessage#1", &pb.CFriendMessages_IncomingMessage_Notification{
			LocalEcho: proto.Bool(true),
			Message:   proto.String("Ignore me"),
		})

		select {
		case <-subMsg.C():
			t.Fatal("LocalEcho message should not trigger an event")
		case <-time.After(50 * time.Millisecond):
			// Success
		}
	})
}

func TestChatManager_HandleGroupMessage(t *testing.T) {
	_, ictx := setupChat(t)
	subGroup := ictx.Bus().Subscribe(&GroupMessageEvent{})

	invokeService(t, ictx, "ChatRoomClient.NotifyIncomingChatMessage#1", &pb.CChatRoom_IncomingChatMessage_Notification{
		ChatGroupId:   proto.Uint64(ChatGroupID),
		ChatId:        proto.Uint64(ChatID),
		SteamidSender: proto.Uint64(FriendSteamID),
		Message:       proto.String("Group msg"),
	})

	select {
	case ev := <-subGroup.C():
		ge := ev.(*GroupMessageEvent)
		if ge.ChatGroupID != ChatGroupID || ge.SenderID != FriendSteamID || ge.Message != "Group msg" {
			t.Errorf("invalid GroupMessageEvent: %+v", ge)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("GroupMessageEvent not received")
	}
}
