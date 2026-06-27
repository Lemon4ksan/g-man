// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package chat

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/test/mock"
)

const (
	BotSteamID    = uint64(76561198000000001)
	FriendSteamID = uint64(76561198000000002)
	ChatGroupID   = uint64(123)
	ChatID        = uint64(456)
)

func setupChat(t *testing.T) (*Chat, *mock.InitContext) {
	t.Helper()

	m := New()
	ictx := mock.NewInitContext()
	require.NoError(t, m.Init(ictx))
	t.Cleanup(func() { _ = m.Close() })

	return m, ictx
}

func TestChat_InitAndClose(t *testing.T) {
	t.Parallel()

	m := New()
	ictx := mock.NewInitContext()

	assert.Equal(t, ModuleName, m.Name())

	err := m.Init(ictx)
	require.NoError(t, err)
	ictx.AssertServiceHandlerRegistered(t, "FriendMessagesClient.IncomingMessage#1")

	err = m.Close()
	require.NoError(t, err)
	ictx.AssertServiceHandlerUnregistered(t, "FriendMessagesClient.IncomingMessage#1")
}

func TestChat_StartAuthed(t *testing.T) {
	t.Parallel()

	m, _ := setupChat(t)
	myID := id.ID(BotSteamID)

	err := m.StartAuthed(t.Context(), mock.NewAuthContext(myID))
	require.NoError(t, err)

	m.stateMu.RLock()
	defer m.stateMu.RUnlock()

	assert.Equal(t, myID, m.steamID)
	assert.Equal(t, myID.AccountID(), m.botAccountID)
}

func TestChat_FriendMessaging(t *testing.T) {
	t.Parallel()

	t.Run("send_message_success", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)
		ctx := t.Context()

		err := m.SendMessage(ctx, FriendSteamID, "hello")
		assert.NoError(t, err)

		req := &pb.CFriendMessages_SendMessage_Request{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, FriendSteamID, req.GetSteamid())
		assert.Equal(t, "hello", req.GetMessage())
	})

	t.Run("send_typing_success", func(t *testing.T) {
		t.Parallel()
		m, _ := setupChat(t)
		ctx := t.Context()

		err := m.SendTyping(ctx, FriendSteamID)
		assert.NoError(t, err)
	})

	t.Run("ack_friend_message_success", func(t *testing.T) {
		t.Parallel()
		m, _ := setupChat(t)
		ctx := t.Context()

		err := m.AckFriendMessage(ctx, FriendSteamID, 12345)
		assert.NoError(t, err)
	})

	t.Run("get_recent_messages_success", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)
		ctx := t.Context()

		m.stateMu.Lock()
		m.steamID = id.ID(BotSteamID)
		m.stateMu.Unlock()

		ictx.MockService().
			SetProtoResponse("FriendMessages", "GetRecentMessages", &pb.CFriendMessages_GetRecentMessages_Response{
				Messages: []*pb.CFriendMessages_GetRecentMessages_Response_FriendMessage{
					{Message: proto.String("hi")},
				},
			})

		msgs, err := m.GetRecentMessages(ctx, FriendSteamID, 1)
		require.NoError(t, err)
		require.NotEmpty(t, msgs)
		assert.Equal(t, "hi", msgs[0].GetMessage())
	})
}

