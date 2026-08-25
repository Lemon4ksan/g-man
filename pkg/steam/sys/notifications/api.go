// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:generate aoni-gen -file=api.go

package notifications

import (
	"context"

	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	pb "github.com/lemon4ksan/g-man/protobuf/steam"
)

var (
	_ enums.EMsg
	_ module.InitContext
)

// Events defines typed RPC, Notify, and Event operations for Steam notifications.
//
// @aoni:service
// @protocol rpc
// @requester "module.InitContext"
type Events interface {
	// @event enums.EMsg_ClientItemAnnouncements
	OnItemAnnouncements(handler func(msg *pb.CMsgClientItemAnnouncements)) (unsubscribe func())

	// @event enums.EMsg_ClientCommentNotifications
	OnCommentNotifications(handler func(msg *pb.CMsgClientCommentNotifications)) (unsubscribe func())

	// @event enums.EMsg_ClientUserNotifications
	OnUserNotifications(handler func(msg *pb.CMsgClientUserNotifications)) (unsubscribe func())

	// @event enums.EMsg_ClientChatOfflineMessageNotification
	OnOfflineMessages(handler func(msg *pb.CMsgClientOfflineMessageNotification)) (unsubscribe func())

	// @event enums.EMsg_ClientMarketingMessageUpdate2
	// @return body | parseMarketingMessages
	OnMarketingMessages(handler func(msg *MarketingMessagesEvent)) (unsubscribe func())

	// @event "SteamNotificationClient.NotificationsReceived#1"
	OnNotificationsReceived(
		handler func(msg *pb.CSteamNotification_NotificationsReceived_Notification),
	) (unsubscribe func())

	// --- Outbound Notifications (One-Way) ---

	// @notify
	// @op enums.EMsg_ClientRequestItemAnnouncements
	RequestItemAnnouncements(ctx context.Context, req *pb.CMsgClientRequestItemAnnouncements) error

	// @notify
	// @op enums.EMsg_ClientRequestCommentNotifications
	RequestCommentNotifications(ctx context.Context, req *pb.CMsgClientRequestCommentNotifications) error

	// @notify
	// @op enums.EMsg_ClientChatRequestOfflineMessageCount
	RequestOfflineMessageCount(ctx context.Context, req *pb.CMsgClientRequestOfflineMessageCount) error

	// Close unsubscribes all active event listeners.
	Close() error
}
