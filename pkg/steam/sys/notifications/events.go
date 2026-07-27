// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package notifications

import (
	"fmt"

	"github.com/lemon4ksan/miyako/bus"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
)

type NotificationType uint32

const (
	NotificationInvalid                       NotificationType = 0
	NotificationTest                          NotificationType = 1
	NotificationGift                          NotificationType = 2
	NotificationComment                       NotificationType = 3
	NotificationItem                          NotificationType = 4
	NotificationFriendInvite                  NotificationType = 5
	NotificationMajorSale                     NotificationType = 6
	NotificationPreloadAvailable              NotificationType = 7
	NotificationWishlist                      NotificationType = 8
	NotificationTradeOffer                    NotificationType = 9
	NotificationGeneral                       NotificationType = 10
	NotificationHelpRequest                   NotificationType = 11
	NotificationAsyncGame                     NotificationType = 12
	NotificationChatMsg                       NotificationType = 13
	NotificationModeratorMsg                  NotificationType = 14
	NotificationParentalFeatureAccessRequest  NotificationType = 15
	NotificationFamilyInvite                  NotificationType = 16
	NotificationFamilyPurchaseRequest         NotificationType = 17
	NotificationParentalPlaytimeRequest       NotificationType = 18
	NotificationFamilyPurchaseRequestResponse NotificationType = 19
	NotificationParentalFeatureAccessResponse NotificationType = 20
	NotificationParentalPlaytimeResponse      NotificationType = 21
	NotificationRequestedGameAdded            NotificationType = 22
	NotificationSendToPhone                   NotificationType = 23
	NotificationClipDownloaded                NotificationType = 24
	Notification2FAPrompt                     NotificationType = 25
	NotificationMobileConfirmation            NotificationType = 26
	NotificationPartnerEvent                  NotificationType = 27
	NotificationPlaytestInvite                NotificationType = 28
	NotificationTradeReversal                 NotificationType = 29
	NotificationReportedContentAction         NotificationType = 30
)

func (t NotificationType) String() string {
	switch t {
	case NotificationInvalid:
		return "Invalid"
	case NotificationTest:
		return "Test"
	case NotificationGift:
		return "Gift"
	case NotificationComment:
		return "Comment"
	case NotificationItem:
		return "Item"
	case NotificationFriendInvite:
		return "FriendInvite"
	case NotificationMajorSale:
		return "MajorSale"
	case NotificationPreloadAvailable:
		return "PreloadAvailable"
	case NotificationWishlist:
		return "Wishlist"
	case NotificationTradeOffer:
		return "TradeOffer"
	case NotificationGeneral:
		return "General"
	case NotificationHelpRequest:
		return "HelpRequest"
	case NotificationAsyncGame:
		return "AsyncGame"
	case NotificationChatMsg:
		return "ChatMsg"
	case NotificationModeratorMsg:
		return "ModeratorMsg"
	case NotificationParentalFeatureAccessRequest:
		return "ParentalFeatureAccessRequest"
	case NotificationFamilyInvite:
		return "FamilyInvite"
	case NotificationFamilyPurchaseRequest:
		return "FamilyPurchaseRequest"
	case NotificationParentalPlaytimeRequest:
		return "ParentalPlaytimeRequest"
	case NotificationFamilyPurchaseRequestResponse:
		return "FamilyPurchaseRequestResponse"
	case NotificationParentalFeatureAccessResponse:
		return "ParentalFeatureAccessResponse"
	case NotificationParentalPlaytimeResponse:
		return "ParentalPlaytimeResponse"
	case NotificationRequestedGameAdded:
		return "RequestedGameAdded"
	case NotificationSendToPhone:
		return "SendToPhone"
	case NotificationClipDownloaded:
		return "ClipDownloaded"
	case Notification2FAPrompt:
		return "2FAPrompt"
	case NotificationMobileConfirmation:
		return "MobileConfirmation"
	case NotificationPartnerEvent:
		return "PartnerEvent"
	case NotificationPlaytestInvite:
		return "PlaytestInvite"
	case NotificationTradeReversal:
		return "TradeReversal"
	case NotificationReportedContentAction:
		return "ReportedContentAction"
	default:
		return fmt.Sprintf("Unknown(%d)", uint32(t))
	}
}

func FromProtoNotificationType(t pb.ESteamNotificationType) NotificationType {
	return NotificationType(t)
}

type ItemAnnouncementsEvent struct {
	bus.BaseEvent
	CountNewItems uint32
	UnseenItems   []*pb.CMsgClientItemAnnouncements_UnseenItem
}

type CommentNotificationsEvent struct {
	bus.BaseEvent
	CountNewComments              uint32
	CountNewCommentsOwner         uint32
	CountNewCommentsSubscriptions uint32
}

type UserNotificationsEvent struct {
	bus.BaseEvent
	Notifications map[NotificationType]uint32
}

type OfflineMessagesEvent struct {
	bus.BaseEvent
	OfflineMessages            uint32
	FriendsWithOfflineMessages []id.ID
}

type MarketingMessagesEvent struct {
	bus.BaseEvent
	Timestamp int64
	Messages  []MarketingMessage
}

type MarketingMessage struct {
	ID    string
	URL   string
	Flags uint32
}

type ReceivedEvent struct {
	bus.BaseEvent
	Notifications            []*pb.SteamNotificationData
	PendingGiftCount         uint32
	PendingFriendCount       uint32
	PendingFamilyInviteCount uint32
}
