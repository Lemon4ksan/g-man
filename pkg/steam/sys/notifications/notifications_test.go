// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package notifications

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	module "github.com/lemon4ksan/g-man/test/mock"
)

func setupNotifications(t *testing.T) (*Notifications, *module.InitContext) {
	t.Helper()

	n := New()
	ictx := module.NewInitContext()

	require.NoError(t, n.Init(ictx))

	t.Cleanup(func() {
		_ = n.Close()
	})

	return n, ictx
}

func TestNotifications_InitAndClose(t *testing.T) {
	n, ictx := setupNotifications(t)
	ictx.AssertPacketHandlerRegistered(t, enums.EMsg_ClientItemAnnouncements)
	ictx.AssertPacketHandlerRegistered(t, enums.EMsg_ClientCommentNotifications)
	ictx.AssertPacketHandlerRegistered(t, enums.EMsg_ClientUserNotifications)
	ictx.AssertPacketHandlerRegistered(t, enums.EMsg_ClientChatOfflineMessageNotification)
	ictx.AssertPacketHandlerRegistered(t, enums.EMsg_ClientMarketingMessageUpdate2)
	ictx.AssertServiceHandlerRegistered(t, "SteamNotificationClient.NotificationsReceived#1")

	err := n.Close()
	require.NoError(t, err)
	ictx.AssertPacketHandlerUnregistered(t, enums.EMsg_ClientItemAnnouncements)
	ictx.AssertPacketHandlerUnregistered(t, enums.EMsg_ClientCommentNotifications)
	ictx.AssertPacketHandlerUnregistered(t, enums.EMsg_ClientUserNotifications)
	ictx.AssertPacketHandlerUnregistered(t, enums.EMsg_ClientChatOfflineMessageNotification)
	ictx.AssertPacketHandlerUnregistered(t, enums.EMsg_ClientMarketingMessageUpdate2)
	ictx.AssertServiceHandlerUnregistered(t, "SteamNotificationClient.NotificationsReceived#1")
	assert.Nil(t, n.unregFuncs)
}

func TestNotifications_ItemAnnouncements(t *testing.T) {
	_, ictx := setupNotifications(t)

	sub := ictx.Bus().Subscribe(&ItemAnnouncementsEvent{})
	defer sub.Unsubscribe()

	ictx.EmitPacket(t, enums.EMsg_ClientItemAnnouncements, &pb.CMsgClientItemAnnouncements{
		CountNewItems: proto.Uint32(5),
	})

	select {
	case ev := <-sub.C():
		event := ev.(*ItemAnnouncementsEvent)
		assert.Equal(t, uint32(5), event.CountNewItems)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("event not received")
	}
}

func TestNotifications_CommentNotifications(t *testing.T) {
	_, ictx := setupNotifications(t)

	sub := ictx.Bus().Subscribe(&CommentNotificationsEvent{})
	defer sub.Unsubscribe()

	ictx.EmitPacket(t, enums.EMsg_ClientCommentNotifications, &pb.CMsgClientCommentNotifications{
		CountNewComments:              proto.Uint32(3),
		CountNewCommentsOwner:         proto.Uint32(1),
		CountNewCommentsSubscriptions: proto.Uint32(2),
	})

	select {
	case ev := <-sub.C():
		event := ev.(*CommentNotificationsEvent)
		assert.Equal(t, uint32(3), event.CountNewComments)
		assert.Equal(t, uint32(1), event.CountNewCommentsOwner)
		assert.Equal(t, uint32(2), event.CountNewCommentsSubscriptions)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("event not received")
	}
}

func TestNotifications_UserNotifications(t *testing.T) {
	n, ictx := setupNotifications(t)

	t.Run("First Emission", func(t *testing.T) {
		sub := ictx.Bus().Subscribe(&UserNotificationsEvent{})
		defer sub.Unsubscribe()

		ictx.EmitPacket(t, enums.EMsg_ClientUserNotifications, &pb.CMsgClientUserNotifications{
			Notifications: []*pb.CMsgClientUserNotifications_Notification{
				{UserNotificationType: proto.Uint32(1), Count: proto.Uint32(5)},
				{UserNotificationType: proto.Uint32(3), Count: proto.Uint32(2)},
			},
		})

		select {
		case ev := <-sub.C():
			event := ev.(*UserNotificationsEvent)
			assert.Equal(t, uint32(5), event.Notifications[1])
			assert.Equal(t, uint32(2), event.Notifications[3])
		case <-time.After(100 * time.Millisecond):
			t.Fatal("event not received")
		}
	})

	t.Run("Suppress Duplicate", func(t *testing.T) {
		sub := ictx.Bus().Subscribe(&UserNotificationsEvent{})
		defer sub.Unsubscribe()

		// Send same values again
		ictx.EmitPacket(t, enums.EMsg_ClientUserNotifications, &pb.CMsgClientUserNotifications{
			Notifications: []*pb.CMsgClientUserNotifications_Notification{
				{UserNotificationType: proto.Uint32(1), Count: proto.Uint32(5)},
				{UserNotificationType: proto.Uint32(3), Count: proto.Uint32(2)},
			},
		})

		select {
		case <-sub.C():
			t.Fatal("duplicate event should be suppressed")
		case <-time.After(100 * time.Millisecond):
			// Expected: no event
		}
	})

	t.Run("Emit on Change", func(t *testing.T) {
		sub := ictx.Bus().Subscribe(&UserNotificationsEvent{})
		defer sub.Unsubscribe()

		ictx.EmitPacket(t, enums.EMsg_ClientUserNotifications, &pb.CMsgClientUserNotifications{
			Notifications: []*pb.CMsgClientUserNotifications_Notification{
				{UserNotificationType: proto.Uint32(1), Count: proto.Uint32(10)},
			},
		})

		select {
		case ev := <-sub.C():
			event := ev.(*UserNotificationsEvent)
			assert.Equal(t, uint32(10), event.Notifications[1])
		case <-time.After(100 * time.Millisecond):
			t.Fatal("event not received")
		}
	})

	_ = n
}

