// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package notifications

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

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

func TestFrom_NilClient_ReturnsNil(t *testing.T) {
	t.Parallel()

	assert.Nil(t, From(nil))
}

func TestWithModule_ValidOption_ReturnsNonNil(t *testing.T) {
	t.Parallel()

	opt := WithModule()
	assert.NotNil(t, opt)
}

func TestInit_SuccessLifecycle_RegistersAndUnregistersEMsg(t *testing.T) {
	t.Parallel()

	t.Run("success_lifecycle", func(t *testing.T) {
		t.Parallel()
		n, ictx := setupNotifications(t)
		assert.Equal(t, ModuleName, n.Name())
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
	})
}

func TestRequestNotifications_VariousOperations_SendsLegacyOrUnifiedProto(t *testing.T) {
	t.Parallel()

	t.Run("request_notifications", func(t *testing.T) {
		t.Parallel()
		n, ictx := setupNotifications(t)
		ctx := t.Context()

		err := n.RequestNotifications(ctx)
		assert.NoError(t, err)

		ictx.MockService().ResponseErrs[enums.EMsg_ClientRequestItemAnnouncements.String()] = errors.New("timeout")
		err = n.RequestNotifications(ctx)
		assert.NoError(t, err)
	})

	t.Run("mark_notifications_read", func(t *testing.T) {
		t.Parallel()
		n, ictx := setupNotifications(t)
		ctx := t.Context()

		ictx.MockService().SetProtoResponse("SteamNotification", "MarkNotificationsRead", &structpb.Struct{})

		err := n.MarkNotificationsRead(ctx, []uint64{123, 456})
		assert.NoError(t, err)

		ictx.MockService().ResponseErrs["SteamNotification.MarkNotificationsRead#1"] = errors.New("api fail")
		ictx.MockService().ResponseErrs["SteamNotification.MarkNotificationsRead"] = errors.New("api fail")
		err = n.MarkNotificationsRead(ctx, []uint64{123, 456})
		assert.ErrorContains(t, err, "api fail")
	})

	t.Run("mark_all_notifications_read", func(t *testing.T) {
		t.Parallel()
		n, ictx := setupNotifications(t)
		ctx := t.Context()

		ictx.MockService().SetProtoResponse("SteamNotification", "MarkNotificationsRead", &structpb.Struct{})

		err := n.MarkAllNotificationsRead(ctx)
		assert.NoError(t, err)

		ictx.MockService().ResponseErrs["SteamNotification.MarkNotificationsRead#1"] = errors.New("api fail")
		ictx.MockService().ResponseErrs["SteamNotification.MarkNotificationsRead"] = errors.New("api fail")
		err = n.MarkAllNotificationsRead(ctx)
		assert.ErrorContains(t, err, "api fail")
	})
}

func TestHandleItemAnnouncements_VariousPayloads_PublishesEvents(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		_, ictx := setupNotifications(t)

		sub := ictx.Bus().Subscribe(&ItemAnnouncementsEvent{})
		defer sub.Unsubscribe()

		ictx.EmitPacket(t, enums.EMsg_ClientItemAnnouncements, &pb.CMsgClientItemAnnouncements{
			CountNewItems: proto.Uint32(5),
		})

		waitCtx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		t.Cleanup(cancel)

		select {
		case ev := <-sub.C():
			event := ev.(*ItemAnnouncementsEvent)
			assert.Equal(t, uint32(5), event.CountNewItems)
		case <-waitCtx.Done():
			t.Fatal("event not received")
		}
	})

	t.Run("error_unmarshal", func(t *testing.T) {
		t.Parallel()
		n, _ := setupNotifications(t)
		assert.NotPanics(t, func() {
			n.handleItemAnnouncements(&protocol.Packet{
				EMsg:    enums.EMsg_ClientItemAnnouncements,
				Payload: []byte{0xFF},
			})
		})
	})
}

