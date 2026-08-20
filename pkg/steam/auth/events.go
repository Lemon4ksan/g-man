// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth

import (
	"github.com/lemon4ksan/foundation/async/event"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
)

// StateEvent is emitted when the authenticator transitions between lifecycle states.
type StateEvent struct {
	event.BaseEvent
	Old State
	New State
}

// LoggedOnEvent is emitted after successful authentication with a Connection Manager.
type LoggedOnEvent struct {
	event.BaseEvent
	ClientInstanceID uint32
	CellID           uint32
	PublicIP         uint32
	SteamID          uint64
	Body             *pb.CMsgClientLogonResponse
}

// SteamGuardRequiredEvent is emitted when password logon requires mobile or email Steam Guard verification codes.
type SteamGuardRequiredEvent struct {
	event.BaseEvent
	IsAppConfirm bool
	Is2FA        bool
	EmailDomain  string
	Callback     func(code string)
}
