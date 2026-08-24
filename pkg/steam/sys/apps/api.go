// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:generate aoni-gen -file=api.go

package apps

import (
	"context"

	pb "github.com/lemon4ksan/g-man/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

var (
	_ enums.EMsg
	_ module.InitContext
)

// Events defines typed RPC, Notify, and Event operations for Steam apps and games.
//
// @aoni:service
// @protocol rpc
// @requester "module.InitContext"
type Events interface {
	// @event enums.EMsg_ClientPlayingSessionState
	OnPlayingSessionState(handler func(msg *pb.CMsgClientPlayingSessionState)) (unsubscribe func())

	// @event enums.EMsg_ClientLicenseList
	OnLicenseList(handler func(msg *pb.CMsgClientLicenseList)) (unsubscribe func())

	// @event enums.EMsg_ClientGameConnectTokens
	OnGameConnectTokens(handler func(msg *pb.CMsgClientGameConnectTokens)) (unsubscribe func())

	// --- Outbound RPCs ---

	// @op enums.EMsg_ClientGetNumberOfCurrentPlayersDP
	GetPlayerCount(
		ctx context.Context,
		req *pb.CMsgDPGetNumberOfCurrentPlayers,
	) (*pb.CMsgDPGetNumberOfCurrentPlayersResponse, error)

	// --- Outbound Notifications (One-Way) ---

	// @notify
	// @op enums.EMsg_ClientGamesPlayedWithDataBlob
	GamesPlayed(ctx context.Context, req *pb.CMsgClientGamesPlayed) error

	// @notify
	// @op enums.EMsg_ClientKickPlayingSession
	KickPlayingSession(ctx context.Context, req *pb.CMsgClientKickPlayingSession) error

	// Close unsubscribes all active event listeners.
	Close() error
}
