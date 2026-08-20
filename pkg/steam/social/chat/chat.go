// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package chat manages 1-on-1 friend messages and modern Steam group chats via Unified Services.
package chat

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/async/log"
	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/client"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
)

const ModuleName string = "chat"

const messageInterval = 1200 * time.Millisecond

// ErrNotInGroupChat indicates an operation was performed on a group chat the bot is not currently joined to.
var ErrNotInGroupChat = errors.New("chat: not currently in this group chat")

// WithModule registers the Chat module in the client.
func WithModule() client.Option {
	return client.WithModule(New())
}

// From retrieves the Chat module instance from the client.
func From(c *client.Client) *Chat {
	return client.GetModule[*Chat](c)
}

var messageEventPool = sync.Pool{
	New: func() any {
		return &MessageEvent{}
	},
}

// AcquireMessageEvent fetches a pooled MessageEvent instance.
func AcquireMessageEvent(senderID uint64, msg string, ts time.Time, ordinal uint32) *MessageEvent {
	e := messageEventPool.Get().(*MessageEvent)
	e.SenderID = senderID
	e.Message = msg
	e.Timestamp = ts
	e.Ordinal = ordinal

	return e
}

// ReleaseMessageEvent recycles a MessageEvent back to memory pool.
func ReleaseMessageEvent(e *MessageEvent) {
	if e == nil {
		return
	}

	e.SenderID = 0
	e.Message = ""
	e.Ordinal = 0
	messageEventPool.Put(e)
}

// Chat manages friend messaging, group chat interactions, reactions, and offline message synchronization.
//
// Thread Safety:
//   - Safe for concurrent use across all public methods.
type Chat struct {
	module.Base

	events Events

	stateMu          sync.RWMutex
	steamID          id.ID
	botAccountID     uint32
	activeGroupChats map[uint64]uint64

	rateLimitMu     sync.Mutex
	lastMessageTime time.Time
}

// New constructs a Chat module instance.
func New() *Chat {
	return &Chat{
		Base:             module.New(ModuleName),
		activeGroupChats: make(map[uint64]uint64),
	}
}

func (c *Chat) Init(init module.InitContext) error {
	if err := c.Base.Init(init); err != nil {
		return err
	}

	c.events = NewEvents(init)

	c.events.OnIncomingMessage(c.handleIncomingMessage)
	c.events.OnIncomingGroupMessage(c.handleGroupMessage)
	c.events.OnFriendReaction(c.handleFriendReaction)
	c.events.OnGroupReaction(c.handleGroupReaction)
	c.events.OnLegacyFriendMsg(c.handleLegacyFriendMsg)

	return nil
}

func (c *Chat) StartAuthed(ctx context.Context, auth module.AuthContext) error {
	c.stateMu.Lock()
	c.steamID = auth.SteamID()
	c.botAccountID = c.steamID.AccountID()
	c.stateMu.Unlock()

	c.Go(func(ctx context.Context) {
		c.synchronizeOfflineMessages(ctx)
	})

	return nil
}

func (c *Chat) Close() error {
	if c.events != nil {
		_ = c.events.Close()
	}

	return c.Base.Close()
}

// SendMessage transmits a private chat message to a friend, enforcing safety rate limits.
func (c *Chat) SendMessage(ctx context.Context, steamID uint64, text string) error {
	if err := c.applyRateLimit(); err != nil {
		return err
	}

	entryType := int32(ChatEntryTypeChatMsg)
	req := &pb.CFriendMessages_SendMessage_Request{
		Steamid:        &steamID,
		ChatEntryType:  &entryType,
		Message:        &text,
		ContainsBbcode: generic.Ptr[bool](true),
	}

	_, err := c.events.SendMessage(ctx, req)

	return err
}

// SendTyping sends a typing indicator signal to a friend.
func (c *Chat) SendTyping(ctx context.Context, steamID uint64) error {
	req := &pb.CFriendMessages_SendMessage_Request{
		Steamid:       proto.Uint64(steamID),
		ChatEntryType: proto.Int32(ChatEntryTypeTyping),
	}

	_, err := c.events.SendMessage(ctx, req)

	return err
}

// AckFriendMessage acknowledges received messages up to timestamp.
func (c *Chat) AckFriendMessage(ctx context.Context, steamID uint64, timestamp uint32) error {
	req := &pb.CFriendMessages_AckMessage_Notification{
		SteamidPartner: proto.Uint64(steamID),
		Timestamp:      proto.Uint32(timestamp),
	}

	return c.events.AckFriendMessage(ctx, req)
}

