// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth

import (
	"github.com/lemon4ksan/miyako/bus"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

// StateEvent is emitted whenever the authenticator transitions between states.
type StateEvent struct {
	bus.BaseEvent
	// Old is the previous authentication state.
	Old State
	// New is the updated authentication state.
	New State
}

// LoggedOnEvent is emitted after successful authentication with Steam.
// This indicates that the client is fully logged on and ready to use.
// It contains details about the logged-in session provided by the server.
type LoggedOnEvent struct {
	bus.BaseEvent
	// ClientInstanceID is the unique instance identifier for this client session.
	ClientInstanceID uint32
	// CellID is the content delivery region identifier assigned by Steam.
	CellID uint32
	// PublicIP is the client's public IP address as seen by Steam.
	PublicIP uint32
	// SteamID is the unique 64-bit identifier of the authenticated user.
	SteamID uint64
	// Body is the complete underlying logon response packet from Steam.
	Body *pb.CMsgClientLogonResponse
}

// SteamGuardRequiredEvent is emitted during password-based authentication
// when Steam Guard verification is required. The user must provide a code
// from email or mobile authenticator and call the Callback function.
type SteamGuardRequiredEvent struct {
	bus.BaseEvent
	// IsAppConfirm is true if the confirmation must be completed via the mobile app.
	IsAppConfirm bool
	// Is2FA is true if the code is a mobile authenticator code, false if it is an email code.
	Is2FA bool
	// EmailDomain is the target domain name where the email confirmation was sent.
	EmailDomain string
	// Callback is the handler function that must be called with the user-provided code to continue login.
	Callback func(code string)
}

// LoggedOffEvent is emitted after the auth client disconnected from CM server unexpectedly.
type LoggedOffEvent struct {
	bus.BaseEvent
	// Result is the Steam EResult code explaining why the client was logged off.
	Result enums.EResult
}
