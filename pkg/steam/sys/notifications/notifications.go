// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package notifications receives, processes, and routes incoming Steam notification messages.
package notifications

import (
	"context"
	"encoding/binary"
	"fmt"
	"strconv"
	"sync"

	"github.com/lemon4ksan/miyako/log"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
)

// ModuleName is the unique string identifier of the notifications coordinator module.
const ModuleName string = "notifications"

// WithModule returns a [steam.Option] that registers the [Notifications] module in the client.
func WithModule() steam.Option {
	return steam.WithModule(New())
}

// From retrieves the registered [Notifications] module instance from the specified [steam.Client].
// It returns nil if the module is not registered or if the client is nil.
func From(c *steam.Client) *Notifications {
	return steam.GetModule[*Notifications](c)
}

// Notifications manages incoming Steam notification messages and routes them to the event bus.
// It tracks notification counts and publishes events when changes are detected.
// Register the notifications manager as a client module using [WithModule] or retrieve it via [From].
type Notifications struct {
	module.Base

	client service.Doer

	mu                     sync.RWMutex
	lastNotificationCounts map[NotificationType]uint32

	unregFuncs []func()
}

// New creates a new [Notifications] module instance.
func New() *Notifications {
	return &Notifications{
		Base:                   module.New(ModuleName),
		lastNotificationCounts: make(map[NotificationType]uint32),
	}
}

// Init registers packet handlers for all notification-related network messages.
// It configures callbacks for item announcements, comment notifications, and offline messages.
// It will panic if the provided [module.InitContext] argument is nil.
func (n *Notifications) Init(init module.InitContext) error {
	if err := n.Base.Init(init); err != nil {
		return err
	}

	n.client = init.Service()

	init.RegisterPacketHandler(enums.EMsg_ClientItemAnnouncements, n.handleItemAnnouncements)
	init.RegisterPacketHandler(enums.EMsg_ClientCommentNotifications, n.handleCommentNotifications)
	init.RegisterPacketHandler(enums.EMsg_ClientUserNotifications, n.handleUserNotifications)
	init.RegisterPacketHandler(enums.EMsg_ClientChatOfflineMessageNotification, n.handleOfflineMessages)
	init.RegisterPacketHandler(enums.EMsg_ClientMarketingMessageUpdate2, n.handleMarketingMessages)
	init.RegisterServiceHandler("SteamNotificationClient.NotificationsReceived#1", n.handleNotificationsReceived)

	n.unregFuncs = append(n.unregFuncs, func() {
		init.UnregisterPacketHandler(enums.EMsg_ClientItemAnnouncements)
		init.UnregisterPacketHandler(enums.EMsg_ClientCommentNotifications)
		init.UnregisterPacketHandler(enums.EMsg_ClientUserNotifications)
		init.UnregisterPacketHandler(enums.EMsg_ClientChatOfflineMessageNotification)
		init.UnregisterPacketHandler(enums.EMsg_ClientMarketingMessageUpdate2)
		init.UnregisterServiceHandler("SteamNotificationClient.NotificationsReceived#1")
	})

	return nil
}

// Close unregisters all packet handlers and releases internal resources.
// Subsequent calls to Close are safe and will be ignored.
func (n *Notifications) Close() error {
	n.mu.Lock()
	for _, unreg := range n.unregFuncs {
		unreg()
	}

	n.unregFuncs = nil
	n.mu.Unlock()

	return n.Base.Close()
}

// RequestNotifications requests current notification counts from Steam.
// It returns an error if sending the request fails or if the context is cancelled.
// It will panic if the provided [context.Context] argument is nil.
func (n *Notifications) RequestNotifications(ctx context.Context) error {
	_ = n.sendProto(ctx, enums.EMsg_ClientRequestItemAnnouncements, &pb.CMsgClientRequestItemAnnouncements{})
	_ = n.sendProto(ctx, enums.EMsg_ClientRequestCommentNotifications, &pb.CMsgClientRequestCommentNotifications{})
	_ = n.sendProto(ctx, enums.EMsg_ClientChatRequestOfflineMessageCount, &pb.CMsgClientRequestOfflineMessageCount{})

	return nil
}

