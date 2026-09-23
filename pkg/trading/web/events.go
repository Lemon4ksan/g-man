// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package web

import (
	"github.com/lemon4ksan/foundation/async/event"

	"github.com/lemon4ksan/g-man/pkg/trading"
)

// StateEvent is emitted when the trade manager polling FSM transitions to a new lifecycle state.
type StateEvent struct {
	event.BaseEvent
	New State
}

// NewOfferEvent is emitted when a new incoming or outgoing trade offer is discovered during polling.
type NewOfferEvent struct {
	event.BaseEvent
	Offer *trading.TradeOffer
}

// OfferChangedEvent is emitted when an existing trade offer transitions from OldState to a new OfferState.
type OfferChangedEvent struct {
	event.BaseEvent
	Offer    *trading.TradeOffer
	OldState trading.OfferState
}

// PollSuccessEvent is emitted upon the completion of a successful trade offer poll cycle.
type PollSuccessEvent struct {
	event.BaseEvent
}

// PollDataEvent is emitted when polling state data changes, containing updated offer states and timestamps.
type PollDataEvent struct {
	event.BaseEvent
	PollData trading.PollData
}
