// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package notifications processes incoming Steam notifications, comments, trade offer signals, and item announcements.
package notifications

import (
	"context"
	"fmt"
	"maps"
	"sync/atomic"

	"github.com/lemon4ksan/foundation/async/log"
	"google.golang.org/protobuf/types/known/structpb"

	pb "github.com/lemon4ksan/g-man/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/client"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
)

const ModuleName string = "notifications"

// WithModule registers the Notifications module in the client.
func WithModule() client.Option {
	return client.WithModule(New())
}

// From retrieves the Notifications module instance from the client.
func From(c *client.Client) *Notifications {
	return client.GetModule[*Notifications](c)
}

// Snapshot is an immutable point-in-time state of notifications.
type Snapshot struct {
	LastNotificationCounts map[NotificationType]uint32
}

// Notifications tracks unread notification counts and publishes events when changes are detected.
//
// Thread Safety:
//   - Lock-free for all reads (0 mutexes, atomic pointer load).
//   - Safe for concurrent use across all methods.
type Notifications struct {
	module.Base

	client service.Doer
	events Events
	state  atomic.Pointer[Snapshot]
}

// New constructs a Notifications module instance.
func New() *Notifications {
	n := &Notifications{
		Base: module.New(ModuleName),
	}
	n.state.Store(&Snapshot{
		LastNotificationCounts: make(map[NotificationType]uint32),
	})

	return n
}

func (n *Notifications) Init(init module.InitContext) error {
	if err := n.Base.Init(init); err != nil {
		return err
	}

	n.client = init.Service()
	n.events = NewEvents(init)

	n.events.OnItemAnnouncements(n.handleItemAnnouncements)
	n.events.OnCommentNotifications(n.handleCommentNotifications)
	n.events.OnUserNotifications(n.handleUserNotifications)
	n.events.OnOfflineMessages(n.handleOfflineMessages)
	n.events.OnMarketingMessages(n.handleMarketingMessages)
	n.events.OnNotificationsReceived(n.handleNotificationsReceived)

	return nil
}

func (n *Notifications) Close() error {
	if n.events != nil {
		_ = n.events.Close()
	}

	return n.Base.Close()
}

// --- Lock-Free State Readers (1 CPU instruction) ---

// Snapshot returns an immutable point-in-time state snapshot.
func (n *Notifications) Snapshot() Snapshot {
	return *n.state.Load()
}

// LastNotificationCounts returns a clone of the last recorded notification counts.
func (n *Notifications) LastNotificationCounts() map[NotificationType]uint32 {
	return maps.Clone(n.state.Load().LastNotificationCounts)
}

// --- Outbound Notifications & Commands ---

// RequestNotifications explicitly requests notification count updates from Steam.
func (n *Notifications) RequestNotifications(ctx context.Context) error {
	_ = n.events.RequestItemAnnouncements(ctx, &pb.CMsgClientRequestItemAnnouncements{})
	_ = n.events.RequestCommentNotifications(ctx, &pb.CMsgClientRequestCommentNotifications{})
	_ = n.events.RequestOfflineMessageCount(ctx, &pb.CMsgClientRequestOfflineMessageCount{})

	return nil
}

// MarkNotificationsRead marks specified notifications as read via SteamNotification/MarkNotificationsRead.
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

// --- Copy-On-Write State Updates ---

func (n *Notifications) updateState(fn func(next *Snapshot)) {
	for {
		current := n.state.Load()
		next := *current
		fn(&next)

		if n.state.CompareAndSwap(current, &next) {
			return
		}
	}
}

func (n *Notifications) handleItemAnnouncements(msg *pb.CMsgClientItemAnnouncements) {
	n.Logger.Debug("Item announcements received", log.Uint32("count", msg.GetCountNewItems()))

	n.Bus.Publish(&ItemAnnouncementsEvent{
		CountNewItems: msg.GetCountNewItems(),
		UnseenItems:   msg.GetUnseenItems(),
	})
}

func (n *Notifications) handleCommentNotifications(msg *pb.CMsgClientCommentNotifications) {
	n.Logger.Debug("Comment notifications received", log.Uint32("count", msg.GetCountNewComments()))

	n.Bus.Publish(&CommentNotificationsEvent{
		CountNewComments:              msg.GetCountNewComments(),
		CountNewCommentsOwner:         msg.GetCountNewCommentsOwner(),
		CountNewCommentsSubscriptions: msg.GetCountNewCommentsSubscriptions(),
	})
}

func (n *Notifications) handleUserNotifications(msg *pb.CMsgClientUserNotifications) {
	notifications := make(map[NotificationType]uint32)
	for _, notif := range msg.GetNotifications() {
		notifications[NotificationType(notif.GetUserNotificationType())] = notif.GetCount()
	}

	changed := false
	n.updateState(func(s *Snapshot) {
		counts := maps.Clone(s.LastNotificationCounts)
		if counts == nil {
			counts = make(map[NotificationType]uint32)
		}

		for notifType, count := range notifications {
			prev, exists := counts[notifType]
			if !exists && count == 0 {
				counts[notifType] = 0
				continue
			}

			if prev == count {
				continue
			}

			counts[notifType] = count
			changed = true
		}

		s.LastNotificationCounts = counts
	})

	if changed {
		n.Bus.Publish(&UserNotificationsEvent{
			Notifications: notifications,
		})
	}
}

func (n *Notifications) handleOfflineMessages(msg *pb.CMsgClientOfflineMessageNotification) {
	friends := make([]id.ID, 0, len(msg.GetFriendsWithOfflineMessages()))
	for _, accountID := range msg.GetFriendsWithOfflineMessages() {
		sid := id.FromAccountID(accountID)
		friends = append(friends, sid)
	}

	n.Logger.Debug("Offline messages received", log.Uint32("count", msg.GetOfflineMessages()))

	n.Bus.Publish(&OfflineMessagesEvent{
		OfflineMessages:            msg.GetOfflineMessages(),
		FriendsWithOfflineMessages: friends,
	})
}

func (n *Notifications) handleMarketingMessages(ev *MarketingMessagesEvent) {
	if ev == nil {
		return
	}

	n.Logger.Debug("Marketing messages received", log.Uint32("count", uint32(len(ev.Messages))))
	n.Bus.Publish(ev)
}

func (n *Notifications) handleNotificationsReceived(msg *pb.CSteamNotification_NotificationsReceived_Notification) {
	if len(msg.GetNotifications()) == 0 {
		return
	}

	n.Logger.Debug("Notifications received", log.Int("count", len(msg.GetNotifications())))

	n.Bus.Publish(&ReceivedEvent{
		Notifications:            msg.GetNotifications(),
		PendingGiftCount:         msg.GetPendingGiftCount(),
		PendingFriendCount:       msg.GetPendingFriendCount(),
		PendingFamilyInviteCount: msg.GetPendingFamilyInviteCount(),
	})
}
