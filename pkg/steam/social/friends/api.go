// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:generate aoni-gen -file=api.go

package friends

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

	// @op enums.EMsg_ClientAddFriend
	// @notify
	AddFriend(ctx context.Context, req *pb.CMsgClientAddFriend) error

	// @op enums.EMsg_ClientRemoveFriend
	// @notify
	RemoveFriend(ctx context.Context, req *pb.CMsgClientRemoveFriend) error

	// @op enums.EMsg_ClientChangeStatus
	// @notify
	ChangeStatus(ctx context.Context, req *pb.CMsgClientChangeStatus) error

	// @op enums.EMsg_ClientCurrentUIMode
	// @notify
	SetUIMode(ctx context.Context, req *pb.CMsgClientUIMode) error

	// @op enums.EMsg_ClientRichPresenceUpload
	// @notify
	UploadRichPresence(ctx context.Context, req *pb.CMsgClientRichPresenceUpload) error

	// @op enums.EMsg_ClientRequestFriendData
	// @notify
	RequestFriendData(ctx context.Context, req *pb.CMsgClientRequestFriendData) error

	// Close unsubscribes all active event listeners.
	Close() error
}
