// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:generate aoni-gen -file=api.go

package chat

import (
	"context"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

var (
	_ enums.EMsg
	_ module.InitContext
)

// Events defines typed RPC, Notify, and Event operations for Steam friend and group chats.
//
// @aoni:service
// @protocol rpc
// @requester "module.InitContext"
type Events interface {
	// --- Inbound Events ---

	// @event "FriendMessagesClient.IncomingMessage#1"
	OnIncomingMessage(handler func(msg *pb.CFriendMessages_IncomingMessage_Notification)) (unsubscribe func())

	// @event "ChatRoomClient.NotifyIncomingChatMessage#1"
	OnIncomingGroupMessage(handler func(msg *pb.CChatRoom_IncomingChatMessage_Notification)) (unsubscribe func())

	// @event "FriendMessagesClient.MessageReaction#1"
	OnFriendReaction(handler func(msg *pb.CFriendMessages_MessageReaction_Notification)) (unsubscribe func())

	// @event "ChatRoomClient.NotifyMessageReaction#1"
	OnGroupReaction(handler func(msg *pb.CChatRoom_MessageReaction_Notification)) (unsubscribe func())

	// @event enums.EMsg_ClientFriendMsgIncoming
	OnLegacyFriendMsg(handler func(msg *pb.CMsgClientFriendMsgIncoming)) (unsubscribe func())

	// --- Friend Messages Operations ---

	// @op "FriendMessages.SendMessage#1"
	SendMessage(
		ctx context.Context,
		req *pb.CFriendMessages_SendMessage_Request,
	) (*pb.CFriendMessages_SendMessage_Response, error)

	// @notify
	// @op "FriendMessages.AckMessage#1"
	AckFriendMessage(ctx context.Context, req *pb.CFriendMessages_AckMessage_Notification) error

	// @op "FriendMessages.GetRecentMessages#1"
	GetRecentMessages(
		ctx context.Context,
		req *pb.CFriendMessages_GetRecentMessages_Request,
	) (*pb.CFriendMessages_GetRecentMessages_Response, error)

	// @op "FriendMessages.GetActiveMessageSessions#1"
	GetActiveMessageSessions(
		ctx context.Context,
		req *pb.CFriendsMessages_GetActiveMessageSessions_Request,
	) (*pb.CFriendsMessages_GetActiveMessageSessions_Response, error)

	// --- Group Chat Operations ---

	// @op "ChatRoom.SendChatMessage#1"
	SendChatMessage(
		ctx context.Context,
		req *pb.CChatRoom_SendChatMessage_Request,
	) (*pb.CChatRoom_SendChatMessage_Response, error)

	// @op "ChatRoom.UpdateMessageReaction#1"
	UpdateMessageReaction(
		ctx context.Context,
		req *pb.CChatRoom_UpdateMessageReaction_Request,
	) (*pb.CChatRoom_UpdateMessageReaction_Response, error)

	// @op "ChatRoom.GetMessageHistory#1"
	GetMessageHistory(
		ctx context.Context,
		req *pb.CChatRoom_GetMessageHistory_Request,
	) (*pb.CChatRoom_GetMessageHistory_Response, error)

	// @op "ChatRoom.JoinChatRoomGroup#1"
	JoinChatRoomGroup(
		ctx context.Context,
		req *pb.CChatRoom_JoinChatRoomGroup_Request,
	) (*pb.CChatRoom_JoinChatRoomGroup_Response, error)

	// @op "ChatRoom.LeaveChatRoomGroup#1"
	LeaveChatRoomGroup(
		ctx context.Context,
		req *pb.CChatRoom_LeaveChatRoomGroup_Request,
	) (*pb.CChatRoom_LeaveChatRoomGroup_Response, error)

	// @op "ChatRoom.DeleteChatMessages#1"
	DeleteChatMessages(
		ctx context.Context,
		req *pb.CChatRoom_DeleteChatMessages_Request,
	) (*pb.CChatRoom_DeleteChatMessages_Response, error)

	// @notify
	// @op "ChatRoom.AckChatMessage#1"
	AckChatMessage(ctx context.Context, req *pb.CChatRoom_AckChatMessage_Notification) error

	// @op "ChatRoom.InviteFriendToChatRoomGroup#1"
	InviteFriendToChatRoomGroup(
		ctx context.Context,
		req *pb.CChatRoom_InviteFriendToChatRoomGroup_Request,
	) (*pb.CChatRoom_InviteFriendToChatRoomGroup_Response, error)

	// @op "ChatRoom.KickUser#1"
	KickUser(ctx context.Context, req *pb.CChatRoom_KickUser_Request) (*pb.CChatRoom_KickUser_Response, error)

	// @op "ChatRoom.MuteUser#1"
	MuteUser(ctx context.Context, req *pb.CChatRoom_MuteUser_Request) (*pb.CChatRoom_MuteUser_Response, error)

	// @op "ChatRoom.SetUserBanState#1"
	SetUserBanState(
		ctx context.Context,
		req *pb.CChatRoom_SetUserBanState_Request,
	) (*pb.CChatRoom_SetUserBanState_Response, error)

	// @op "ChatRoom.CreateChatRoomGroup#1"
	CreateChatRoomGroup(
		ctx context.Context,
		req *pb.CChatRoom_CreateChatRoomGroup_Request,
	) (*pb.CChatRoom_CreateChatRoomGroup_Response, error)

	// @op "ChatRoom.SaveChatRoomGroup#1"
	SaveChatRoomGroup(
		ctx context.Context,
		req *pb.CChatRoom_SaveChatRoomGroup_Request,
	) (*pb.CChatRoom_SaveChatRoomGroup_Response, error)

	// @op "ChatRoom.RenameChatRoomGroup#1"
	RenameChatRoomGroup(
		ctx context.Context,
		req *pb.CChatRoom_RenameChatRoomGroup_Request,
	) (*pb.CChatRoom_RenameChatRoomGroup_Response, error)

	// @op "ChatRoom.GetMyChatRoomGroups#1"
	GetMyChatRoomGroups(
		ctx context.Context,
		req *pb.CChatRoom_GetMyChatRoomGroups_Request,
	) (*pb.CChatRoom_GetMyChatRoomGroups_Response, error)

	// @op "ChatRoom.GetChatRoomGroupState#1"
	GetChatRoomGroupState(
		ctx context.Context,
		req *pb.CChatRoom_GetChatRoomGroupState_Request,
	) (*pb.CChatRoom_GetChatRoomGroupState_Response, error)

	// @op "ChatRoom.CreateInviteLink#1"
	CreateInviteLink(
		ctx context.Context,
		req *pb.CChatRoom_CreateInviteLink_Request,
	) (*pb.CChatRoom_CreateInviteLink_Response, error)

	// @op "ChatRoom.GetInviteLinksForGroup#1"
	GetInviteLinksForGroup(
		ctx context.Context,
		req *pb.CChatRoom_GetInviteLinksForGroup_Request,
	) (*pb.CChatRoom_GetInviteLinksForGroup_Response, error)

	// @op "ChatRoom.DeleteInviteLink#1"
	DeleteInviteLink(
		ctx context.Context,
		req *pb.CChatRoom_DeleteInviteLink_Request,
	) (*pb.CChatRoom_DeleteInviteLink_Response, error)

	// Close unsubscribes all active event listeners.
	Close() error
}
