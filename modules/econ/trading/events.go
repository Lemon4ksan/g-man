// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trading

import (
	"github.com/lemon4ksan/g-man/modules/econ"
	"github.com/lemon4ksan/g-man/steam/bus"
)

// NewOfferEvent is emitted when a new trade offer is received.
type NewOfferEvent struct {
	bus.BaseEvent
	Offer *TradeOffer
}

func (e *NewOfferEvent) Topic() string { return "offers.new" }

// OfferChangedEvent is emitted when a tracked offer changes state (e.g. Accepted, Declined).
type OfferChangedEvent struct {
	bus.BaseEvent
	Offer    *TradeOffer
	OldState econ.TradeOfferState
}

func (e *OfferChangedEvent) Topic() string { return "offers.changed" }

// PollSuccessEvent is emitted after a successful poll cycle.
type PollSuccessEvent struct {
	bus.BaseEvent
}

func (e *PollSuccessEvent) Topic() string { return "offers.poll_success" }