func TestHandleCommentNotifications_VariousPayloads_PublishesEvents(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		_, ictx := setupNotifications(t)

		sub := ictx.Bus().Subscribe(&CommentNotificationsEvent{})
		defer sub.Unsubscribe()

		ictx.EmitPacket(t, enums.EMsg_ClientCommentNotifications, &pb.CMsgClientCommentNotifications{
			CountNewComments:              proto.Uint32(3),
			CountNewCommentsOwner:         proto.Uint32(1),
			CountNewCommentsSubscriptions: proto.Uint32(2),
		})

		waitCtx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		t.Cleanup(cancel)

		select {
		case ev := <-sub.C():
			event := ev.(*CommentNotificationsEvent)
			assert.Equal(t, uint32(3), event.CountNewComments)
			assert.Equal(t, uint32(1), event.CountNewCommentsOwner)
			assert.Equal(t, uint32(2), event.CountNewCommentsSubscriptions)
		case <-waitCtx.Done():
			t.Fatal("event not received")
		}
	})

	t.Run("error_unmarshal", func(t *testing.T) {
		t.Parallel()
		n, _ := setupNotifications(t)
		assert.NotPanics(t, func() {
			n.handleCommentNotifications(&protocol.Packet{
				EMsg:    enums.EMsg_ClientCommentNotifications,
				Payload: []byte{0xFF},
			})
		})
	})
}

func TestHandleUserNotifications_VariousPayloads_TracksAndPublishesEvents(t *testing.T) {
	t.Parallel()

	t.Run("first_emission", func(t *testing.T) {
		t.Parallel()
		_, ictx := setupNotifications(t)

		sub := ictx.Bus().Subscribe(&UserNotificationsEvent{})
		defer sub.Unsubscribe()

		ictx.EmitPacket(t, enums.EMsg_ClientUserNotifications, &pb.CMsgClientUserNotifications{
			Notifications: []*pb.CMsgClientUserNotifications_Notification{
				{UserNotificationType: proto.Uint32(1), Count: proto.Uint32(5)},
				{UserNotificationType: proto.Uint32(3), Count: proto.Uint32(2)},
			},
		})

		waitCtx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		t.Cleanup(cancel)

		select {
		case ev := <-sub.C():
			event := ev.(*UserNotificationsEvent)
			assert.Equal(t, uint32(5), event.Notifications[1])
			assert.Equal(t, uint32(2), event.Notifications[3])
		case <-waitCtx.Done():
			t.Fatal("event not received")
		}
	})

	t.Run("suppress_duplicate", func(t *testing.T) {
		t.Parallel()
		_, ictx := setupNotifications(t)

		sub := ictx.Bus().Subscribe(&UserNotificationsEvent{})
		defer sub.Unsubscribe()

		ictx.EmitPacket(t, enums.EMsg_ClientUserNotifications, &pb.CMsgClientUserNotifications{
			Notifications: []*pb.CMsgClientUserNotifications_Notification{
				{UserNotificationType: proto.Uint32(1), Count: proto.Uint32(5)},
				{UserNotificationType: proto.Uint32(3), Count: proto.Uint32(2)},
			},
		})
		<-sub.C()

		ictx.EmitPacket(t, enums.EMsg_ClientUserNotifications, &pb.CMsgClientUserNotifications{
			Notifications: []*pb.CMsgClientUserNotifications_Notification{
				{UserNotificationType: proto.Uint32(1), Count: proto.Uint32(5)},
				{UserNotificationType: proto.Uint32(3), Count: proto.Uint32(2)},
			},
		})

		negCtx, negCancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		t.Cleanup(negCancel)

		select {
		case <-sub.C():
			t.Fatal("duplicate event should be suppressed")
		case <-negCtx.Done():
			// Expected
		}
	})

	t.Run("emit_on_change", func(t *testing.T) {
		t.Parallel()
		_, ictx := setupNotifications(t)

		sub := ictx.Bus().Subscribe(&UserNotificationsEvent{})
		defer sub.Unsubscribe()

		ictx.EmitPacket(t, enums.EMsg_ClientUserNotifications, &pb.CMsgClientUserNotifications{
			Notifications: []*pb.CMsgClientUserNotifications_Notification{
				{UserNotificationType: proto.Uint32(1), Count: proto.Uint32(10)},
			},
		})

		waitCtx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		t.Cleanup(cancel)

		select {
		case ev := <-sub.C():
			event := ev.(*UserNotificationsEvent)
			assert.Equal(t, uint32(10), event.Notifications[1])
		case <-waitCtx.Done():
			t.Fatal("event not received")
		}
	})

	t.Run("new_type_count_zero", func(t *testing.T) {
		t.Parallel()
		_, ictx := setupNotifications(t)

		sub := ictx.Bus().Subscribe(&UserNotificationsEvent{})
		defer sub.Unsubscribe()

		ictx.EmitPacket(t, enums.EMsg_ClientUserNotifications, &pb.CMsgClientUserNotifications{
			Notifications: []*pb.CMsgClientUserNotifications_Notification{
				{UserNotificationType: proto.Uint32(99), Count: proto.Uint32(0)},
			},
		})

		negCtx, negCancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		t.Cleanup(negCancel)

		select {
		case <-sub.C():
			t.Fatal("should not emit event when new type has 0 count")
		case <-negCtx.Done():
			// Expected
		}
	})

	t.Run("error_unmarshal", func(t *testing.T) {
		t.Parallel()
		n, _ := setupNotifications(t)
		assert.NotPanics(t, func() {
			n.handleUserNotifications(&protocol.Packet{
				EMsg:    enums.EMsg_ClientUserNotifications,
				Payload: []byte{0xFF},
			})
		})
	})
}

