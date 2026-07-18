// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package chat manages one-on-one friend messages and Steam group chats.
package chat

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lemon4ksan/miyako/log"
	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
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
	return steam.WithModule(New())
}

// From returns the chat module from the client.
func From(c *steam.Client) *Chat {
	return steam.GetModule[*Chat](c)
}

// Chat handles sending and receiving messages via Steam's Unified Services.
//
// It provides comprehensive methods for managing private sessions, group chats,
// and invite links. During authentication, it automatically synchronizes unread
// offline messages using [Chat.synchronizeOfflineMessages] in a background goroutine.
//
// Create and register new instances of Chat using the [New] constructor.
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
func (c *Chat) Init(init module.InitContext) error {
	if err := c.Base.Init(init); err != nil {
		return err
	}

	c.service = init.Service()

	friendHandler := "FriendMessagesClient.IncomingMessage#1"
	groupHandler := "ChatRoomClient.NotifyIncomingChatMessage#1"
	friendReactionHandler := "FriendMessagesClient.MessageReaction#1"
	groupReactionHandler := "ChatRoomClient.NotifyMessageReaction#1"

	init.RegisterServiceHandler(friendHandler, c.handleIncomingMessage)
	init.RegisterServiceHandler(groupHandler, c.handleGroupMessage)
	init.RegisterServiceHandler(friendReactionHandler, c.handleFriendReaction)
	init.RegisterServiceHandler(groupReactionHandler, c.handleGroupReaction)
	init.RegisterPacketHandler(enums.EMsg_ClientFriendMsgIncoming, c.handleLegacyFriendMsg)

	c.unregFuncs = append(c.unregFuncs, func() {
		init.UnregisterServiceHandler(friendHandler)
		init.UnregisterServiceHandler(groupHandler)
		init.UnregisterServiceHandler(friendReactionHandler)
		init.UnregisterServiceHandler(groupReactionHandler)
		init.UnregisterPacketHandler(enums.EMsg_ClientFriendMsgIncoming)
	})

	return nil
}

// StartAuthed updates the user's SteamID and starts offline message sync.
func (c *Chat) StartAuthed(ctx context.Context, auth module.AuthContext) error {
	c.stateMu.Lock()
	c.steamID = auth.SteamID()
	c.botAccountID = c.steamID.AccountID() // Extract 32-bit ID
	c.stateMu.Unlock()

	// Start synchronization in the background to not block the client startup.
	c.Go(func(ctx context.Context) {
		c.synchronizeOfflineMessages(ctx)
	})

	return nil
}

// Close ensures all service handlers are removed and background tasks are stopped.
func (c *Chat) Close() error {
	c.stateMu.Lock()
	for _, unreg := range c.unregFuncs {
		unreg()
	}

	c.unregFuncs = nil
	c.stateMu.Unlock()

	return c.Base.Close()
}

// SendMessage sends a plain text message to a specific Steam user.
//
// It blocks and waits if messages are being sent faster than the configured
// safety interval of 1.2 seconds.
func (c *Chat) SendMessage(ctx context.Context, steamID uint64, text string) error {
	if err := c.applyRateLimit(); err != nil {
		return err
	}

	req := &pb.CFriendMessages_SendMessage_Request{
		Steamid:        proto.Uint64(steamID),
		ChatEntryType:  proto.Int32(ChatEntryTypeChatMsg),
		Message:        proto.String(text),
		ContainsBbcode: proto.Bool(true),
	}
	_, err := service.Unified[service.NoResponse](ctx, c.service, req)

	return err
}

// SendTyping notifies a friend that the bot is currently typing a message.
func (c *Chat) SendTyping(ctx context.Context, steamID uint64) error {
	req := &pb.CFriendMessages_SendMessage_Request{
		Steamid:       proto.Uint64(steamID),
		ChatEntryType: proto.Int32(ChatEntryTypeTyping),
	}
	_, err := service.Unified[service.NoResponse](ctx, c.service, req)

	return err
}