// GetRecentMessages fetches chat history logs with a friend.
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

	resp, err := c.events.GetRecentMessages(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp.GetMessages(), nil
}

// SendChatMessage sends a message to a specific chat room channel in a group.
func (c *Chat) SendChatMessage(ctx context.Context, chatGroupID, chatID uint64, message string) error {
	if err := c.applyRateLimit(); err != nil {
		return err
	}

	req := &pb.CChatRoom_SendChatMessage_Request{
		ChatGroupId: proto.Uint64(chatGroupID),
		ChatId:      proto.Uint64(chatID),
		Message:     proto.String(message),
	}

	_, err := c.events.SendChatMessage(ctx, req)

	return err
}

// SendChatReaction adds or removes an emoji reaction on a message in a group chat channel.
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

	_, err := c.events.UpdateMessageReaction(ctx, req)

	return err
}

// GetChatHistory fetches chat history records for a group chat channel.
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

	resp, err := c.events.GetMessageHistory(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp.GetMessages(), nil
}

// JoinGroupChat enters a group chat room.
func (c *Chat) JoinGroupChat(ctx context.Context, groupID uint64) error {
	req := &pb.CChatRoom_JoinChatRoomGroup_Request{ChatGroupId: proto.Uint64(groupID)}

	resp, err := c.events.JoinChatRoomGroup(ctx, req)
	if err != nil {
		return err
	}

	c.stateMu.Lock()
	c.activeGroupChats[groupID] = resp.GetJoinChatId()
	c.stateMu.Unlock()

	return nil
}

// LeaveGroupChat exits a group chat room.
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

	_, err := c.events.LeaveChatRoomGroup(ctx, req)
	if err == nil {
		c.stateMu.Lock()
		delete(c.activeGroupChats, groupID)
		c.stateMu.Unlock()
	}

	return err
}

// SendGroupMessage sends a message to the active main channel of a group chat.
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

	_, err := c.events.SendChatMessage(ctx, req)

	return err
}

// DeleteGroupMessages deletes messages from a group chat channel.
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

	_, err := c.events.DeleteChatMessages(ctx, req)

	return err
}

// AckGroupMessage acknowledges received group messages up to timestamp.
func (c *Chat) AckGroupMessage(ctx context.Context, groupID, chatID uint64, timestamp uint32) error {
	req := &pb.CChatRoom_AckChatMessage_Notification{
		ChatGroupId: proto.Uint64(groupID),
		ChatId:      proto.Uint64(chatID),
		Timestamp:   proto.Uint32(timestamp),
	}

	return c.events.AckChatMessage(ctx, req)
}

// GetGroupMessageHistory fetches message history for a group chat room.
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

	resp, err := c.events.GetMessageHistory(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp.GetMessages(), nil
}

// InviteFriendToGroupChat invites a friend to a group chat room.
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

	_, err := c.events.InviteFriendToChatRoomGroup(ctx, req)

	return err
}

// KickUserFromGroupChat kicks a user from a group chat room.
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

	_, err := c.events.KickUser(ctx, req)

	return err
}

// MuteUserInGroupChat mutes a user in a group chat room.
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

	_, err := c.events.MuteUser(ctx, req)

	return err
}

// SetUserBanStateInGroupChat bans or unbans a user in a group chat room.
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

	_, err := c.events.SetUserBanState(ctx, req)

	return err
}

// CreateChatRoomGroup creates a new group chat room and invites candidate users.
func (c *Chat) CreateChatRoomGroup(
	ctx context.Context,
	name string,
	inviteeSteamIDs []uint64,
) (*pb.CChatRoom_CreateChatRoomGroup_Response, error) {
	req := &pb.CChatRoom_CreateChatRoomGroup_Request{
		Name:            proto.String(name),
		SteamidInvitees: inviteeSteamIDs,
	}

	return c.events.CreateChatRoomGroup(ctx, req)
}

// SaveChatRoomGroup converts an ad-hoc group chat into a saved named group chat.
func (c *Chat) SaveChatRoomGroup(ctx context.Context, groupID uint64, name string) error {
	req := &pb.CChatRoom_SaveChatRoomGroup_Request{
		ChatGroupId: proto.Uint64(groupID),
		Name:        proto.String(name),
	}

	_, err := c.events.SaveChatRoomGroup(ctx, req)

	return err
}

