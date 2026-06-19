// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package web

import (
	"github.com/lemon4ksan/miyako/bus"

	"github.com/lemon4ksan/g-man/pkg/trading"
)

// StateEvent is emitted whenever the manager transitions between state lifecycles.
type StateEvent struct {
	bus.BaseEvent
	// New represents the target state the manager transitioned to.
	New State
}

// NewOfferEvent is emitted when a new active trade offer is received.
type NewOfferEvent struct {
	bus.BaseEvent
	// Offer is the newly received trade offer details.
	Offer *trading.TradeOffer
}

// OfferChangedEvent is emitted when a tracked offer changes its transactional state.
type OfferChangedEvent struct {
	bus.BaseEvent
	// Offer is the updated trade offer details.
	Offer *trading.TradeOffer
	// OldState is the state the offer transitioned away from.
	OldState trading.OfferState
}

// PollSuccessEvent is emitted after a successful poll cycle completes.
type PollSuccessEvent struct {
	bus.BaseEvent
}

// PollDataEvent is emitted when polling state changes and needs to be persisted.
type PollDataEvent struct {
	bus.BaseEvent
	// PollData contains the serialized state tracking details.
	PollData trading.PollData
}
