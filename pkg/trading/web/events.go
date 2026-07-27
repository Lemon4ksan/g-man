// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package web

import (
	"github.com/lemon4ksan/miyako/bus"

	"github.com/lemon4ksan/g-man/pkg/trading"
)

type StateEvent struct {
	bus.BaseEvent
	New State
}

type NewOfferEvent struct {
	bus.BaseEvent
	Offer *trading.TradeOffer
}

type OfferChangedEvent struct {
	bus.BaseEvent
	Offer    *trading.TradeOffer
	OldState trading.OfferState
}

type PollSuccessEvent struct {
	bus.BaseEvent
}

type PollDataEvent struct {
	bus.BaseEvent
	PollData trading.PollData
}