// AckFriendMessage marks all messages from a specific friend up to the timestamp as read.
func (c *Chat) AckFriendMessage(ctx context.Context, steamID uint64, timestamp uint32) error {
	req := &pb.CFriendMessages_AckMessage_Notification{
		SteamidPartner: proto.Uint64(steamID),
		Timestamp:      proto.Uint32(timestamp),
	}
	_, err := service.Unified[service.NoResponse](ctx, c.service, req)

	return err
}

// GetRecentMessages retrieves the chat history with a specific friend.
func (c *Chat) GetRecentMessages(
	ctx context.Context, steamID uint64, count uint32,
) ([]*pb.CFriendMessages_GetRecentMessages_Response_FriendMessage, error) {
	c.stateMu.RLock()
	myID := c.steamID
	c.stateMu.RUnlock()

	req := &pb.CFriendMessages_GetRecentMessages_Request{
		Steamid1:     proto.Uint64(myID.Uint64()),
		Steamid2:     proto.Uint64(steamID),
		Count:        proto.Uint32(count),
		BbcodeFormat: proto.Bool(true),
	}

	resp, err := service.Unified[pb.CFriendMessages_GetRecentMessages_Response](ctx, c.service, req)
	if err != nil {
		return nil, err
	}

	return resp.GetMessages(), nil
}

// SendChatMessage sends a message to a specific modern chat room group channel.
//
// It blocks and waits if messages are being sent faster than the configured
// safety interval of 1.2 seconds.
func (c *Chat) SendChatMessage(ctx context.Context, chatGroupID, chatID uint64, message string) error {
	if err := c.applyRateLimit(); err != nil {
		return err
	}

	req := &pb.CChatRoom_SendChatMessage_Request{
		ChatGroupId: proto.Uint64(chatGroupID),
		ChatId:      proto.Uint64(chatID),
		Message:     proto.String(message),
	}

	_, err := service.Unified[pb.CChatRoom_SendChatMessage_Response](ctx, c.service, req)

	return err
}

// SendChatReaction updates (adds or removes) a reaction to a specific message in a modern chat room channel.
func (c *Chat) SendChatReaction(
	ctx context.Context,
	chatGroupID, chatID uint64,
	serverTimestamp, ordinal uint32,
	reaction string,
	reactionType pb.EChatRoomMessageReactionType,
	isAdd bool,
) error {
	req := &pb.CChatRoom_UpdateMessageReaction_Request{
		ChatGroupId:     proto.Uint64(chatGroupID),
		ChatId:          proto.Uint64(chatID),
		ServerTimestamp: proto.Uint32(serverTimestamp),
		Ordinal:         proto.Uint32(ordinal),
		ReactionType:    &reactionType,
		Reaction:        proto.String(reaction),
		IsAdd:           proto.Bool(isAdd),
	}

	_, err := service.Unified[pb.CChatRoom_UpdateMessageReaction_Response](ctx, c.service, req)

	return err
}

// GetChatHistory retrieves chat history for a given modern chat room group channel with pagination options.
func (c *Chat) GetChatHistory(
	ctx context.Context,
	chatGroupID, chatID uint64,
	startTime, startOrdinal, maxCount uint32,
) ([]*pb.CChatRoom_GetMessageHistory_Response_ChatMessage, error) {
	req := &pb.CChatRoom_GetMessageHistory_Request{
		ChatGroupId:  proto.Uint64(chatGroupID),
		ChatId:       proto.Uint64(chatID),
		StartTime:    proto.Uint32(startTime),
		StartOrdinal: proto.Uint32(startOrdinal),
		MaxCount:     proto.Uint32(maxCount),
	}

	resp, err := service.Unified[pb.CChatRoom_GetMessageHistory_Response](ctx, c.service, req)
	if err != nil {
		return nil, err
	}

	return resp.GetMessages(), nil
}

