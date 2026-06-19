// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package live

import (
	"github.com/lemon4ksan/miyako/bus"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

// TradeProposedEvent is emitted when an incoming live trade request is received from another user.
type TradeProposedEvent struct {
	bus.BaseEvent
	// OtherSteamID is the 64-bit Steam ID of the user who initiated the trade proposal.
	OtherSteamID uint64
	// TradeID is the unique identifier of the proposed trade request.
	TradeID uint32
	// Respond is a callback function that allows accepting or declining the proposal.
	Respond func(accept bool)
}

// TradeResultEvent is emitted when a trade request is answered or fails.
type TradeResultEvent struct {
	bus.BaseEvent
	// OtherSteamID is the 64-bit Steam ID of the trade partner.
	OtherSteamID uint64
	// Response is the result status returned by Steam.
	Response enums.EEconTradeResponse
	// SteamGuardRequiredDays is the number of Steam Guard days required to trade.
	SteamGuardRequiredDays uint32
	// NewDeviceCooldownDays is the number of cooldown days applied due to a new login device.
	NewDeviceCooldownDays uint32
}

// TradeSessionStartedEvent is emitted when the trade window is officially open.
type TradeSessionStartedEvent struct {
	bus.BaseEvent
	// OtherSteamID is the 64-bit Steam ID of the trade partner.
	OtherSteamID uint64
}