func TestChat_GroupMessaging(t *testing.T) {
	t.Parallel()

	t.Run("join_group_chat", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)
		ctx := t.Context()

		ictx.MockService().SetProtoResponse("ChatRoom", "JoinChatRoomGroup", &pb.CChatRoom_JoinChatRoomGroup_Response{
			JoinChatId: proto.Uint64(ChatID),
		})

		err := m.JoinGroupChat(ctx, ChatGroupID)
		assert.NoError(t, err)

		m.stateMu.RLock()
		defer m.stateMu.RUnlock()

		assert.Equal(t, ChatID, m.activeGroupChats[ChatGroupID])
	})

	t.Run("send_group_message_fail_not_in_group", func(t *testing.T) {
		t.Parallel()
		m, _ := setupChat(t)
		ctx := t.Context()

		err := m.SendGroupMessage(ctx, 9999, "hi")
		assert.ErrorIs(t, err, ErrNotInGroupChat)
	})

	t.Run("send_group_message_success", func(t *testing.T) {
		t.Parallel()
		m, _ := setupChat(t)
		ctx := t.Context()

		m.stateMu.Lock()
		m.activeGroupChats[ChatGroupID] = ChatID
		m.stateMu.Unlock()

		err := m.SendGroupMessage(ctx, ChatGroupID, "hello group")
		assert.NoError(t, err)
	})

	t.Run("delete_group_messages_fail_not_in_group", func(t *testing.T) {
		t.Parallel()
		m, _ := setupChat(t)
		ctx := t.Context()

		err := m.DeleteGroupMessages(ctx, 9999, nil)
		assert.ErrorIs(t, err, ErrNotInGroupChat)
	})

	t.Run("delete_group_messages_success", func(t *testing.T) {
		t.Parallel()
		m, _ := setupChat(t)
		ctx := t.Context()

		m.stateMu.Lock()
		m.activeGroupChats[ChatGroupID] = ChatID
		m.stateMu.Unlock()

		err := m.DeleteGroupMessages(ctx, ChatGroupID, nil)
		assert.NoError(t, err)
	})

	t.Run("leave_group_chat_fail_not_in_group", func(t *testing.T) {
		t.Parallel()
		m, _ := setupChat(t)
		ctx := t.Context()

		err := m.LeaveGroupChat(ctx, 9999)
		assert.ErrorIs(t, err, ErrNotInGroupChat)
	})

	t.Run("leave_group_chat_success", func(t *testing.T) {
		t.Parallel()
		m, _ := setupChat(t)
		ctx := t.Context()

		m.stateMu.Lock()
		m.activeGroupChats[ChatGroupID] = ChatID
		m.stateMu.Unlock()

		err := m.LeaveGroupChat(ctx, ChatGroupID)
		assert.NoError(t, err)

		m.stateMu.RLock()
		defer m.stateMu.RUnlock()

		assert.NotContains(t, m.activeGroupChats, ChatGroupID)
	})

	t.Run("ack_group_message_success", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)
		ctx := t.Context()

		err := m.AckGroupMessage(ctx, ChatGroupID, ChatID, 123456)
		require.NoError(t, err)

		req := &pb.CChatRoom_AckChatMessage_Notification{}
		ictx.MockService().GetLastCall(req)

		assert.Equal(t, ChatGroupID, req.GetChatGroupId())
		assert.Equal(t, ChatID, req.GetChatId())
		assert.Equal(t, uint32(123456), req.GetTimestamp())
	})

	t.Run("handle_group_message_unmarshal_error", func(t *testing.T) {
		t.Parallel()
		m, _ := setupChat(t)

		assert.NotPanics(t, func() {
			m.handleGroupMessage(&protocol.Packet{Payload: []byte{0xFF, 0xFF}})
		})

		m.stateMu.RLock()
		defer m.stateMu.RUnlock()

		assert.Empty(t, m.activeGroupChats)
	})

	t.Run("handle_group_message_success_and_state_update", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)

		sub := ictx.Bus().Subscribe(&GroupMessageEvent{})
		defer sub.Unsubscribe()

		ts := uint32(time.Now().Unix())

		msg := &pb.CChatRoom_IncomingChatMessage_Notification{
			ChatGroupId:   proto.Uint64(ChatGroupID),
			ChatId:        proto.Uint64(ChatID),
			SteamidSender: proto.Uint64(FriendSteamID),
			Message:       proto.String("hello group"),
			Timestamp:     proto.Uint32(ts),
		}
		b, err := proto.Marshal(msg)
		require.NoError(t, err)

		m.handleGroupMessage(&protocol.Packet{Payload: b})

		m.stateMu.RLock()
		assert.Equal(t, ChatID, m.activeGroupChats[ChatGroupID])
		m.stateMu.RUnlock()

		select {
		case ev := <-sub.C():
			gme := ev.(*GroupMessageEvent)
			assert.Equal(t, ChatGroupID, gme.ChatGroupID)
			assert.Equal(t, ChatID, gme.ChatID)
			assert.Equal(t, FriendSteamID, gme.SenderID)
			assert.Equal(t, "hello group", gme.Message)
			assert.Equal(t, int64(ts), gme.Timestamp.Unix())

		case <-time.After(100 * time.Millisecond):
			t.Fatal("GroupMessageEvent was never published")
		}
	})
}

