// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package web

import (
	"github.com/lemon4ksan/foundation/async/event"

	"github.com/lemon4ksan/g-man/pkg/trading"
)

type StateEvent struct {
	event.BaseEvent
	New State
}

type NewOfferEvent struct {
	event.BaseEvent
	Offer *trading.TradeOffer
}

type OfferChangedEvent struct {
	event.BaseEvent
	Offer    *trading.TradeOffer
	OldState trading.OfferState
}

type PollSuccessEvent struct {
	event.BaseEvent
}

type PollDataEvent struct {
	event.BaseEvent
	PollData trading.PollData
}