func TestNotifications_OfflineMessages(t *testing.T) {
	_, ictx := setupNotifications(t)

	sub := ictx.Bus().Subscribe(&OfflineMessagesEvent{})
	defer sub.Unsubscribe()

	ictx.EmitPacket(t, enums.EMsg_ClientChatOfflineMessageNotification, &pb.CMsgClientOfflineMessageNotification{
		OfflineMessages:            proto.Uint32(10),
		FriendsWithOfflineMessages: []uint32{12345, 67890},
	})

	select {
	case ev := <-sub.C():
		event := ev.(*OfflineMessagesEvent)
		assert.Equal(t, uint32(10), event.OfflineMessages)
		assert.Len(t, event.FriendsWithOfflineMessages, 2)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("event not received")
	}
}

func TestNotifications_MarketingMessages(t *testing.T) {
	_, ictx := setupNotifications(t)

	sub := ictx.Bus().Subscribe(&MarketingMessagesEvent{})
	defer sub.Unsubscribe()

	handler, ok := ictx.GetPacketHandler(enums.EMsg_ClientMarketingMessageUpdate2)
	require.True(t, ok, "handler should be registered")

	// Sub message
	subMsg := make([]byte, 0, 32)
	// 8-byte message ID
	subMsg = append(subMsg, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00)
	// URL (null-terminated)
	subMsg = append(subMsg, []byte("https://example.com")...)
	subMsg = append(subMsg, 0x00)
	// flags = 42
	subMsg = append(subMsg, 0x2A, 0x00, 0x00, 0x00)

	// Build raw payload: timestamp(4) + count(4) + submsg_len(4) + msg_id(8) + url(cstring) + flags(4)
	payload := make([]byte, 0, 12+len(subMsg))
	// timestamp = 1000
	payload = append(payload, 0xE8, 0x03, 0x00, 0x00)
	// count = 1
	payload = append(payload, 0x01, 0x00, 0x00, 0x00)

	// sub message length
	subLen := uint32(len(subMsg))
	payload = append(payload, byte(subLen), byte(subLen>>8), byte(subLen>>16), byte(subLen>>24))
	payload = append(payload, subMsg...)

	handler(&protocol.Packet{
		EMsg:    enums.EMsg_ClientMarketingMessageUpdate2,
		Payload: payload,
	})

	select {
	case ev := <-sub.C():
		event := ev.(*MarketingMessagesEvent)
		assert.Equal(t, int64(1000), event.Timestamp)
		assert.Len(t, event.Messages, 1)
		assert.Equal(t, "https://example.com", event.Messages[0].URL)
		assert.Equal(t, uint32(42), event.Messages[0].Flags)

	case <-time.After(100 * time.Millisecond):
		t.Fatal("event not received")
	}
}

func TestNotifications_NotificationsReceived(t *testing.T) {
	_, ictx := setupNotifications(t)

	sub := ictx.Bus().Subscribe(&ReceivedEvent{})
	defer sub.Unsubscribe()

	serviceHandler, ok := ictx.GetServiceHandler("SteamNotificationClient.NotificationsReceived#1")
	require.True(t, ok, "service handler should be registered")

	payload, err := proto.Marshal(&pb.CSteamNotification_NotificationsReceived_Notification{
		Notifications: []*pb.SteamNotificationData{
			{
				NotificationId: proto.Uint64(123),
				NotificationType: func() *pb.ESteamNotificationType {
					v := pb.ESteamNotificationType_k_ESteamNotificationType_TradeOffer
					return &v
				}(),
			},
		},
		PendingGiftCount: proto.Uint32(2),
	})
	require.NoError(t, err)

	serviceHandler(&protocol.Packet{
		Payload: payload,
	})

	select {
	case ev := <-sub.C():
		event := ev.(*ReceivedEvent)
		assert.Len(t, event.Notifications, 1)
		assert.Equal(t, uint64(123), event.Notifications[0].GetNotificationId())
		assert.Equal(t, uint32(2), event.PendingGiftCount)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("event not received")
	}
}

func TestNotifications_EmptyNotificationsReceived(t *testing.T) {
	_, ictx := setupNotifications(t)

	sub := ictx.Bus().Subscribe(&ReceivedEvent{})
	defer sub.Unsubscribe()

	serviceHandler, ok := ictx.GetServiceHandler("SteamNotificationClient.NotificationsReceived#1")
	require.True(t, ok)

	payload, err := proto.Marshal(&pb.CSteamNotification_NotificationsReceived_Notification{
		Notifications: []*pb.SteamNotificationData{},
	})
	require.NoError(t, err)

	serviceHandler(&protocol.Packet{
		Payload: payload,
	})

	select {
	case <-sub.C():
		t.Fatal("empty notifications should not emit event")
	case <-time.After(100 * time.Millisecond):
		// Expected
	}
}