// RenameChatRoomGroup changes the display name of a group chat room.
func (c *Chat) RenameChatRoomGroup(ctx context.Context, groupID uint64, newName string) (string, error) {
	req := &pb.CChatRoom_RenameChatRoomGroup_Request{
		ChatGroupId: proto.Uint64(groupID),
		Name:        proto.String(newName),
	}

	resp, err := c.events.RenameChatRoomGroup(ctx, req)
	if err != nil {
		return "", err
	}

	return resp.GetName(), nil
}

// GetMyChatRoomGroups lists all group chat rooms joined by the account.
func (c *Chat) GetMyChatRoomGroups(ctx context.Context) (*pb.CChatRoom_GetMyChatRoomGroups_Response, error) {
	req := &pb.CChatRoom_GetMyChatRoomGroups_Request{}

	return c.events.GetMyChatRoomGroups(ctx, req)
}

// GetChatRoomGroupState fetches detailed state metrics for a group chat room.
func (c *Chat) GetChatRoomGroupState(
	ctx context.Context,
	groupID uint64,
) (*pb.CChatRoom_GetChatRoomGroupState_Response, error) {
	req := &pb.CChatRoom_GetChatRoomGroupState_Request{
		ChatGroupId: proto.Uint64(groupID),
	}

	return c.events.GetChatRoomGroupState(ctx, req)
}

// CreateInviteLink generates a shareable invite link for a group chat room.
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

	return c.events.CreateInviteLink(ctx, req)
}

// GetInviteLinksForGroup lists active invite links for a group chat room.
func (c *Chat) GetInviteLinksForGroup(
	ctx context.Context,
	groupID uint64,
) ([]*pb.CChatRoom_GetInviteLinksForGroup_Response_LinkInfo, error) {
	req := &pb.CChatRoom_GetInviteLinksForGroup_Request{
		ChatGroupId: proto.Uint64(groupID),
	}

	resp, err := c.events.GetInviteLinksForGroup(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp.GetInviteLinks(), nil
}

// DeleteInviteLink revokes a group chat invite link by code.
func (c *Chat) DeleteInviteLink(ctx context.Context, groupID uint64, inviteCode string) error {
	req := &pb.CChatRoom_DeleteInviteLink_Request{
		ChatGroupId: proto.Uint64(groupID),
		InviteCode:  proto.String(inviteCode),
	}

	_, err := c.events.DeleteInviteLink(ctx, req)

	return err
}

func (c *Chat) handleIncomingMessage(msg *pb.CFriendMessages_IncomingMessage_Notification) {
	if msg == nil || msg.GetLocalEcho() {
		return
	}

	senderID := msg.GetSteamidFriend()
	timestamp := time.Unix(int64(msg.GetRtime32ServerTimestamp()), 0)

	switch msg.GetChatEntryType() {
	case ChatEntryTypeChatMsg, ChatEntryTypeEmote:
		evt := AcquireMessageEvent(senderID, msg.GetMessage(), timestamp, msg.GetOrdinal())
		c.Bus.Publish(evt)

	case ChatEntryTypeSticker:
		evt := &StickerEvent{
			SenderID:  senderID,
			StickerID: msg.GetMessage(),
			Timestamp: timestamp,
		}
		c.Bus.Publish(evt)

	case ChatEntryTypeTyping:
		evt := &TypingEvent{SenderID: senderID}
		c.Bus.Publish(evt)

	default:
		c.Logger.Debug(
			"Received unhandled chat entry type",
			log.Int32("type", msg.GetChatEntryType()),
		)
	}
}

func (c *Chat) handleGroupMessage(msg *pb.CChatRoom_IncomingChatMessage_Notification) {
	if msg == nil {
		return
	}

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

	c.Bus.Publish(evt)
}

func (c *Chat) handleFriendReaction(msg *pb.CFriendMessages_MessageReaction_Notification) {
	if msg == nil {
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

func (c *Chat) handleGroupReaction(msg *pb.CChatRoom_MessageReaction_Notification) {
	if msg == nil {
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
		sessionsResp, err = c.events.GetActiveMessageSessions(ctx, req)
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

func (c *Chat) handleLegacyFriendMsg(msg *pb.CMsgClientFriendMsgIncoming) {
	if msg == nil {
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
		c.Bus.Publish(evt)

	case ChatEntryTypeTyping:
		evt := &TypingEvent{SenderID: senderID}
		c.Bus.Publish(evt)

	default:
		c.Logger.Debug(
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