func TestHandleOfflineMessages_VariousPayloads_PublishesEvents(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		_, ictx := setupNotifications(t)

		sub := ictx.Bus().Subscribe(&OfflineMessagesEvent{})
		defer sub.Unsubscribe()

		ictx.EmitPacket(t, enums.EMsg_ClientChatOfflineMessageNotification, &pb.CMsgClientOfflineMessageNotification{
			OfflineMessages:            proto.Uint32(10),
			FriendsWithOfflineMessages: []uint32{12345, 67890},
		})

		waitCtx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		t.Cleanup(cancel)

		select {
		case ev := <-sub.C():
			event := ev.(*OfflineMessagesEvent)
			assert.Equal(t, uint32(10), event.OfflineMessages)
			assert.Len(t, event.FriendsWithOfflineMessages, 2)
		case <-waitCtx.Done():
			t.Fatal("event not received")
		}
	})

	t.Run("error_unmarshal", func(t *testing.T) {
		t.Parallel()
		n, _ := setupNotifications(t)
		assert.NotPanics(t, func() {
			n.handleOfflineMessages(&protocol.Packet{
				EMsg:    enums.EMsg_ClientChatOfflineMessageNotification,
				Payload: []byte{0xFF},
			})
		})
	})
}

func TestHandleMarketingMessages_VariousPayloads_ParsesAndPublishesEvents(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		_, ictx := setupNotifications(t)

		sub := ictx.Bus().Subscribe(&MarketingMessagesEvent{})
		defer sub.Unsubscribe()

		handler, ok := ictx.GetPacketHandler(enums.EMsg_ClientMarketingMessageUpdate2)
		require.True(t, ok)

		subMsg := make([]byte, 0, 32)
		subMsg = append(subMsg, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00)
		subMsg = append(subMsg, []byte("https://example.com")...)
		subMsg = append(subMsg, 0x00)
		subMsg = append(subMsg, 0x2A, 0x00, 0x00, 0x00)

		payload := make([]byte, 0, 12+len(subMsg))
		payload = append(payload, 0xE8, 0x03, 0x00, 0x00)
		payload = append(payload, 0x01, 0x00, 0x00, 0x00)

		subLen := uint32(len(subMsg))
		payload = append(payload, byte(subLen), byte(subLen>>8), byte(subLen>>16), byte(subLen>>24))
		payload = append(payload, subMsg...)

		handler(&protocol.Packet{
			EMsg:    enums.EMsg_ClientMarketingMessageUpdate2,
			Payload: payload,
		})

		waitCtx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		t.Cleanup(cancel)

		select {
		case ev := <-sub.C():
			event := ev.(*MarketingMessagesEvent)
			assert.Equal(t, int64(1000), event.Timestamp)
			assert.Len(t, event.Messages, 1)
			assert.Equal(t, "https://example.com", event.Messages[0].URL)
			assert.Equal(t, uint32(42), event.Messages[0].Flags)

		case <-waitCtx.Done():
			t.Fatal("event not received")
		}
	})

	t.Run("short_payload", func(t *testing.T) {
		t.Parallel()
		n, _ := setupNotifications(t)
		assert.NotPanics(t, func() {
			n.handleMarketingMessages(&protocol.Packet{
				EMsg:    enums.EMsg_ClientMarketingMessageUpdate2,
				Payload: []byte{1, 2, 3},
			})
		})
	})

	t.Run("truncated_loop_len", func(t *testing.T) {
		t.Parallel()
		n, _ := setupNotifications(t)
		payload := make([]byte, 10)
		payload[0] = 0xE8
		payload[1] = 0x03
		payload[4] = 0x01

		assert.NotPanics(t, func() {
			n.handleMarketingMessages(&protocol.Packet{
				EMsg:    enums.EMsg_ClientMarketingMessageUpdate2,
				Payload: payload,
			})
		})
	})

	t.Run("truncated_loop_subpayload", func(t *testing.T) {
		t.Parallel()
		n, _ := setupNotifications(t)
		payload := make([]byte, 14)
		payload[0] = 0xE8
		payload[1] = 0x03
		payload[4] = 0x01
		payload[8] = 50

		assert.NotPanics(t, func() {
			n.handleMarketingMessages(&protocol.Packet{
				EMsg:    enums.EMsg_ClientMarketingMessageUpdate2,
				Payload: payload,
			})
		})
	})

	t.Run("parse_payload_short_4", func(t *testing.T) {
		t.Parallel()

		msg := parseMarketingMessage([]byte{1, 2})
		assert.Nil(t, msg)
	})

	t.Run("parse_payload_short_12", func(t *testing.T) {
		t.Parallel()

		msg := parseMarketingMessage([]byte{1, 2, 3, 4, 5})
		assert.Nil(t, msg)
	})

	t.Run("parse_no_url_null_terminator", func(t *testing.T) {
		t.Parallel()

		payload := make([]byte, 15)
		for i := range payload {
			payload[i] = 'a'
		}

		msg := parseMarketingMessage(payload)
		assert.Nil(t, msg)
	})

	t.Run("parse_truncated_flags", func(t *testing.T) {
		t.Parallel()

		payload := make([]byte, 0, 15)
		payload = append(payload, make([]byte, 8)...)
		payload = append(payload, []byte("https://url.com")...)
		payload = append(payload, 0x00)
		payload = append(payload, []byte{1, 2}...)

		msg := parseMarketingMessage(payload)
		assert.Nil(t, msg)
	})
}

