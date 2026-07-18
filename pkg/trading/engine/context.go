// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"context"
	"sync"

	"github.com/lemon4ksan/miyako/generic"

	"github.com/lemon4ksan/g-man/pkg/trading"
	"github.com/lemon4ksan/g-man/pkg/trading/reason"
)

// Verdict contains the final decision and the reasoning behind it.
type Verdict struct {
	// Action specifies the targeted operation to perform on the offer (e.g. accept, decline).
	Action trading.ActionType
	// Reason is the explanation or rule identifier that justified the action.
	Reason reason.TradeReason
	// Data is an optional payload container, typically holding [trading.CounterParams] for counter-offers.
	Data any
}

// Decision converts the engine verdict into a generic trading ActionDecision
// that the automated Processor understands.
func (v Verdict) Decision() trading.ActionDecision {
	d := trading.ActionDecision{
		Action: v.Action,
		Reason: v.Reason.String(),
	}

	if v.Action == trading.ActionCounter {
		if params, ok := v.Data.(*trading.CounterParams); ok {
			d.CounterParams = params
		}
	}

	// For the automated processor, Review/Ignore/Undecided are treated as Skip
	if d.Action == "" || d.Action == trading.ActionReview || d.Action == trading.ActionIgnore {
		d.Action = trading.ActionSkip
	}

	return d
}

// TradeContext flows through the middleware chain.
//
// It carries the original offer, execution context, and shared metadata.
// The context is concurrent-safe and supports storing arbitrary key-value metadata.
type TradeContext struct {
	context.Context

	// Offer is the trade offer being evaluated by the engine.
	Offer *trading.TradeOffer
	// Verdict is the current decision reached by the middleware chain.
	Verdict Verdict

	// Metadata storage
	mu   sync.RWMutex
	data map[string]any
}

// NewTradeContext creates a fresh context for an incoming trade offer.
func NewTradeContext(ctx context.Context, offer *trading.TradeOffer) *TradeContext {
	return &TradeContext{
		Context: ctx,
		Offer:   offer,
		Verdict: Verdict{Action: trading.ActionSkip}, // Default to skip (undecided)
		data:    make(map[string]any),
	}
}

// Set stores a key-value pair in the context's metadata.
//
// If the key is empty, the function returns early. It is concurrent-safe.
func (c *TradeContext) Set(key string, val any) {
	if key == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.data[key] = val
}

// Get retrieves a value from the context's metadata by key.
func (c *TradeContext) Get(key string) generic.Optional[any] {
	c.mu.RLock()
	defer c.mu.RUnlock()

	val, ok := c.data[key]
	if !ok {
		return generic.None[any]()
	}

	return generic.Some(val)
}

// Accept sets the verdict action to accept and assigns the justification reason.
func (c *TradeContext) Accept(reason reason.TradeReason) {
	c.Verdict = Verdict{Action: trading.ActionAccept, Reason: reason}
}

// Decline sets the verdict action to decline and assigns the justification reason.
func (c *TradeContext) Decline(reason reason.TradeReason) {
	c.Verdict = Verdict{Action: trading.ActionDecline, Reason: reason}
}

// Review sets the verdict action to review and assigns the justification reason.
func (c *TradeContext) Review(reason reason.TradeReason) {
	c.Verdict = Verdict{Action: trading.ActionReview, Reason: reason}
}

// Counter sets the verdict action to counter, assigns the justification reason, and attaches parameters.
func (c *TradeContext) Counter(reason reason.TradeReason, params *trading.CounterParams) {
	c.Verdict = Verdict{Action: trading.ActionCounter, Reason: reason, Data: params}
}
