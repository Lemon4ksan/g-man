// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package chat manages one-on-one friend messages and Steam group chats.
package chat

import (
	"context"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
)

// ModuleName is the unique identifier for the chat module.
const ModuleName string = "chat"

// WithModule returns a steam.Option that registers the chat module in the client.
func WithModule() steam.Option {
	return func(c *steam.Client) {
		c.RegisterModule(New())
	}
}

// Chat handles sending and receiving messages via Steam's Unified Services.
type Chat struct {
	module.Base

	// Dependencies
	service service.Doer
	steamID id.ID

	mu         sync.Mutex
	unregFuncs []func()
}

// New creates a new instance of the chat manager.
func New() *Chat {
	return &Chat{
		Base: module.New(ModuleName),
	}
}

// Init registers service handlers for incoming friend and group messages.
func (m *Chat) Init(init module.InitContext) error {
	if err := m.Base.Init(init); err != nil {
		return err
	}

	m.service = init.Service()

	friendHandler := "FriendMessagesClient.IncomingMessage#1"
	groupHandler := "ChatRoomClient.NotifyIncomingChatMessage#1"

	init.RegisterServiceHandler(friendHandler, m.handleIncomingMessage)
	init.RegisterServiceHandler(groupHandler, m.handleGroupMessage)

	m.unregFuncs = append(m.unregFuncs, func() {
		init.UnregisterServiceHandler(friendHandler)
		init.UnregisterServiceHandler(groupHandler)
	})

	return nil
}

// StartAuthed updates the current user's SteamID after a successful login.
func (m *Chat) StartAuthed(ctx context.Context, auth module.AuthContext) error {
	m.mu.Lock()
	m.steamID = auth.SteamID()
	m.mu.Unlock()

	return nil
}

// Close ensures all service handlers are removed and background tasks are stopped.
func (m *Chat) Close() error {
	m.mu.Lock()
	for _, unreg := range m.unregFuncs {
		unreg()
	}

	m.unregFuncs = nil
	m.mu.Unlock()

	return m.Base.Close()
}

// SendMessage sends a plain text message to a specific Steam user.
func (m *Chat) SendMessage(ctx context.Context, steamID uint64, text string) error {
	req := &pb.CFriendMessages_SendMessage_Request{
		Steamid:        proto.Uint64(steamID),
		ChatEntryType:  proto.Int32(ChatEntryTypeChatMsg),
		Message:        proto.String(text),
		ContainsBbcode: proto.Bool(true),
	}
	_, err := service.Unified[service.NoResponse](ctx, m.service, req)

	return err
}

// SendTyping notifies a friend that the bot is currently typing a message.
func (m *Chat) SendTyping(ctx context.Context, steamID uint64) error {
	req := &pb.CFriendMessages_SendMessage_Request{
		Steamid:       proto.Uint64(steamID),
		ChatEntryType: proto.Int32(ChatEntryTypeTyping),
	}
	_, err := service.Unified[service.NoResponse](ctx, m.service, req)

	return err
}

// AckFriendMessage marks all messages from a specific friend up to the timestamp as read.
func (m *Chat) AckFriendMessage(ctx context.Context, steamID uint64, timestamp uint32) error {
	req := &pb.CFriendMessages_AckMessage_Notification{
		SteamidPartner: proto.Uint64(steamID),
		Timestamp:      proto.Uint32(timestamp),
	}
	_, err := service.Unified[service.NoResponse](ctx, m.service, req)

	return err
}

// SendGroupMessage sends a text message to a Steam group chatroom.
func (m *Chat) SendGroupMessage(ctx context.Context, groupID, chatID uint64, text string) error {
	req := &pb.CChatRoom_SendChatMessage_Request{
		ChatGroupId: proto.Uint64(groupID),
		ChatId:      proto.Uint64(chatID),
		Message:     proto.String(text),
	}
	_, err := service.Unified[service.NoResponse](ctx, m.service, req)

	return err
}

// GetRecentMessages retrieves the chat history with a specific friend.
func (m *Chat) GetRecentMessages(
	ctx context.Context,
	steamID uint64,
	count uint32,
) ([]*pb.CFriendMessages_GetRecentMessages_Response_FriendMessage, error) {
	m.mu.Lock()
	myID := m.steamID
	m.mu.Unlock()

	req := &pb.CFriendMessages_GetRecentMessages_Request{
		Steamid1:     proto.Uint64(myID.Uint64()),
		Steamid2:     proto.Uint64(steamID),
		Count:        proto.Uint32(count),
		BbcodeFormat: proto.Bool(true),
	}

	resp, err := service.Unified[pb.CFriendMessages_GetRecentMessages_Response](ctx, m.service, req)
	if err != nil {
		return nil, err
	}

	return resp.GetMessages(), nil
}

// DeleteGroupMessages removes specific messages from a group chat (requires appropriate permissions).
func (m *Chat) DeleteGroupMessages(
	ctx context.Context,
	groupID, chatID uint64,
	messages []*pb.CChatRoom_DeleteChatMessages_Request_Message,
) error {
	req := &pb.CChatRoom_DeleteChatMessages_Request{
		ChatGroupId: proto.Uint64(groupID),
		ChatId:      proto.Uint64(chatID),
		Messages:    messages,
	}
	_, err := service.Unified[pb.CChatRoom_DeleteChatMessages_Response](ctx, m.service, req)

	return err
}

func (m *Chat) handleIncomingMessage(packet *protocol.Packet) {
	msg := &pb.CFriendMessages_IncomingMessage_Notification{}
	if err := proto.Unmarshal(packet.Payload, msg); err != nil {
		m.Logger.Error("Failed to unmarshal incoming friend message", log.Err(err))
		return
	}

	if msg.GetLocalEcho() {
		return // Ignore our own messages reflected by the server
	}

	senderID := msg.GetSteamidFriend()
	chatType := msg.GetChatEntryType()

	switch chatType {
	case ChatEntryTypeChatMsg:
		m.Bus.Publish(&MessageEvent{
			SenderID:  senderID,
			Message:   msg.GetMessage(),
			Timestamp: time.Unix(int64(msg.GetRtime32ServerTimestamp()), 0),
			Ordinal:   msg.GetOrdinal(),
		})

	case ChatEntryTypeTyping:
		m.Bus.Publish(&TypingEvent{SenderID: senderID})
	}
}

func (m *Chat) handleGroupMessage(packet *protocol.Packet) {
	msg := &pb.CChatRoom_IncomingChatMessage_Notification{}
	if err := proto.Unmarshal(packet.Payload, msg); err != nil {
		m.Logger.Error("Failed to unmarshal incoming group message", log.Err(err))
		return
	}

	m.Bus.Publish(&GroupMessageEvent{
		ChatGroupID: msg.GetChatGroupId(),
		ChatID:      msg.GetChatId(),
		SenderID:    msg.GetSteamidSender(),
		Message:     msg.GetMessage(),
		Timestamp:   time.Unix(int64(msg.GetTimestamp()), 0),
	})
}