func TestChat_HandleIncomingMessage(t *testing.T) {
	t.Parallel()

	t.Run("chat_message", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)

		subMsg := ictx.Bus().Subscribe(&MessageEvent{})
		defer subMsg.Unsubscribe()

		msg := &pb.CFriendMessages_IncomingMessage_Notification{
			SteamidFriend: proto.Uint64(FriendSteamID),
			ChatEntryType: proto.Int32(ChatEntryTypeChatMsg),
			Message:       proto.String("hello"),
		}
		b, _ := proto.Marshal(msg)
		m.handleIncomingMessage(&protocol.Packet{Payload: b})

		select {
		case ev := <-subMsg.C():
			assert.Equal(t, "hello", ev.(*MessageEvent).Message)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Message not received")
		}
	})

	t.Run("emote_message", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)

		subMsg := ictx.Bus().Subscribe(&MessageEvent{})
		defer subMsg.Unsubscribe()

		msg := &pb.CFriendMessages_IncomingMessage_Notification{
			SteamidFriend: proto.Uint64(FriendSteamID),
			ChatEntryType: proto.Int32(ChatEntryTypeEmote),
			Message:       proto.String("emote_text"),
		}
		b, _ := proto.Marshal(msg)
		m.handleIncomingMessage(&protocol.Packet{Payload: b})

		select {
		case ev := <-subMsg.C():
			assert.Equal(t, "emote_text", ev.(*MessageEvent).Message)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Emote message not received")
		}
	})

	t.Run("sticker_message", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)

		subSticker := ictx.Bus().Subscribe(&StickerEvent{})
		defer subSticker.Unsubscribe()

		msg := &pb.CFriendMessages_IncomingMessage_Notification{
			ChatEntryType: proto.Int32(ChatEntryTypeSticker),
			Message:       proto.String("sticker_123"),
		}
		b, _ := proto.Marshal(msg)
		m.handleIncomingMessage(&protocol.Packet{Payload: b})

		select {
		case ev := <-subSticker.C():
			assert.Equal(t, "sticker_123", ev.(*StickerEvent).StickerID)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Sticker not received")
		}
	})

	t.Run("typing_notification", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)

		subTyping := ictx.Bus().Subscribe(&TypingEvent{})
		defer subTyping.Unsubscribe()

		msg := &pb.CFriendMessages_IncomingMessage_Notification{
			ChatEntryType: proto.Int32(ChatEntryTypeTyping),
		}
		b, _ := proto.Marshal(msg)
		m.handleIncomingMessage(&protocol.Packet{Payload: b})

		select {
		case <-subTyping.C():
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Typing not received")
		}
	})

	t.Run("local_echo_ignored", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)

		subMsg := ictx.Bus().Subscribe(&MessageEvent{})
		defer subMsg.Unsubscribe()

		msg := &pb.CFriendMessages_IncomingMessage_Notification{
			LocalEcho:     proto.Bool(true),
			ChatEntryType: proto.Int32(ChatEntryTypeChatMsg),
			Message:       proto.String("should ignore echo"),
		}
		b, _ := proto.Marshal(msg)
		m.handleIncomingMessage(&protocol.Packet{Payload: b})

		select {
		case <-subMsg.C():
			t.Fatal("Local echo should be ignored")
		case <-time.After(50 * time.Millisecond):
			// Ignored!
		}
	})

	t.Run("unhandled_message_type", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)

		subMsg := ictx.Bus().Subscribe(&MessageEvent{})
		defer subMsg.Unsubscribe()

		msg := &pb.CFriendMessages_IncomingMessage_Notification{
			ChatEntryType: proto.Int32(99),
		}
		b, _ := proto.Marshal(msg)
		assert.NotPanics(t, func() {
			m.handleIncomingMessage(&protocol.Packet{Payload: b})
		})

		select {
		case <-subMsg.C():
			t.Fatal("Unhandled message type should be ignored")
		case <-time.After(50 * time.Millisecond):
			// Ignored!
		}
	})

	t.Run("unmarshal_error", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)

		subMsg := ictx.Bus().Subscribe(&MessageEvent{})
		defer subMsg.Unsubscribe()

		assert.NotPanics(t, func() {
			m.handleIncomingMessage(&protocol.Packet{Payload: []byte{0xFF, 0xFF}})
		})

		select {
		case <-subMsg.C():
			t.Fatal("Unmarshal error packet should be ignored")
		case <-time.After(50 * time.Millisecond):
			// Ignored!
		}
	})
}

