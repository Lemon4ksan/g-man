// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package live

import (
	"github.com/lemon4ksan/foundation/async/event"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

type TradeProposedEvent struct {
	event.BaseEvent
	OtherSteamID uint64
	TradeID      uint32
	Respond      func(accept bool)
}

type TradeResultEvent struct {
	event.BaseEvent
	OtherSteamID           uint64
	Response               enums.EEconTradeResponse
	SteamGuardRequiredDays uint32
	NewDeviceCooldownDays  uint32
}

type TradeSessionStartedEvent struct {
	event.BaseEvent
	OtherSteamID uint64
}