// MarkNotificationsRead marks specific notifications as read using their IDs.
// It sends a unified request to Steam and ignores empty slices of notification IDs.
// It returns an error if the request fails or if the context is cancelled.
// It will panic if the provided [context.Context] argument is nil.
func (n *Notifications) MarkNotificationsRead(ctx context.Context, notificationIds []uint64) error {
	ids := make([]any, 0, len(notificationIds))
	for _, id := range notificationIds {
		ids = append(ids, float64(id))
	}

	body, err := structpb.NewStruct(map[string]any{
		"notification_ids": ids,
	})
	if err != nil {
		return fmt.Errorf("notifications: failed to build mark read request: %w", err)
	}

	_, err = service.UnifiedExplicit[service.NoResponse](
		ctx,
		n.client,
		"POST",
		"SteamNotification",
		"MarkNotificationsRead",
		1,
		body,
	)
	if err != nil {
		n.Logger.Debug("Failed to mark notifications read", log.Err(err))
	}

	return err
}

// MarkAllNotificationsRead marks all active notifications as read.
// It returns an error if the request fails or if the context is cancelled.
// It will panic if the provided [context.Context] argument is nil.
func (n *Notifications) MarkAllNotificationsRead(ctx context.Context) error {
	body, err := structpb.NewStruct(map[string]any{
		"mark_all_read": true,
	})
	if err != nil {
		return fmt.Errorf("notifications: failed to build mark all read request: %w", err)
	}

	_, err = service.UnifiedExplicit[service.NoResponse](
		ctx,
		n.client,
		"POST",
		"SteamNotification",
		"MarkNotificationsRead",
		1,
		body,
	)
	if err != nil {
		n.Logger.Debug("Failed to mark all notifications read", log.Err(err))
	}

	return err
}

func (n *Notifications) sendProto(ctx context.Context, eMsg enums.EMsg, msg proto.Message) error {
	_, err := service.LegacyProto[service.NoResponse](ctx, n.client, eMsg, msg)
	if err != nil {
		n.Logger.Debug("Failed to send notification request",
			log.String("emsg", eMsg.String()),
			log.Err(err),
		)
	}

	return err
}

func (n *Notifications) handleItemAnnouncements(packet *protocol.Packet) {
	msg := &pb.CMsgClientItemAnnouncements{}
	if err := proto.Unmarshal(packet.Payload, msg); err != nil {
		n.Logger.Error("Failed to unmarshal item announcements", log.Err(err))
		return
	}

	n.Logger.Debug("Item announcements received", log.Uint32("count", msg.GetCountNewItems()))

	n.Bus.Publish(&ItemAnnouncementsEvent{
		CountNewItems: msg.GetCountNewItems(),
		UnseenItems:   msg.GetUnseenItems(),
	})
}

func (n *Notifications) handleCommentNotifications(packet *protocol.Packet) {
	msg := &pb.CMsgClientCommentNotifications{}
	if err := proto.Unmarshal(packet.Payload, msg); err != nil {
		n.Logger.Error("Failed to unmarshal comment notifications", log.Err(err))
		return
	}

	n.Logger.Debug("Comment notifications received",
		log.Uint32("count", msg.GetCountNewComments()),
	)

	n.Bus.Publish(&CommentNotificationsEvent{
		CountNewComments:              msg.GetCountNewComments(),
		CountNewCommentsOwner:         msg.GetCountNewCommentsOwner(),
		CountNewCommentsSubscriptions: msg.GetCountNewCommentsSubscriptions(),
	})
}

func (n *Notifications) handleUserNotifications(packet *protocol.Packet) {
	msg := &pb.CMsgClientUserNotifications{}
	if err := proto.Unmarshal(packet.Payload, msg); err != nil {
		n.Logger.Error("Failed to unmarshal user notifications", log.Err(err))
		return
	}

	notifications := make(map[NotificationType]uint32)
	for _, notif := range msg.GetNotifications() {
		notifications[NotificationType(notif.GetUserNotificationType())] = notif.GetCount()
	}

	n.mu.Lock()

	changed := false
	for notifType, count := range notifications {
		prev, exists := n.lastNotificationCounts[notifType]
		if !exists && count == 0 {
			n.lastNotificationCounts[notifType] = 0
			continue
		}

		if prev == count {
			continue
		}

		n.lastNotificationCounts[notifType] = count
		changed = true
	}

	n.mu.Unlock()

	if changed {
		n.Bus.Publish(&UserNotificationsEvent{
			Notifications: notifications,
		})
	}
}