func TestChat_OfflineSync(t *testing.T) {
	t.Parallel()

	t.Run("sync_unread_success", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)
		m.botAccountID = id.ID(BotSteamID).AccountID()
		ctx := t.Context()

		ictx.MockService().
			SetProtoResponse("FriendMessages", "GetActiveMessageSessions", &pb.CFriendsMessages_GetActiveMessageSessions_Response{
				MessageSessions: []*pb.CFriendsMessages_GetActiveMessageSessions_Response_FriendMessageSession{
					{
						AccountidFriend: proto.Uint32(id.ID(FriendSteamID).AccountID()),
						LastMessage:     proto.Uint32(200),
						LastView:        proto.Uint32(100),
					},
				},
			})

		ictx.MockService().
			SetProtoResponse("FriendMessages", "GetRecentMessages", &pb.CFriendMessages_GetRecentMessages_Response{
				Messages: []*pb.CFriendMessages_GetRecentMessages_Response_FriendMessage{
					{
						Accountid: proto.Uint32(9999),
						Timestamp: proto.Uint32(160),
						Message:   proto.String("unread friend message"),
					},
				},
			})

		sub := ictx.Bus().Subscribe(&MessageEvent{})
		defer sub.Unsubscribe()

		m.synchronizeOfflineMessages(ctx)

		select {
		case ev := <-sub.C():
			assert.Equal(t, "unread friend message", ev.(*MessageEvent).Message)
		case <-time.After(time.Second):
			t.Fatal("Message not synced")
		}
	})

	t.Run("sync_active_sessions_fails_all_retries", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)
		ctx := t.Context()

		ictx.MockService().ResponseErrs["FriendMessages.GetActiveMessageSessions"] = errors.New(
			"sessions WebAPI failed",
		)

		assert.NotPanics(t, func() {
			m.synchronizeOfflineMessages(ctx)
		})
	})

	t.Run("sync_active_sessions_succeeds_on_second_retry", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)
		ctx := t.Context()

		callCount := 0
		ictx.MockService().OnRest = func(method, path string, body any) (*http.Response, error) {
			callCount++
			if callCount == 1 {
				return nil, errors.New("temporary service unavailable")
			}

			resp := &pb.CFriendsMessages_GetActiveMessageSessions_Response{}
			b, _ := proto.Marshal(resp)

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(b)),
			}, nil
		}

		assert.NotPanics(t, func() {
			m.synchronizeOfflineMessages(ctx)
		})
	})

	t.Run("sync_canceled_during_backoff", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)

		ictx.MockService().ResponseErrs["FriendMessages.GetActiveMessageSessions"] = errors.New("temp error")

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		assert.NotPanics(t, func() {
			m.synchronizeOfflineMessages(ctx)
		})
	})

	t.Run("sync_recent_messages_api_error_ignored", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)
		ctx := t.Context()

		ictx.MockService().
			SetProtoResponse("FriendMessages", "GetActiveMessageSessions", &pb.CFriendsMessages_GetActiveMessageSessions_Response{
				MessageSessions: []*pb.CFriendsMessages_GetActiveMessageSessions_Response_FriendMessageSession{
					{
						AccountidFriend: proto.Uint32(id.ID(FriendSteamID).AccountID()),
						LastMessage:     proto.Uint32(200),
						LastView:        proto.Uint32(100),
					},
				},
			})

		ictx.MockService().ResponseErrs["FriendMessages.GetRecentMessages"] = errors.New(
			"recent messages WebAPI failed",
		)

		assert.NotPanics(t, func() {
			m.synchronizeOfflineMessages(ctx)
		})
	})

	t.Run("sync_filter_bot_local_echo_messages", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)
		m.botAccountID = id.ID(BotSteamID).AccountID()
		ctx := t.Context()

		ictx.MockService().
			SetProtoResponse("FriendMessages", "GetActiveMessageSessions", &pb.CFriendsMessages_GetActiveMessageSessions_Response{
				MessageSessions: []*pb.CFriendsMessages_GetActiveMessageSessions_Response_FriendMessageSession{
					{
						AccountidFriend: proto.Uint32(id.ID(FriendSteamID).AccountID()),
						LastMessage:     proto.Uint32(200),
						LastView:        proto.Uint32(100),
					},
				},
			})

		ictx.MockService().
			SetProtoResponse("FriendMessages", "GetRecentMessages", &pb.CFriendMessages_GetRecentMessages_Response{
				Messages: []*pb.CFriendMessages_GetRecentMessages_Response_FriendMessage{
					{
						Accountid: proto.Uint32(m.botAccountID),
						Timestamp: proto.Uint32(150),
						Message:   proto.String("my own echo message"),
					},
					{
						Accountid: proto.Uint32(9999),
						Timestamp: proto.Uint32(160),
						Message:   proto.String("friend message"),
					},
				},
			})

		sub := ictx.Bus().Subscribe(&MessageEvent{})
		defer sub.Unsubscribe()

		m.synchronizeOfflineMessages(ctx)

		select {
		case ev := <-sub.C():
			assert.Equal(t, "friend message", ev.(*MessageEvent).Message)
		case <-time.After(time.Second):
			t.Fatal("Friend message was not published")
		}
	})
}

func TestChat_RateLimit(t *testing.T) {
	t.Parallel()

	m, _ := setupChat(t)
	m.lastMessageTime = time.Now()

	start := time.Now()
	_ = m.applyRateLimit()
	elapsed := time.Since(start)

	assert.GreaterOrEqual(t, elapsed, messageInterval-(200*time.Millisecond))
}