// JoinGroupChat enters a group chat room.
//
// It updates the internal active group chats state and returns an error
// if the WebAPI request fails.
func (c *Chat) JoinGroupChat(ctx context.Context, groupID uint64) error {
	req := &pb.CChatRoom_JoinChatRoomGroup_Request{ChatGroupId: proto.Uint64(groupID)}

	resp, err := service.Unified[pb.CChatRoom_JoinChatRoomGroup_Response](ctx, c.service, req)
	if err != nil {
		return err
	}

	c.stateMu.Lock()
	c.activeGroupChats[groupID] = resp.GetJoinChatId()
	c.stateMu.Unlock()

	return nil
}

// LeaveGroupChat exits a group chat room.
//
// It returns [ErrNotInGroupChat] if the bot is not currently a member
// of the specified group chatroom.
func (c *Chat) LeaveGroupChat(ctx context.Context, groupID uint64) error {
	c.stateMu.RLock()
	_, ok := c.activeGroupChats[groupID]
	c.stateMu.RUnlock()

	if !ok {
		return ErrNotInGroupChat
	}

	req := &pb.CChatRoom_LeaveChatRoomGroup_Request{
		ChatGroupId: proto.Uint64(groupID),
	}

	_, err := service.Unified[service.NoResponse](ctx, c.service, req)
	if err == nil {
		c.stateMu.Lock()
		delete(c.activeGroupChats, groupID)
		c.stateMu.Unlock()
	}

	return err
}

// SendGroupMessage sends a text message to a Steam group chatroom.
//
// It blocks and waits if messages are being sent faster than the configured
// safety interval of 1.2 seconds. It returns [ErrNotInGroupChat] if the bot
// is not a member of the group chatroom.
func (c *Chat) SendGroupMessage(ctx context.Context, groupID uint64, text string) error {
	c.stateMu.RLock()
	chatID, ok := c.activeGroupChats[groupID]
	c.stateMu.RUnlock()

	if !ok {
		return ErrNotInGroupChat
	}

	if err := c.applyRateLimit(); err != nil {
		return err
	}

	req := &pb.CChatRoom_SendChatMessage_Request{
		ChatGroupId: proto.Uint64(groupID),
		ChatId:      proto.Uint64(chatID),
		Message:     proto.String(text),
	}
	_, err := service.Unified[service.NoResponse](ctx, c.service, req)

	return err
}

// DeleteGroupMessages deletes messages from a group chatroom.
func (c *Chat) DeleteGroupMessages(
	ctx context.Context,
	groupID uint64,
	messages []*pb.CChatRoom_DeleteChatMessages_Request_Message,
) error {
	c.stateMu.RLock()
	chatID, ok := c.activeGroupChats[groupID]
	c.stateMu.RUnlock()

	if !ok {
		return ErrNotInGroupChat
	}

	if err := c.applyRateLimit(); err != nil {
		return err
	}

	req := &pb.CChatRoom_DeleteChatMessages_Request{
		ChatGroupId: proto.Uint64(groupID),
		ChatId:      proto.Uint64(chatID),
		Messages:    messages,
	}
	_, err := service.Unified[pb.CChatRoom_DeleteChatMessages_Response](ctx, c.service, req)

	return err
}

// AckGroupMessage marks all messages in a group chat as read up to a given timestamp.
func (c *Chat) AckGroupMessage(ctx context.Context, groupID, chatID uint64, timestamp uint32) error {
	req := &pb.CChatRoom_AckChatMessage_Notification{
		ChatGroupId: proto.Uint64(groupID),
		ChatId:      proto.Uint64(chatID),
		Timestamp:   proto.Uint32(timestamp),
	}
	_, err := service.Unified[service.NoResponse](ctx, c.service, req)

	return err
}

