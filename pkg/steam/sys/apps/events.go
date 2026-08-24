// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apps

import (
	"github.com/lemon4ksan/foundation/async/event"

	pb "github.com/lemon4ksan/g-man/protobuf/steam"
)

type AppLaunchedEvent struct {
	event.BaseEvent
	AppID uint32
}

type AppQuitEvent struct {
	event.BaseEvent
	AppID uint32
}

type PlayingStateEvent struct {
	event.BaseEvent
	Blocked    bool
	PlayingApp uint32
}

type LicensesEvent struct {
	event.BaseEvent
	Licenses []*pb.CMsgClientLicenseList_License
}

type GameConnectTokensEvent struct {
	event.BaseEvent
	Tokens [][]byte
}