func TestChat_GroupModerationAndHistory(t *testing.T) {
	t.Parallel()

	t.Run("get_group_message_history_success", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)
		ctx := t.Context()

		m.stateMu.Lock()
		m.activeGroupChats[ChatGroupID] = ChatID
		m.stateMu.Unlock()

		ictx.MockService().
			SetProtoResponse("ChatRoom", "GetMessageHistory", &pb.CChatRoom_GetMessageHistory_Response{
				Messages: []*pb.CChatRoom_GetMessageHistory_Response_ChatMessage{
					{
						Sender:          proto.Uint32(9999),
						ServerTimestamp: proto.Uint32(1620000000),
						Message:         proto.String("group scrollback message"),
						Ordinal:         proto.Uint32(1),
					},
				},
			})

		msgs, err := m.GetGroupMessageHistory(ctx, ChatGroupID, 10)
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		assert.Equal(t, "group scrollback message", msgs[0].GetMessage())

		req := &pb.CChatRoom_GetMessageHistory_Request{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, ChatGroupID, req.GetChatGroupId())
		assert.Equal(t, ChatID, req.GetChatId())
		assert.Equal(t, uint32(10), req.GetMaxCount())
	})

	t.Run("get_group_message_history_fail_not_in_group", func(t *testing.T) {
		t.Parallel()
		m, _ := setupChat(t)
		ctx := t.Context()

		_, err := m.GetGroupMessageHistory(ctx, ChatGroupID, 10)
		assert.ErrorIs(t, err, ErrNotInGroupChat)
	})

	t.Run("invite_friend_to_group_chat_success", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)
		ctx := t.Context()

		m.stateMu.Lock()
		m.activeGroupChats[ChatGroupID] = ChatID
		m.stateMu.Unlock()

		ictx.MockService().
			SetProtoResponse("ChatRoom", "InviteFriendToChatRoomGroup", &pb.CChatRoom_InviteFriendToChatRoomGroup_Response{})

		err := m.InviteFriendToGroupChat(ctx, ChatGroupID, FriendSteamID)
		assert.NoError(t, err)

		req := &pb.CChatRoom_InviteFriendToChatRoomGroup_Request{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, ChatGroupID, req.GetChatGroupId())
		assert.Equal(t, ChatID, req.GetChatId())
		assert.Equal(t, FriendSteamID, req.GetSteamid())
	})

	t.Run("invite_friend_to_group_chat_fail_not_in_group", func(t *testing.T) {
		t.Parallel()
		m, _ := setupChat(t)
		ctx := t.Context()

		err := m.InviteFriendToGroupChat(ctx, ChatGroupID, FriendSteamID)
		assert.ErrorIs(t, err, ErrNotInGroupChat)
	})

	t.Run("kick_user_from_group_chat_success", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)
		ctx := t.Context()

		m.stateMu.Lock()
		m.activeGroupChats[ChatGroupID] = ChatID
		m.stateMu.Unlock()

		ictx.MockService().
			SetProtoResponse("ChatRoom", "KickUserFromGroup", &pb.CChatRoom_KickUser_Response{})

		err := m.KickUserFromGroupChat(ctx, ChatGroupID, FriendSteamID, 3600)
		assert.NoError(t, err)

		req := &pb.CChatRoom_KickUser_Request{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, ChatGroupID, req.GetChatGroupId())
		assert.Equal(t, FriendSteamID, req.GetSteamid())
		assert.Equal(t, int32(3600), req.GetExpiration())
	})

	t.Run("kick_user_from_group_chat_fail_not_in_group", func(t *testing.T) {
		t.Parallel()
		m, _ := setupChat(t)
		ctx := t.Context()

		err := m.KickUserFromGroupChat(ctx, ChatGroupID, FriendSteamID, 3600)
		assert.ErrorIs(t, err, ErrNotInGroupChat)
	})

	t.Run("mute_user_in_group_chat_success", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)
		ctx := t.Context()

		m.stateMu.Lock()
		m.activeGroupChats[ChatGroupID] = ChatID
		m.stateMu.Unlock()

		ictx.MockService().
			SetProtoResponse("ChatRoom", "MuteUserInGroup", &pb.CChatRoom_MuteUser_Response{})

		err := m.MuteUserInGroupChat(ctx, ChatGroupID, FriendSteamID, 600)
		assert.NoError(t, err)

		req := &pb.CChatRoom_MuteUser_Request{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, ChatGroupID, req.GetChatGroupId())
		assert.Equal(t, FriendSteamID, req.GetSteamid())
		assert.Equal(t, int32(600), req.GetExpiration())
	})

	t.Run("mute_user_in_group_chat_fail_not_in_group", func(t *testing.T) {
		t.Parallel()
		m, _ := setupChat(t)
		ctx := t.Context()

		err := m.MuteUserInGroupChat(ctx, ChatGroupID, FriendSteamID, 600)
		assert.ErrorIs(t, err, ErrNotInGroupChat)
	})

	t.Run("set_user_ban_state_in_group_chat_success", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)
		ctx := t.Context()

		m.stateMu.Lock()
		m.activeGroupChats[ChatGroupID] = ChatID
		m.stateMu.Unlock()

		ictx.MockService().
			SetProtoResponse("ChatRoom", "SetUserBanState", &pb.CChatRoom_SetUserBanState_Response{})

		err := m.SetUserBanStateInGroupChat(ctx, ChatGroupID, FriendSteamID, true)
		assert.NoError(t, err)

		req := &pb.CChatRoom_SetUserBanState_Request{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, ChatGroupID, req.GetChatGroupId())
		assert.Equal(t, FriendSteamID, req.GetSteamid())
		assert.True(t, req.GetBanState())
	})

	t.Run("set_user_ban_state_in_group_chat_fail_not_in_group", func(t *testing.T) {
		t.Parallel()
		m, _ := setupChat(t)
		ctx := t.Context()

		err := m.SetUserBanStateInGroupChat(ctx, ChatGroupID, FriendSteamID, true)
		assert.ErrorIs(t, err, ErrNotInGroupChat)
	})
}