// GetGroupMessageHistory retrieves chat history for a group chatroom.
func (c *Chat) GetGroupMessageHistory(
	ctx context.Context,
	groupID uint64,
	maxCount uint32,
) ([]*pb.CChatRoom_GetMessageHistory_Response_ChatMessage, error) {
	c.stateMu.RLock()
	chatID, ok := c.activeGroupChats[groupID]
	c.stateMu.RUnlock()

	if !ok {
		return nil, ErrNotInGroupChat
	}

	req := &pb.CChatRoom_GetMessageHistory_Request{
		ChatGroupId: proto.Uint64(groupID),
		ChatId:      proto.Uint64(chatID),
		MaxCount:    proto.Uint32(maxCount),
	}

	resp, err := service.Unified[pb.CChatRoom_GetMessageHistory_Response](ctx, c.service, req)
	if err != nil {
		return nil, err
	}

	return resp.GetMessages(), nil
}

// InviteFriendToGroupChat invites a friend to a Steam group chatroom.
func (c *Chat) InviteFriendToGroupChat(ctx context.Context, groupID, friendSteamID uint64) error {
	c.stateMu.RLock()
	chatID, ok := c.activeGroupChats[groupID]
	c.stateMu.RUnlock()

	if !ok {
		return ErrNotInGroupChat
	}

	req := &pb.CChatRoom_InviteFriendToChatRoomGroup_Request{
		ChatGroupId: proto.Uint64(groupID),
		ChatId:      proto.Uint64(chatID),
		Steamid:     proto.Uint64(friendSteamID),
	}
	_, err := service.Unified[pb.CChatRoom_InviteFriendToChatRoomGroup_Response](ctx, c.service, req)

	return err
}

// KickUserFromGroupChat removes a user from a Steam group chatroom.
func (c *Chat) KickUserFromGroupChat(
	ctx context.Context,
	groupID, targetSteamID uint64,
	expirationSeconds int32,
) error {
	c.stateMu.RLock()
	_, ok := c.activeGroupChats[groupID]
	c.stateMu.RUnlock()

	if !ok {
		return ErrNotInGroupChat
	}

	req := &pb.CChatRoom_KickUser_Request{
		ChatGroupId: proto.Uint64(groupID),
		Steamid:     proto.Uint64(targetSteamID),
		Expiration:  proto.Int32(expirationSeconds),
	}
	_, err := service.Unified[pb.CChatRoom_KickUser_Response](ctx, c.service, req)

	return err
}

// MuteUserInGroupChat mutes a user in a Steam group chatroom.
func (c *Chat) MuteUserInGroupChat(ctx context.Context, groupID, targetSteamID uint64, expirationSeconds int32) error {
	c.stateMu.RLock()
	_, ok := c.activeGroupChats[groupID]
	c.stateMu.RUnlock()

	if !ok {
		return ErrNotInGroupChat
	}

	req := &pb.CChatRoom_MuteUser_Request{
		ChatGroupId: proto.Uint64(groupID),
		Steamid:     proto.Uint64(targetSteamID),
		Expiration:  proto.Int32(expirationSeconds),
	}
	_, err := service.Unified[pb.CChatRoom_MuteUser_Response](ctx, c.service, req)

	return err
}

// SetUserBanStateInGroupChat bans or unbans a user in a Steam group chatroom.
func (c *Chat) SetUserBanStateInGroupChat(ctx context.Context, groupID, targetSteamID uint64, ban bool) error {
	c.stateMu.RLock()
	_, ok := c.activeGroupChats[groupID]
	c.stateMu.RUnlock()

	if !ok {
		return ErrNotInGroupChat
	}

	req := &pb.CChatRoom_SetUserBanState_Request{
		ChatGroupId: proto.Uint64(groupID),
		Steamid:     proto.Uint64(targetSteamID),
		BanState:    proto.Bool(ban),
	}
	_, err := service.Unified[pb.CChatRoom_SetUserBanState_Response](ctx, c.service, req)

	return err
}

// CreateChatRoomGroup creates a new chat room group and invites people to join it.
// If name is empty, it creates an "ad-hoc" group chat.
func (c *Chat) CreateChatRoomGroup(
	ctx context.Context,
	name string,
	inviteeSteamIDs []uint64,
) (*pb.CChatRoom_CreateChatRoomGroup_Response, error) {
	req := &pb.CChatRoom_CreateChatRoomGroup_Request{
		Name:            proto.String(name),
		SteamidInvitees: inviteeSteamIDs,
	}

	return service.Unified[pb.CChatRoom_CreateChatRoomGroup_Response](ctx, c.service, req)
}

