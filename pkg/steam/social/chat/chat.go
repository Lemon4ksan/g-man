// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package chat manages one-on-one friend messages and Steam group chats.
package chat

import (
	"context"
	"errors"
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

// messageInterval is the minimum time between sent messages to avoid rate limits.
const messageInterval = 1200 * time.Millisecond

// ErrNotInGroupChat is returned when trying to perform an operation on a group chat that the bot is not a member of.
var ErrNotInGroupChat = errors.New("chat: not currently in this group chat")

// WithModule returns a steam.Option that registers the chat module in the client.
func WithModule() steam.Option {
	return func(c *steam.Client) {
		c.RegisterModule(New())
	}
}

// From returns the chat module from the client.
func From(c *steam.Client) *Chat {
	return steam.GetModule[*Chat](c)
}

// Chat handles sending and receiving messages via Steam's Unified Services.
type Chat struct {
	module.Base

	// Dependencies
	service service.Doer

	// State management
	stateMu          sync.RWMutex
	steamID          id.ID
	botAccountID     uint32            // Cached 32-bit account ID for fast comparison
	activeGroupChats map[uint64]uint64 // GroupID -> ChatID
	unregFuncs       []func()

	// Rate limiting
	rateLimitMu     sync.Mutex
	lastMessageTime time.Time
}

// New creates a new instance of the chat manager.
func New() *Chat {
	return &Chat{
		Base:             module.New(ModuleName),
		activeGroupChats: make(map[uint64]uint64),
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

// StartAuthed updates the user's SteamID and starts offline message sync.
func (m *Chat) StartAuthed(ctx context.Context, auth module.AuthContext) error {
	m.stateMu.Lock()
	m.steamID = auth.SteamID()
	m.botAccountID = m.steamID.AccountID() // Extract 32-bit ID
	m.stateMu.Unlock()

	// Start synchronization in the background to not block the client startup.
	m.Go(func(ctx context.Context) {
		m.synchronizeOfflineMessages(ctx)
	})

	return nil
}

// Close ensures all service handlers are removed and background tasks are stopped.
func (m *Chat) Close() error {
	m.stateMu.Lock()
	for _, unreg := range m.unregFuncs {
		unreg()
	}

	m.unregFuncs = nil
	m.stateMu.Unlock()

	return m.Base.Close()
}

// SendMessage sends a plain text message to a specific Steam user.
func (m *Chat) SendMessage(ctx context.Context, steamID uint64, text string) error {
	if err := m.applyRateLimit(); err != nil {
		return err
	}

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

// GetRecentMessages retrieves the chat history with a specific friend.
func (m *Chat) GetRecentMessages(
	ctx context.Context, steamID uint64, count uint32,
) ([]*pb.CFriendMessages_GetRecentMessages_Response_FriendMessage, error) {
	m.stateMu.RLock()
	myID := m.steamID
	m.stateMu.RUnlock()

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

// JoinGroupChat enters a group chat room. The internal state will be updated.
func (m *Chat) JoinGroupChat(ctx context.Context, groupID uint64) error {
	req := &pb.CChatRoom_JoinChatRoomGroup_Request{ChatGroupId: proto.Uint64(groupID)}

	resp, err := service.Unified[pb.CChatRoom_JoinChatRoomGroup_Response](ctx, m.service, req)
	if err != nil {
		return err
	}

	m.stateMu.Lock()
	m.activeGroupChats[groupID] = resp.GetJoinChatId()
	m.stateMu.Unlock()

	return nil
}

// LeaveGroupChat exits a group chat room.
func (m *Chat) LeaveGroupChat(ctx context.Context, groupID uint64) error {
	m.stateMu.RLock()
	_, ok := m.activeGroupChats[groupID]
	m.stateMu.RUnlock()

	if !ok {
		return ErrNotInGroupChat
	}

	req := &pb.CChatRoom_LeaveChatRoomGroup_Request{
		ChatGroupId: proto.Uint64(groupID),
	}

	_, err := service.Unified[service.NoResponse](ctx, m.service, req)
	if err == nil {
		m.stateMu.Lock()
		delete(m.activeGroupChats, groupID)
		m.stateMu.Unlock()
	}

	return err
}

// SendGroupMessage sends a text message to a Steam group chatroom.
func (m *Chat) SendGroupMessage(ctx context.Context, groupID uint64, text string) error {
	m.stateMu.RLock()
	chatID, ok := m.activeGroupChats[groupID]
	m.stateMu.RUnlock()

	if !ok {
		return ErrNotInGroupChat
	}

	if err := m.applyRateLimit(); err != nil {
		return err
	}

	req := &pb.CChatRoom_SendChatMessage_Request{
		ChatGroupId: proto.Uint64(groupID),
		ChatId:      proto.Uint64(chatID),
		Message:     proto.String(text),
	}
	_, err := service.Unified[service.NoResponse](ctx, m.service, req)

	return err
}

// DeleteGroupMessages deletes messages from a group chatroom.
func (m *Chat) DeleteGroupMessages(
	ctx context.Context,
	groupID uint64,
	messages []*pb.CChatRoom_DeleteChatMessages_Request_Message,
) error {
	m.stateMu.RLock()
	chatID, ok := m.activeGroupChats[groupID]
	m.stateMu.RUnlock()

	if !ok {
		return ErrNotInGroupChat
	}

	if err := m.applyRateLimit(); err != nil {
		return err
	}

	req := &pb.CChatRoom_DeleteChatMessages_Request{
		ChatGroupId: proto.Uint64(groupID),
		ChatId:      proto.Uint64(chatID),
		Messages:    messages,
	}
	_, err := service.Unified[pb.CChatRoom_DeleteChatMessages_Response](ctx, m.service, req)

	return err
}

// AckGroupMessage marks all messages in a group chat as read up to a given timestamp.
func (m *Chat) AckGroupMessage(ctx context.Context, groupID, chatID uint64, timestamp uint32) error {
	req := &pb.CChatRoom_AckChatMessage_Notification{
		ChatGroupId: proto.Uint64(groupID),
		ChatId:      proto.Uint64(chatID),
		Timestamp:   proto.Uint32(timestamp),
	}
	_, err := service.Unified[service.NoResponse](ctx, m.service, req)

	return err
}

// handleIncomingMessage dispatches friend messages into the event bus.
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
	timestamp := time.Unix(int64(msg.GetRtime32ServerTimestamp()), 0)

	switch msg.GetChatEntryType() {
	case ChatEntryTypeChatMsg, ChatEntryTypeEmote:
		m.Bus.Publish(&MessageEvent{
			SenderID:  senderID,
			Message:   msg.GetMessage(),
			Timestamp: timestamp,
			Ordinal:   msg.GetOrdinal(),
		})

	case ChatEntryTypeSticker:
		m.Bus.Publish(&StickerEvent{
			SenderID:  senderID,
			StickerID: msg.GetMessage(), // The message body contains sticker data
			Timestamp: timestamp,
		})

	case ChatEntryTypeTyping:
		m.Bus.Publish(&TypingEvent{SenderID: senderID})
	default:
		m.Logger.Debug("Received unhandled chat entry type", log.Int32("type", msg.GetChatEntryType()))
	}
}

// handleGroupMessage dispatches group messages and updates internal state.
func (m *Chat) handleGroupMessage(packet *protocol.Packet) {
	msg := &pb.CChatRoom_IncomingChatMessage_Notification{}
	if err := proto.Unmarshal(packet.Payload, msg); err != nil {
		m.Logger.Error("Failed to unmarshal incoming group message", log.Err(err))
		return
	}

	// Update our state in case we joined this chat from another client.
	m.stateMu.Lock()
	m.activeGroupChats[msg.GetChatGroupId()] = msg.GetChatId()
	m.stateMu.Unlock()

	m.Bus.Publish(&GroupMessageEvent{
		ChatGroupID: msg.GetChatGroupId(),
		ChatID:      msg.GetChatId(),
		SenderID:    msg.GetSteamidSender(),
		Message:     msg.GetMessage(),
		Timestamp:   time.Unix(int64(msg.GetTimestamp()), 0),
	})
}

// synchronizeOfflineMessages fetches unread messages for all active chat sessions.
func (m *Chat) synchronizeOfflineMessages(ctx context.Context) {
	sessionsResp, err := service.Unified[pb.CFriendsMessages_GetActiveMessageSessions_Response](
		ctx,
		m.service,
		&pb.CFriendsMessages_GetActiveMessageSessions_Request{},
	)
	if err != nil {
		m.Logger.Error("Failed to get active message sessions", log.Err(err))
		return
	}

	m.stateMu.RLock()
	botAccID := m.botAccountID
	m.stateMu.RUnlock()

	for _, session := range sessionsResp.GetMessageSessions() {
		if session.GetLastMessage() > session.GetLastView() {
			friendID := session.GetAccountidFriend()
			m.Logger.Debug("Found unread messages", log.Uint32("from", friendID))

			history, err := m.GetRecentMessages(ctx, uint64(friendID), 50)
			if err != nil {
				m.Logger.Error("Failed to fetch history for sync", log.Uint32("friend", friendID), log.Err(err))
				continue
			}

			var lastTimestamp uint32
			for _, msg := range history {
				// Use Accountid to filter out bot's own messages in history
				if msg.GetAccountid() == botAccID {
					continue
				}

				if msg.GetTimestamp() > session.GetLastView() {
					m.Bus.Publish(&MessageEvent{
						SenderID:  uint64(friendID),
						Message:   msg.GetMessage(),
						Timestamp: time.Unix(int64(msg.GetTimestamp()), 0),
						Ordinal:   msg.GetOrdinal(),
					})
				}

				if msg.GetTimestamp() > lastTimestamp {
					lastTimestamp = msg.GetTimestamp()
				}
			}

			if lastTimestamp > 0 {
				_ = m.AckFriendMessage(ctx, uint64(friendID), lastTimestamp)
			}
		}
	}
}

// applyRateLimit blocks for a short duration if messages are sent too frequently.
func (m *Chat) applyRateLimit() error {
	m.rateLimitMu.Lock()
	defer m.rateLimitMu.Unlock()

	since := time.Since(m.lastMessageTime)
	if since < messageInterval {
		time.Sleep(messageInterval - since)
	}

	m.lastMessageTime = time.Now()

	return nil
}