func TestChat_GroupManagement(t *testing.T) {
	t.Parallel()

	t.Run("create_chat_room_group_success", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)
		ctx := t.Context()

		ictx.MockService().
			SetProtoResponse("ChatRoom", "CreateChatRoomGroup", &pb.CChatRoom_CreateChatRoomGroup_Response{
				ChatGroupId: proto.Uint64(ChatGroupID),
			})

		resp, err := m.CreateChatRoomGroup(ctx, "Test Group", []uint64{FriendSteamID})
		assert.NoError(t, err)
		assert.Equal(t, ChatGroupID, resp.GetChatGroupId())

		req := &pb.CChatRoom_CreateChatRoomGroup_Request{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, "Test Group", req.GetName())
		assert.Equal(t, []uint64{FriendSteamID}, req.GetSteamidInvitees())
	})

	t.Run("save_chat_room_group_success", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)
		ctx := t.Context()

		ictx.MockService().
			SetProtoResponse("ChatRoom", "SaveChatRoomGroup", &pb.CChatRoom_SaveChatRoomGroup_Response{})

		err := m.SaveChatRoomGroup(ctx, ChatGroupID, "Saved Group")
		assert.NoError(t, err)

		req := &pb.CChatRoom_SaveChatRoomGroup_Request{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, ChatGroupID, req.GetChatGroupId())
		assert.Equal(t, "Saved Group", req.GetName())
	})

	t.Run("rename_chat_room_group_success", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)
		ctx := t.Context()

		ictx.MockService().
			SetProtoResponse("ChatRoom", "RenameChatRoomGroup", &pb.CChatRoom_RenameChatRoomGroup_Response{
				Name: proto.String("Renamed Group"),
			})

		name, err := m.RenameChatRoomGroup(ctx, ChatGroupID, "Renamed Group")
		assert.NoError(t, err)
		assert.Equal(t, "Renamed Group", name)

		req := &pb.CChatRoom_RenameChatRoomGroup_Request{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, ChatGroupID, req.GetChatGroupId())
		assert.Equal(t, "Renamed Group", req.GetName())
	})

	t.Run("get_my_chat_room_groups_success", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)
		ctx := t.Context()

		ictx.MockService().
			SetProtoResponse("ChatRoom", "GetMyChatRoomGroups", &pb.CChatRoom_GetMyChatRoomGroups_Response{
				ChatRoomGroups: []*pb.CChatRoomSummaryPair{
					{
						GroupSummary: &pb.CChatRoom_GetChatRoomGroupSummary_Response{
							ChatGroupId:   proto.Uint64(ChatGroupID),
							ChatGroupName: proto.String("My Group"),
						},
					},
				},
			})

		resp, err := m.GetMyChatRoomGroups(ctx)
		assert.NoError(t, err)
		require.Len(t, resp.GetChatRoomGroups(), 1)
		assert.Equal(t, ChatGroupID, resp.GetChatRoomGroups()[0].GetGroupSummary().GetChatGroupId())

		req := &pb.CChatRoom_GetMyChatRoomGroups_Request{}
		ictx.MockService().GetLastCall(req)
		assert.NotNil(t, req)
	})

	t.Run("get_chat_room_group_state_success", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)
		ctx := t.Context()

		ictx.MockService().
			SetProtoResponse("ChatRoom", "GetChatRoomGroupState", &pb.CChatRoom_GetChatRoomGroupState_Response{
				State: &pb.CChatRoomGroupState{
					HeaderState: &pb.CChatRoomGroupHeaderState{
						ChatGroupId: proto.Uint64(ChatGroupID),
						ChatName:    proto.String("My Group"),
					},
				},
			})

		resp, err := m.GetChatRoomGroupState(ctx, ChatGroupID)
		assert.NoError(t, err)
		assert.Equal(t, ChatGroupID, resp.GetState().GetHeaderState().GetChatGroupId())

		req := &pb.CChatRoom_GetChatRoomGroupState_Request{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, ChatGroupID, req.GetChatGroupId())
	})

	t.Run("create_invite_link_with_voice", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)
		ctx := t.Context()

		ictx.MockService().
			SetProtoResponse("ChatRoom", "CreateInviteLink", &pb.CChatRoom_CreateInviteLink_Response{
				InviteCode: proto.String("XYZ"),
			})

		resp, err := m.CreateInviteLink(ctx, ChatGroupID, 3600, ChatID)
		assert.NoError(t, err)
		assert.Equal(t, "XYZ", resp.GetInviteCode())

		req := &pb.CChatRoom_CreateInviteLink_Request{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, ChatGroupID, req.GetChatGroupId())
		assert.Equal(t, uint32(3600), req.GetSecondsValid())
		assert.Equal(t, ChatID, req.GetChatId())
	})

	t.Run("create_invite_link_no_voice", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)
		ctx := t.Context()

		ictx.MockService().
			SetProtoResponse("ChatRoom", "CreateInviteLink", &pb.CChatRoom_CreateInviteLink_Response{
				InviteCode: proto.String("XYZ"),
			})

		resp, err := m.CreateInviteLink(ctx, ChatGroupID, 3600, 0)
		assert.NoError(t, err)
		assert.Equal(t, "XYZ", resp.GetInviteCode())

		req := &pb.CChatRoom_CreateInviteLink_Request{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, ChatGroupID, req.GetChatGroupId())
		assert.Equal(t, uint32(3600), req.GetSecondsValid())
		assert.Equal(t, uint64(0), req.GetChatId())
	})

	t.Run("get_invite_links_for_group_success", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)
		ctx := t.Context()

		ictx.MockService().
			SetProtoResponse("ChatRoom", "GetInviteLinksForGroup", &pb.CChatRoom_GetInviteLinksForGroup_Response{
				InviteLinks: []*pb.CChatRoom_GetInviteLinksForGroup_Response_LinkInfo{
					{
						InviteCode: proto.String("XYZ"),
					},
				},
			})

		links, err := m.GetInviteLinksForGroup(ctx, ChatGroupID)
		assert.NoError(t, err)
		require.Len(t, links, 1)
		assert.Equal(t, "XYZ", links[0].GetInviteCode())

		req := &pb.CChatRoom_GetInviteLinksForGroup_Request{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, ChatGroupID, req.GetChatGroupId())
	})

	t.Run("delete_invite_link_success", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)
		ctx := t.Context()

		ictx.MockService().
			SetProtoResponse("ChatRoom", "DeleteInviteLink", &pb.CChatRoom_DeleteInviteLink_Response{})

		err := m.DeleteInviteLink(ctx, ChatGroupID, "XYZ")
		assert.NoError(t, err)

		req := &pb.CChatRoom_DeleteInviteLink_Request{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, ChatGroupID, req.GetChatGroupId())
		assert.Equal(t, "XYZ", req.GetInviteCode())
	})
}