// SaveChatRoomGroup saves an unnamed "ad-hoc" group chat and converts it into a full chat room group.
func (c *Chat) SaveChatRoomGroup(ctx context.Context, groupID uint64, name string) error {
	req := &pb.CChatRoom_SaveChatRoomGroup_Request{
		ChatGroupId: proto.Uint64(groupID),
		Name:        proto.String(name),
	}
	_, err := service.Unified[service.NoResponse](ctx, c.service, req)

	return err
}

// RenameChatRoomGroup renames a saved chat room group.
func (c *Chat) RenameChatRoomGroup(ctx context.Context, groupID uint64, newName string) (string, error) {
	req := &pb.CChatRoom_RenameChatRoomGroup_Request{
		ChatGroupId: proto.Uint64(groupID),
		Name:        proto.String(newName),
	}

	resp, err := service.Unified[pb.CChatRoom_RenameChatRoomGroup_Response](ctx, c.service, req)
	if err != nil {
		return "", err
	}

	return resp.GetName(), nil
}

// GetMyChatRoomGroups retrieves a list of all the chat room groups the bot is currently in.
func (c *Chat) GetMyChatRoomGroups(ctx context.Context) (*pb.CChatRoom_GetMyChatRoomGroups_Response, error) {
	req := &pb.CChatRoom_GetMyChatRoomGroups_Request{}
	return service.Unified[pb.CChatRoom_GetMyChatRoomGroups_Response](ctx, c.service, req)
}

// GetChatRoomGroupState retrieves the detailed state of a specific chat room group.
func (c *Chat) GetChatRoomGroupState(
	ctx context.Context,
	groupID uint64,
) (*pb.CChatRoom_GetChatRoomGroupState_Response, error) {
	req := &pb.CChatRoom_GetChatRoomGroupState_Request{
		ChatGroupId: proto.Uint64(groupID),
	}

	return service.Unified[pb.CChatRoom_GetChatRoomGroupState_Response](ctx, c.service, req)
}

// CreateInviteLink creates an invite link for a given chat group.
// voiceChatID is optional (can be 0 if not targeting a specific voice chat).
func (c *Chat) CreateInviteLink(
	ctx context.Context,
	groupID uint64,
	secondsValid uint32,
	voiceChatID uint64,
) (*pb.CChatRoom_CreateInviteLink_Response, error) {
	req := &pb.CChatRoom_CreateInviteLink_Request{
		ChatGroupId:  proto.Uint64(groupID),
		SecondsValid: proto.Uint32(secondsValid),
	}
	if voiceChatID > 0 {
		req.ChatId = proto.Uint64(voiceChatID)
	}

	return service.Unified[pb.CChatRoom_CreateInviteLink_Response](ctx, c.service, req)
}

// GetInviteLinksForGroup gets all active invite links for a given chat group.
func (c *Chat) GetInviteLinksForGroup(
	ctx context.Context,
	groupID uint64,
) ([]*pb.CChatRoom_GetInviteLinksForGroup_Response_LinkInfo, error) {
	req := &pb.CChatRoom_GetInviteLinksForGroup_Request{
		ChatGroupId: proto.Uint64(groupID),
	}

	resp, err := service.Unified[pb.CChatRoom_GetInviteLinksForGroup_Response](ctx, c.service, req)
	if err != nil {
		return nil, err
	}

	return resp.GetInviteLinks(), nil
}

// DeleteInviteLink revokes and deletes an active invite link by its code.
func (c *Chat) DeleteInviteLink(ctx context.Context, groupID uint64, inviteCode string) error {
	req := &pb.CChatRoom_DeleteInviteLink_Request{
		ChatGroupId: proto.Uint64(groupID),
		InviteCode:  proto.String(inviteCode),
	}
	_, err := service.Unified[service.NoResponse](ctx, c.service, req)

	return err
}

