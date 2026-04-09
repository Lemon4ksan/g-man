// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth

import (
	"github.com/lemon4ksan/g-man/pkg/bus"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/protocol"
)

// AuthEvent is a marker interface for all auth events.
// Events are emitted via the Events() channel and allow consumers to
// react to connection lifecycle changes, messages, and errors.
type AuthEvent interface {
	bus.Event
	IsAuthEvent()
}

// StateEvent is emitted whenever the authenticator transitions between states.
type StateEvent struct {
	bus.BaseEvent
	Old State
	New State
}

func (e StateEvent) IsAuthEvent() {}

// LoggedOnEvent is emitted after successful authentication with Steam.
// This indicates that the client is fully logged on and ready to use.
// It contains details about the logged-in session provided by the server.
type LoggedOnEvent struct {
	bus.BaseEvent
	ClientInstanceID uint32                      // Instance ID for this client session
	CellID           uint32                      // Content delivery region/cell ID
	PublicIP         uint32                      // Client's public IP as seen by Steam
	SteamID          uint64                      // Logged-in user's SteamID
	Body             *pb.CMsgClientLogonResponse // Complete logon response for advanced use
}

func (e LoggedOnEvent) IsAuthEvent() {}

// SteamGuardRequiredEvent is emitted during password-based authentication
// when Steam Guard verification is required. The user must provide a code
// from email or mobile authenticator and call the Callback function.
type SteamGuardRequiredEvent struct {
	bus.BaseEvent
	IsAppConfirm bool              // True = must approve by pressing "confirm" in the mobile app
	Is2FA        bool              // True = mobile authenticator (2FA), False = email code
	EmailDomain  string            // For email codes, the domain of the email address (if known)
	Callback     func(code string) // Function to call with the user-provided code to continue login
}

func (e SteamGuardRequiredEvent) IsAuthEvent() {}

// WebSessionReadyEvent is emitted after the successful web session refresh
type WebSessionReadyEvent struct {
	bus.BaseEvent
}

// LoggedOffEvent is emitted after the auth client disconnected from CM server unexpectedly.
type LoggedOffEvent struct {
	bus.BaseEvent
	Result protocol.EResult
}

func (e LoggedOffEvent) IsAuthEvent() {}