func TestChat_ModernChatRooms(t *testing.T) {
	t.Parallel()

	t.Run("send_chat_message_success", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)
		ctx := t.Context()

		ictx.MockService().
			SetProtoResponse("ChatRoom", "SendChatMessage", &pb.CChatRoom_SendChatMessage_Response{})

		err := m.SendChatMessage(ctx, ChatGroupID, ChatID, "modern message")
		assert.NoError(t, err)

		req := &pb.CChatRoom_SendChatMessage_Request{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, ChatGroupID, req.GetChatGroupId())
		assert.Equal(t, ChatID, req.GetChatId())
		assert.Equal(t, "modern message", req.GetMessage())
	})

	t.Run("send_chat_reaction_success", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)
		ctx := t.Context()

		ictx.MockService().
			SetProtoResponse("ChatRoom", "UpdateMessageReaction", &pb.CChatRoom_UpdateMessageReaction_Response{})

		err := m.SendChatReaction(
			ctx,
			ChatGroupID,
			ChatID,
			123456,
			1,
			"👍",
			pb.EChatRoomMessageReactionType_k_EChatRoomMessageReactionType_Emoticon,
			true,
		)
		assert.NoError(t, err)

		req := &pb.CChatRoom_UpdateMessageReaction_Request{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, ChatGroupID, req.GetChatGroupId())
		assert.Equal(t, ChatID, req.GetChatId())
		assert.Equal(t, uint32(123456), req.GetServerTimestamp())
		assert.Equal(t, uint32(1), req.GetOrdinal())
		assert.Equal(t, "👍", req.GetReaction())
		assert.Equal(t, pb.EChatRoomMessageReactionType_k_EChatRoomMessageReactionType_Emoticon, req.GetReactionType())
		assert.True(t, req.GetIsAdd())
	})

	t.Run("get_chat_history_success", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)
		ctx := t.Context()

		ictx.MockService().
			SetProtoResponse("ChatRoom", "GetMessageHistory", &pb.CChatRoom_GetMessageHistory_Response{
				Messages: []*pb.CChatRoom_GetMessageHistory_Response_ChatMessage{
					{
						Message: proto.String("history msg"),
					},
				},
			})

		msgs, err := m.GetChatHistory(ctx, ChatGroupID, ChatID, 1620000000, 1, 5)
		assert.NoError(t, err)
		require.Len(t, msgs, 1)
		assert.Equal(t, "history msg", msgs[0].GetMessage())

		req := &pb.CChatRoom_GetMessageHistory_Request{}
		ictx.MockService().GetLastCall(req)
		assert.Equal(t, ChatGroupID, req.GetChatGroupId())
		assert.Equal(t, ChatID, req.GetChatId())
		assert.Equal(t, uint32(1620000000), req.GetStartTime())
		assert.Equal(t, uint32(1), req.GetStartOrdinal())
		assert.Equal(t, uint32(5), req.GetMaxCount())
	})
}

