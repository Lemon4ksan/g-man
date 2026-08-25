// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:generate aoni-gen -file=api.go

package friends

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

// Events defines typed RPC, Notify, and Event operations for friends and persona state management.
//
// @aoni:service
// @protocol rpc
// @requester "module.InitContext"
type Events interface {
	// @event enums.EMsg_ClientFriendsList
	OnFriendsList(handler func(msg *pb.CMsgClientFriendsList)) (unsubscribe func())

	// @event enums.EMsg_ClientPersonaState
	OnPersonaState(handler func(msg *pb.CMsgClientPersonaState)) (unsubscribe func())

	// @event enums.EMsg_ClientFriendsGroupsList
	OnFriendsGroupsList(handler func(msg *pb.CMsgClientFriendsGroupsList)) (unsubscribe func())

	// @event enums.EMsg_ClientPlayerNicknameList
	OnPlayerNicknameList(handler func(msg *pb.CMsgClientPlayerNicknameList)) (unsubscribe func())

	// @event "PlayerClient.NotifyFriendNicknameChanged#1"
	OnNotifyFriendNicknameChanged(handler func(msg *pb.CPlayer_FriendNicknameChanged_Notification)) (unsubscribe func())

	// --- Outbound Notifications (One-Way) ---

	// @notify
	// @op enums.EMsg_ClientAddFriend
	AddFriend(ctx context.Context, req *pb.CMsgClientAddFriend) error

	// @notify
	// @op enums.EMsg_ClientRemoveFriend
	RemoveFriend(ctx context.Context, req *pb.CMsgClientRemoveFriend) error

	// @notify
	// @op enums.EMsg_ClientChangeStatus
	ChangeStatus(ctx context.Context, req *pb.CMsgClientChangeStatus) error

	// @notify
	// @op enums.EMsg_ClientCurrentUIMode
	SetUIMode(ctx context.Context, req *pb.CMsgClientUIMode) error

	// @notify
	// @op enums.EMsg_ClientRichPresenceUpload
	UploadRichPresence(ctx context.Context, req *pb.CMsgClientRichPresenceUpload) error

	// @notify
	// @op enums.EMsg_ClientRequestFriendData
	RequestFriendData(ctx context.Context, req *pb.CMsgClientRequestFriendData) error

	// Close unsubscribes all active event listeners.
	Close() error
}
