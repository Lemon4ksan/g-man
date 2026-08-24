// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:generate aoni-gen -file=api.go

package account

import (
	pb "github.com/lemon4ksan/g-man/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

var (
	_ enums.EMsg
	_ module.InitContext
)

// Events defines typed subscription hooks for inbound Steam account events.
//
// @aoni:service
// @protocol rpc
// @requester "module.InitContext"
type Events interface {
	// @event enums.EMsg_ClientAccountInfo
	OnAccountInfo(handler func(msg *pb.CMsgClientAccountInfo)) (unsubscribe func())

	// @event enums.EMsg_ClientEmailAddrInfo
	OnEmailAddrInfo(handler func(msg *pb.CMsgClientEmailAddrInfo)) (unsubscribe func())

	// @event enums.EMsg_ClientIsLimitedAccount
	OnIsLimitedAccount(handler func(msg *pb.CMsgClientIsLimitedAccount)) (unsubscribe func())

	// @event enums.EMsg_ClientVACBanStatus
	// @return body | parseVACBans
	OnVACBanStatus(handler func(bans *VACBansEvent)) (unsubscribe func())

	// @event enums.EMsg_ClientWalletInfoUpdate
	OnWalletInfoUpdate(handler func(msg *pb.CMsgClientWalletInfoUpdate)) (unsubscribe func())

	// @event enums.EMsg_ClientVanityURLChangedNotification
	OnVanityURLChanged(handler func(msg *pb.CMsgClientVanityURLChangedNotification)) (unsubscribe func())

	// @event enums.EMsg_ClientUpdateGuestPassesList
	// @return body | parseGuestPasses
	OnUpdateGuestPassesList(handler func(gifts []map[string]any)) (unsubscribe func())

	// Close unsubscribes all active event listeners.
	Close() error
}
