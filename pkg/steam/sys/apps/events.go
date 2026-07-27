// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apps

import (
	"github.com/lemon4ksan/miyako/bus"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
)

type AppLaunchedEvent struct {
	bus.BaseEvent
	AppID uint32
}

type AppQuitEvent struct {
	bus.BaseEvent
	AppID uint32
}

type PlayingStateEvent struct {
	bus.BaseEvent
	Blocked    bool
	PlayingApp uint32
}

type LicensesEvent struct {
	bus.BaseEvent
	Licenses []*pb.CMsgClientLicenseList_License
}

type GameConnectTokensEvent struct {
	bus.BaseEvent
	Tokens [][]byte
}
