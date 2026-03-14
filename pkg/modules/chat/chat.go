// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package chat

import (
	"context"
	"time"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"
	pb "github.com/lemon4ksan/g-man/pkg/steam/protobuf"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"google.golang.org/protobuf/proto"
)

const ModuleName string = "chat"

type Manager struct {
	bus       *bus.Bus
	logger    log.Logger
	unified   api.UnifiedRequester
	proto     api.LegacyRequester
	steamID   uint64
	closeFunc func()
}

func New() *Manager {
	return &Manager{}
}

func (m *Manager) Name() string { return ModuleName }

func (m *Manager) Init(init steam.InitContext) error {
	m.bus = init.Bus()
	m.logger = init.Logger().WithModule(ModuleName)
	m.unified = init.Unified()
	m.proto = init.Proto()

	init.RegisterServiceHandler("FriendMessagesClient.IncomingMessage#1", m.handleIncomingMessage)
	init.RegisterServiceHandler("ChatRoomClient.NotifyIncomingChatMessage#1", m.handleGroupMessage)
	// "ChatRoomClient.NotifyChatGroupUserStateChanged#1"
	// "ChatRoomClient.NotifyMemberStateChange#1"
	m.closeFunc = func() {
		init.UnregisterServiceHandler("FriendMessagesClient.IncomingMessage#1")
		init.UnregisterServiceHandler("ChatRoomClient.NotifyIncomingChatMessage#1")
	}

	return nil
}

func (m *Manager) Start(ctx context.Context) error { return nil }

func (m *Manager) StartAuthed(ctx context.Context, auth steam.AuthContext) error {
	m.steamID = auth.SteamID()
	return nil
}

func (m *Manager) Close() error {
	if m.closeFunc != nil {
		m.closeFunc()
		m.closeFunc = nil
	}
	return nil
}

// SendMessage sends a text message to the user by SteamID64.
func (m *Manager) SendMessage(ctx context.Context, steamID uint64, text string) error {
	req := &pb.CFriendMessages_SendMessage_Request{
		Steamid:        proto.Uint64(steamID),
		ChatEntryType:  proto.Int32(ChatEntryTypeChatMsg),
		Message:        proto.String(text),
		ContainsBbcode: proto.Bool(true),
	}
	var resp pb.CFriendMessages_SendMessage_Response
	return m.unified.CallUnified(ctx, "", "FriendMessages", "SendMessage", 1, req, &resp)
}

// SendTyping sends the "Typing..." status to the user.
// Useful for simulating the bot's "live" behavior before sending a long message.
func (m *Manager) SendTyping(ctx context.Context, steamID uint64) error {
	req := &pb.CFriendMessages_SendMessage_Request{
		Steamid:       proto.Uint64(steamID),
		ChatEntryType: proto.Int32(ChatEntryTypeTyping),
	}
	return m.unified.CallUnified(ctx, "", "FriendMessages", "SendMessage", 1, req, nil)
}

// AckFriendMessage marks messages from a friend as "read".
func (m *Manager) AckFriendMessage(ctx context.Context, steamID uint64, timestamp uint32) error {
	req := &pb.CFriendMessages_AckMessage_Notification{
		SteamidPartner: proto.Uint64(steamID),
		Timestamp:      proto.Uint32(timestamp),
	}
	return m.unified.CallUnified(ctx, "", "FriendMessages", "AckMessage", 1, req, nil)
}

// SendGroupMessage sends a message to a group chat.
func (m *Manager) SendGroupMessage(ctx context.Context, groupID, chatID uint64, text string) error {
	req := &pb.CChatRoom_SendChatMessage_Request{
		ChatGroupId: proto.Uint64(groupID),
		ChatId:      proto.Uint64(chatID),
		Message:     proto.String(text),
	}

	var resp pb.CChatRoom_SendChatMessage_Response
	return m.unified.CallUnified(ctx, "", "ChatRoom", "SendChatMessage", 1, req, &resp)
}