func TestChat_ReactionEvents(t *testing.T) {
	t.Parallel()

	t.Run("friend_reaction_event_success", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)

		sub := ictx.Bus().Subscribe(&ReactionEvent{})
		defer sub.Unsubscribe()

		msg := &pb.CFriendMessages_MessageReaction_Notification{
			SteamidFriend:   proto.Uint64(FriendSteamID),
			Reactor:         proto.Uint64(BotSteamID),
			ServerTimestamp: proto.Uint32(111),
			Ordinal:         proto.Uint32(2),
			Reaction:        proto.String("🚀"),
			ReactionType:    pb.EMessageReactionType_k_EMessageReactionType_Emoticon.Enum(),
			IsAdd:           proto.Bool(true),
		}
		b, _ := proto.Marshal(msg)
		m.handleFriendReaction(&protocol.Packet{Payload: b})

		select {
		case ev := <-sub.C():
			rev := ev.(*ReactionEvent)
			assert.Equal(t, FriendSteamID, rev.FriendSteamID)
			assert.Equal(t, BotSteamID, rev.ReactorSteamID)
			assert.Equal(t, uint32(111), rev.ServerTimestamp)
			assert.Equal(t, uint32(2), rev.Ordinal)
			assert.Equal(t, "🚀", rev.Reaction)
			assert.Equal(t, int32(pb.EMessageReactionType_k_EMessageReactionType_Emoticon), rev.ReactionType)
			assert.True(t, rev.IsAdd)

		case <-time.After(100 * time.Millisecond):
			t.Fatal("ReactionEvent not received")
		}
	})

	t.Run("friend_reaction_event_unmarshal_error", func(t *testing.T) {
		t.Parallel()
		m, _ := setupChat(t)
		assert.NotPanics(t, func() {
			m.handleFriendReaction(&protocol.Packet{Payload: []byte{0xFF, 0xFF}})
		})
	})

	t.Run("group_reaction_event_success", func(t *testing.T) {
		t.Parallel()
		m, ictx := setupChat(t)

		sub := ictx.Bus().Subscribe(&GroupReactionEvent{})
		defer sub.Unsubscribe()

		msg := &pb.CChatRoom_MessageReaction_Notification{
			ChatGroupId:     proto.Uint64(ChatGroupID),
			ChatId:          proto.Uint64(ChatID),
			Reactor:         proto.Uint64(FriendSteamID),
			ServerTimestamp: proto.Uint32(222),
			Ordinal:         proto.Uint32(3),
			Reaction:        proto.String("❤️"),
			ReactionType:    pb.EChatRoomMessageReactionType_k_EChatRoomMessageReactionType_Emoticon.Enum(),
			IsAdd:           proto.Bool(false),
		}
		b, _ := proto.Marshal(msg)
		m.handleGroupReaction(&protocol.Packet{Payload: b})

		select {
		case ev := <-sub.C():
			grev := ev.(*GroupReactionEvent)
			assert.Equal(t, ChatGroupID, grev.ChatGroupID)
			assert.Equal(t, ChatID, grev.ChatID)
			assert.Equal(t, FriendSteamID, grev.ReactorSteamID)
			assert.Equal(t, uint32(222), grev.ServerTimestamp)
			assert.Equal(t, uint32(3), grev.Ordinal)
			assert.Equal(t, "❤️", grev.Reaction)
			assert.Equal(
				t,
				int32(pb.EChatRoomMessageReactionType_k_EChatRoomMessageReactionType_Emoticon),
				grev.ReactionType,
			)
			assert.False(t, grev.IsAdd)

		case <-time.After(100 * time.Millisecond):
			t.Fatal("GroupReactionEvent not received")
		}
	})

	t.Run("group_reaction_event_unmarshal_error", func(t *testing.T) {
		t.Parallel()
		m, _ := setupChat(t)
		assert.NotPanics(t, func() {
			m.handleGroupReaction(&protocol.Packet{Payload: []byte{0xFF, 0xFF}})
		})
	})
}