func TestHandleNotificationsReceived_VariousPayloads_PublishesEvents(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		_, ictx := setupNotifications(t)

		sub := ictx.Bus().Subscribe(&ReceivedEvent{})
		defer sub.Unsubscribe()

		serviceHandler, ok := ictx.GetServiceHandler("SteamNotificationClient.NotificationsReceived#1")
		require.True(t, ok)

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

		waitCtx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		t.Cleanup(cancel)

		select {
		case ev := <-sub.C():
			event := ev.(*ReceivedEvent)
			assert.Len(t, event.Notifications, 1)
			assert.Equal(t, uint64(123), event.Notifications[0].GetNotificationId())
			assert.Equal(t, uint32(2), event.PendingGiftCount)
		case <-waitCtx.Done():
			t.Fatal("event not received")
		}
	})

	t.Run("empty_notifications_suppresses_event", func(t *testing.T) {
		t.Parallel()
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

		negCtx, negCancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		t.Cleanup(negCancel)

		select {
		case <-sub.C():
			t.Fatal("empty notifications should not emit event")
		case <-negCtx.Done():
			// Expected
		}
	})

	t.Run("error_unmarshal", func(t *testing.T) {
		t.Parallel()
		n, _ := setupNotifications(t)
		assert.NotPanics(t, func() {
			n.handleNotificationsReceived(&protocol.Packet{
				EMsg:    enums.EMsg_ServiceMethod,
				Payload: []byte{0xFF},
			})
		})
	})
}