func (n *Notifications) handleOfflineMessages(packet *protocol.Packet) {
	msg := &pb.CMsgClientOfflineMessageNotification{}
	if err := proto.Unmarshal(packet.Payload, msg); err != nil {
		n.Logger.Error("Failed to unmarshal offline messages", log.Err(err))
		return
	}

	friends := make([]id.ID, 0, len(msg.GetFriendsWithOfflineMessages()))
	for _, accountID := range msg.GetFriendsWithOfflineMessages() {
		sid := id.FromAccountID(accountID)
		friends = append(friends, sid)
	}

	n.Logger.Debug("Offline messages received",
		log.Uint32("count", msg.GetOfflineMessages()),
	)

	n.Bus.Publish(&OfflineMessagesEvent{
		OfflineMessages:            msg.GetOfflineMessages(),
		FriendsWithOfflineMessages: friends,
	})
}

func (n *Notifications) handleMarketingMessages(packet *protocol.Packet) {
	if len(packet.Payload) < 8 {
		n.Logger.Warn("MarketingMessageUpdate2 payload too short")
		return
	}

	timestamp := binary.LittleEndian.Uint32(packet.Payload[0:4])
	count := binary.LittleEndian.Uint32(packet.Payload[4:8])

	offset := 8
	messages := make([]MarketingMessage, 0, count)

	for range count {
		if offset+4 > len(packet.Payload) {
			break
		}

		subLen := binary.LittleEndian.Uint32(packet.Payload[offset : offset+4])
		offset += 4

		if offset+int(subLen) > len(packet.Payload) {
			break
		}

		subPayload := packet.Payload[offset : offset+int(subLen)]

		msg := parseMarketingMessage(subPayload)
		if msg != nil {
			messages = append(messages, *msg)
		}

		offset += int(subLen)
	}

	n.Logger.Debug("Marketing messages received",
		log.Uint32("count", uint32(len(messages))),
	)

	n.Bus.Publish(&MarketingMessagesEvent{
		Timestamp: int64(timestamp),
		Messages:  messages,
	})
}

func parseMarketingMessage(payload []byte) *MarketingMessage {
	if len(payload) < 4 {
		return nil
	}

	// Skip the 8-byte message ID (uint64)
	if len(payload) < 12 {
		return nil
	}

	// Read 8-byte ID
	msgID := strconv.FormatUint(uint64(payload[0])|uint64(payload[1])<<8|
		uint64(payload[2])<<16|uint64(payload[3])<<24|
		uint64(payload[4])<<32|uint64(payload[5])<<40|
		uint64(payload[6])<<48|uint64(payload[7])<<56, 10)

	offset := 8

	// Read null-terminated URL string
	urlEnd := -1
	for i := offset; i < len(payload); i++ {
		if payload[i] == 0 {
			urlEnd = i
			break
		}
	}

	if urlEnd == -1 {
		return nil
	}

	url := string(payload[offset:urlEnd])
	offset = urlEnd + 1

	if offset+4 > len(payload) {
		return nil
	}

	flags := binary.LittleEndian.Uint32(payload[offset : offset+4])

	return &MarketingMessage{
		ID:    msgID,
		URL:   url,
		Flags: flags,
	}
}

func (n *Notifications) handleNotificationsReceived(packet *protocol.Packet) {
	msg := &pb.CSteamNotification_NotificationsReceived_Notification{}
	if err := proto.Unmarshal(packet.Payload, msg); err != nil {
		n.Logger.Error("Failed to unmarshal notifications received", log.Err(err))
		return
	}

	if len(msg.GetNotifications()) == 0 {
		return
	}

	n.Logger.Debug("Notifications received",
		log.Int("count", len(msg.GetNotifications())),
	)

	n.Bus.Publish(&ReceivedEvent{
		Notifications:            msg.GetNotifications(),
		PendingGiftCount:         msg.GetPendingGiftCount(),
		PendingFriendCount:       msg.GetPendingFriendCount(),
		PendingFamilyInviteCount: msg.GetPendingFamilyInviteCount(),
	})
}
