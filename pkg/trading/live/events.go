// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package live

import (
	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/protocol"
)

// TradeProposedEvent is emitted when someone invites us to a live trade.
type TradeProposedEvent struct {
	bus.BaseEvent
	OtherSteamID uint64
	TradeID      uint32
	// Respond allows the user to accept or decline directly from the event.
	Respond func(accept bool)
}

func (e *TradeProposedEvent) Topic() string { return "trading.proposed" }

// TradeResultEvent is emitted when a trade request is answered or fails.
type TradeResultEvent struct {
	bus.BaseEvent
	OtherSteamID uint64
	Response     protocol.EEconTradeResponse
	// Probation/Cooldown info (useful for logs)
	SteamGuardRequiredDays uint32
	NewDeviceCooldownDays  uint32
}

func (e *TradeResultEvent) Topic() string { return "trading.result" }

// TradeSessionStartedEvent is emitted when the trade window is officially open.
type TradeSessionStartedEvent struct {
	bus.BaseEvent
	OtherSteamID uint64
}

func (e *TradeSessionStartedEvent) Topic() string { return "trading.started" }
