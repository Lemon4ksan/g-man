// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apps

import (
	"github.com/lemon4ksan/miyako/bus"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
)

// AppLaunchedEvent is emitted when the client reports playing a new game.
type AppLaunchedEvent struct {
	bus.BaseEvent
	// AppID is the Steam AppID of the launched game.
	AppID uint32
}

// AppQuitEvent is emitted when the client stops playing a game.
type AppQuitEvent struct {
	bus.BaseEvent
	// AppID is the Steam AppID of the stopped game.
	AppID uint32
}

// PlayingStateEvent is emitted when Steam notifies us about our playing status.
// Blocked is true when another session is currently playing a game on this account.
type PlayingStateEvent struct {
	bus.BaseEvent
	// Blocked is true if another active session is currently playing a game on this account.
	Blocked bool
	// PlayingApp is the Steam AppID currently being played by the conflicting session.
	PlayingApp uint32
}

// LicensesEvent is emitted when Steam sends us the current license list.
type LicensesEvent struct {
	bus.BaseEvent
	Licenses []*pb.CMsgClientLicenseList_License
}

// GameConnectTokensEvent is emitted when Steam sends us new game connect tokens.
type GameConnectTokensEvent struct {
	bus.BaseEvent
	Tokens [][]byte
}
