// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apps

import "github.com/lemon4ksan/g-man/steam/bus"

// AppLaunchedEvent is emitted when the client reports playing a new game.
type AppLaunchedEvent struct {
	bus.BaseEvent
	AppID uint32
}

// AppQuitEvent is emitted when the client stops playing a game.
type AppQuitEvent struct {
	bus.BaseEvent
	AppID uint32
}

// PlayingStateEvent is emitted when Steam notifies us about our playing status.
// Blocked = true means another session is currently playing a game on this account.
type PlayingStateEvent struct {
	bus.BaseEvent
	Blocked    bool
	PlayingApp uint32
}