func (c *Chat) handleIncomingMessage(packet *protocol.Packet) {
	msg := &pb.CFriendMessages_IncomingMessage_Notification{}
	if err := proto.Unmarshal(packet.Payload, msg); err != nil {
		c.Logger.Error("Failed to unmarshal incoming friend message", log.Err(err))
		return
	}

	if msg.GetLocalEcho() {
		return // Ignore our own messages reflected by the server
	}

	senderID := msg.GetSteamidFriend()
	timestamp := time.Unix(int64(msg.GetRtime32ServerTimestamp()), 0)

	switch msg.GetChatEntryType() {
	case ChatEntryTypeChatMsg, ChatEntryTypeEmote:
		evt := &MessageEvent{
			SenderID:  senderID,
			Message:   msg.GetMessage(),
			Timestamp: timestamp,
			Ordinal:   msg.GetOrdinal(),
		}
		evt.SetContext(packet.Context())
		c.Bus.Publish(evt)

	case ChatEntryTypeSticker:
		evt := &StickerEvent{
			SenderID:  senderID,
			StickerID: msg.GetMessage(), // The message body contains sticker data
			Timestamp: timestamp,
		}
		evt.SetContext(packet.Context())
		c.Bus.Publish(evt)

	case ChatEntryTypeTyping:
		evt := &TypingEvent{SenderID: senderID}
		evt.SetContext(packet.Context())
		c.Bus.Publish(evt)
	default:
		c.Logger.DebugContext(
			packet.Context(),
			"Received unhandled chat entry type",
			log.Int32("type", msg.GetChatEntryType()),
		)
	}
}

func (c *Chat) handleGroupMessage(packet *protocol.Packet) {
	msg := &pb.CChatRoom_IncomingChatMessage_Notification{}
	if err := proto.Unmarshal(packet.Payload, msg); err != nil {
		c.Logger.ErrorContext(packet.Context(), "Failed to unmarshal incoming group message", log.Err(err))
		return
	}

	// Update our state in case we joined this chat from another client.
	c.stateMu.Lock()
	c.activeGroupChats[msg.GetChatGroupId()] = msg.GetChatId()
	c.stateMu.Unlock()

	evt := &GroupMessageEvent{
		ChatGroupID: msg.GetChatGroupId(),
		ChatID:      msg.GetChatId(),
		SenderID:    msg.GetSteamidSender(),
		Message:     msg.GetMessage(),
		Timestamp:   time.Unix(int64(msg.GetTimestamp()), 0),
	}
	evt.SetContext(packet.Context())
	c.Bus.Publish(evt)
}

func (c *Chat) handleFriendReaction(packet *protocol.Packet) {
	msg := &pb.CFriendMessages_MessageReaction_Notification{}
	if err := proto.Unmarshal(packet.Payload, msg); err != nil {
		c.Logger.Error("Failed to unmarshal friend reaction notification", log.Err(err))
		return
	}

	c.Bus.Publish(&ReactionEvent{
		FriendSteamID:   msg.GetSteamidFriend(),
		ReactorSteamID:  msg.GetReactor(),
		ServerTimestamp: msg.GetServerTimestamp(),
		Ordinal:         msg.GetOrdinal(),
		Reaction:        msg.GetReaction(),
		ReactionType:    int32(msg.GetReactionType()),
		IsAdd:           msg.GetIsAdd(),
	})
}

func (c *Chat) handleGroupReaction(packet *protocol.Packet) {
	msg := &pb.CChatRoom_MessageReaction_Notification{}
	if err := proto.Unmarshal(packet.Payload, msg); err != nil {
		c.Logger.Error("Failed to unmarshal group reaction notification", log.Err(err))
		return
	}

	c.Bus.Publish(&GroupReactionEvent{
		ChatGroupID:     msg.GetChatGroupId(),
		ChatID:          msg.GetChatId(),
		ReactorSteamID:  msg.GetReactor(),
		ServerTimestamp: msg.GetServerTimestamp(),
		Ordinal:         msg.GetOrdinal(),
		Reaction:        msg.GetReaction(),
		ReactionType:    int32(msg.GetReactionType()),
		IsAdd:           msg.GetIsAdd(),
	})
}