// GetRecentMessages gets the message history with a friend.
func (m *Manager) GetRecentMessages(ctx context.Context, steamID uint64, count uint32) ([]*pb.CFriendMessages_GetRecentMessages_Response_FriendMessage, error) {
	req := &pb.CFriendMessages_GetRecentMessages_Request{
		Steamid1:     proto.Uint64(m.steamID),
		Steamid2:     proto.Uint64(steamID),
		Count:        proto.Uint32(count),
		BbcodeFormat: proto.Bool(true),
	}

	var resp pb.CFriendMessages_GetRecentMessages_Response

	err := m.unified.CallUnified(ctx, "", "FriendMessages", "GetRecentMessages", 1, req, &resp)
	if err != nil {
		return nil, err
	}

	return resp.GetMessages(), nil
}

// GetActiveSessions gets the list of friends with whom we have recent (active) conversations.
func (m *Manager) GetActiveSessions(ctx context.Context, since time.Time) ([]*pb.CFriendsMessages_GetActiveMessageSessions_Response_FriendMessageSession, error) {
	req := &pb.CFriendsMessages_GetActiveMessageSessions_Request{}

	if !since.IsZero() {
		req.LastmessageSince = proto.Uint32(uint32(since.Unix()))
	}

	var resp pb.CFriendsMessages_GetActiveMessageSessions_Response
	err := m.unified.CallUnified(ctx, "", "FriendMessages", "GetActiveMessageSessions", 1, req, &resp)
	if err != nil {
		return nil, err
	}

	return resp.GetMessageSessions(), nil
}

// DeleteGroupMessages deletes messages from a group chat (requires moderator rights).
func (m *Manager) DeleteGroupMessages(ctx context.Context, groupID, chatID uint64, messages []*pb.CChatRoom_DeleteChatMessages_Request_Message) error {
	req := &pb.CChatRoom_DeleteChatMessages_Request{
		ChatGroupId: proto.Uint64(groupID),
		ChatId:      proto.Uint64(chatID),
		Messages:    messages,
	}

	var resp pb.CChatRoom_DeleteChatMessages_Response
	return m.unified.CallUnified(ctx, "", "ChatRoom", "DeleteChatMessages", 1, req, &resp)
}

func (m *Manager) handleIncomingMessage(packet *protocol.Packet) {
	var msg pb.CFriendMessages_IncomingMessage_Notification
	if err := proto.Unmarshal(packet.Payload, &msg); err != nil {
		m.logger.Error("Failed to unmarshal unified incoming message", log.Err(err))
		return
	}

	senderID := msg.GetSteamidFriend()
	chatType := msg.GetChatEntryType()

	if msg.GetLocalEcho() {
		return
	}

	switch chatType {
	case ChatEntryTypeChatMsg:
		m.bus.Publish(&MessageEvent{
			SenderID:  senderID,
			Message:   msg.GetMessage(),
			Timestamp: time.Unix(int64(msg.GetRtime32ServerTimestamp()), 0),
			Ordinal:   msg.GetOrdinal(),
		})

	case ChatEntryTypeTyping:
		m.bus.Publish(&TypingEvent{SenderID: senderID})
	}
}

func (m *Manager) handleGroupMessage(packet *protocol.Packet) {
	var msg pb.CChatRoom_IncomingChatMessage_Notification
	if err := proto.Unmarshal(packet.Payload, &msg); err != nil {
		m.logger.Error("Failed to unmarshal group chat message", log.Err(err))
		return
	}

	m.bus.Publish(&GroupMessageEvent{
		ChatGroupID: msg.GetChatGroupId(),
		ChatID:      msg.GetChatId(),
		SenderID:    msg.GetSteamidSender(),
		Message:     msg.GetMessage(),
		Timestamp:   time.Unix(int64(msg.GetTimestamp()), 0),
	})
}