func (c *Chat) synchronizeOfflineMessages(ctx context.Context) {
	req := &pb.CFriendsMessages_GetActiveMessageSessions_Request{
		OnlySessionsWithMessages: proto.Bool(true),
	}

	var (
		sessionsResp *pb.CFriendsMessages_GetActiveMessageSessions_Response
		err          error
	)

	for attempt := range 3 {
		sessionsResp, err = service.UnifiedExplicit[pb.CFriendsMessages_GetActiveMessageSessions_Response](
			ctx,
			c.service,
			http.MethodPost,
			"FriendMessages",
			"GetActiveMessageSessions",
			1,
			req,
		)
		if err == nil {
			break
		}

		if attempt < 2 {
			c.Logger.WarnContext(ctx, "Failed to get active message sessions, retrying",
				log.Err(err),
				log.Int("attempt", attempt+1),
			)

			backoff := time.Duration(1<<(attempt+1)) * time.Second
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
		}
	}

	if err != nil {
		c.Logger.WarnContext(ctx, "Failed to get active message sessions after retries", log.Err(err))
		return
	}

	c.stateMu.RLock()
	botAccID := c.botAccountID
	c.stateMu.RUnlock()

	for _, session := range sessionsResp.GetMessageSessions() {
		if session.GetLastMessage() > session.GetLastView() {
			friendID := id.FromAccountID(session.GetAccountidFriend())
			c.Logger.Debug("Found unread messages", log.Uint64("steam_id", friendID.Uint64()))

			history, err := c.GetRecentMessages(ctx, friendID.Uint64(), 50)
			if err != nil {
				c.Logger.Error(
					"Failed to fetch history for sync",
					log.Uint64("steam_id", friendID.Uint64()),
					log.Err(err),
				)

				continue
			}

			var lastTimestamp uint32
			for _, msg := range history {
				if msg.GetAccountid() == botAccID {
					continue
				}

				if msg.GetTimestamp() > session.GetLastView() {
					c.Bus.Publish(&MessageEvent{
						SenderID:  friendID.Uint64(),
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
				_ = c.AckFriendMessage(ctx, friendID.Uint64(), lastTimestamp)
			}
		}
	}
}

func (c *Chat) handleLegacyFriendMsg(packet *protocol.Packet) {
	msg := &pb.CMsgClientFriendMsgIncoming{}
	if err := proto.Unmarshal(packet.Payload, msg); err != nil {
		c.Logger.Error("Failed to unmarshal legacy friend message", log.Err(err))
		return
	}

	senderID := msg.GetSteamidFrom()
	timestamp := time.Unix(int64(msg.GetRtime32ServerTimestamp()), 0)
	msgText := strings.TrimRight(string(msg.GetMessage()), "\x00")

	switch msg.GetChatEntryType() {
	case ChatEntryTypeChatMsg, ChatEntryTypeEmote:
		evt := &MessageEvent{
			SenderID:  senderID,
			Message:   msgText,
			Timestamp: timestamp,
			Ordinal:   0,
		}
		evt.SetContext(packet.Context())
		c.Bus.Publish(evt)

	case ChatEntryTypeTyping:
		evt := &TypingEvent{SenderID: senderID}
		evt.SetContext(packet.Context())
		c.Bus.Publish(evt)
	default:
		c.Logger.DebugContext(
			packet.Context(),
			"Received unhandled legacy chat entry type",
			log.Int32("type", msg.GetChatEntryType()),
		)
	}
}

func (c *Chat) applyRateLimit() error {
	c.rateLimitMu.Lock()
	defer c.rateLimitMu.Unlock()

	since := time.Since(c.lastMessageTime)
	if since < messageInterval {
		time.Sleep(messageInterval - since)
	}

	c.lastMessageTime = time.Now()

	return nil
}
