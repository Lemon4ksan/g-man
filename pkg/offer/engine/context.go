// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"context"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/modules/econ/trading"
	"github.com/lemon4ksan/g-man/pkg/offer/reason"
)

// Action represents the final decision made by the engine regarding a trade.
type Action int

const (
	// ActionUndecided means the chain finished without a definitive conclusion.
	ActionUndecided Action = iota
	// ActionAccept instructs the bot to accept the trade.
	ActionAccept
	// ActionDecline instructs the bot to decline the trade.
	ActionDecline
	// ActionCounter instructs the bot to send a counter-offer.
	ActionCounter
	// ActionReview marks the trade for manual review by an administrator.
	ActionReview
	// ActionIgnore means the offer should be ignored (e.g., already handled or glitched).
	ActionIgnore
)

func (a Action) String() string {
	switch a {
	case ActionUndecided:
		return "UNDECIDED"
	case ActionAccept:
		return "ACCEPT"
	case ActionDecline:
		return "DECLINE"
	case ActionCounter:
		return "COUNTER"
	case ActionReview:
		return "REVIEW"
	case ActionIgnore:
		return "IGNORE"
	default:
		return "UNKNOWN"
	}
}

// Verdict contains the final decision and the reasoning behind it.
type Verdict struct {
	Action Action
	Reason reason.TradeReason
	Data   any // Optional payload (e.g., CounterOffer struct)
}

// TradeContext flows through the middleware chain.
// It carries the original offer, execution context, and shared metadata.
type TradeContext struct {
	context.Context

	Offer   *trading.TradeOffer
	Verdict Verdict

	// Metadata storage
	mu   sync.RWMutex
	data map[string]any
}

// NewTradeContext creates a fresh context for an incoming offer.
func NewTradeContext(ctx context.Context, offer *trading.TradeOffer) *TradeContext {
	return &TradeContext{
		Context: ctx,
		Offer:   offer,
		Verdict: Verdict{Action: ActionUndecided},
		data:    make(map[string]any),
	}
}

// Set stores a key-value pair in the context's metadata. Safe for concurrent use.
func (c *TradeContext) Set(key string, val any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[key] = val
}

// Get retrieves a value from the context's metadata.
func (c *TradeContext) Get(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	val, ok := c.data[key]
	return val, ok
}

// Accept sets the verdict to ACCEPT and stops further reasoning.
func (c *TradeContext) Accept(reason reason.TradeReason) {
	c.Verdict = Verdict{Action: ActionAccept, Reason: reason}
}

// Decline sets the verdict to DECLINE and stops further reasoning.
func (c *TradeContext) Decline(reason reason.TradeReason) {
	c.Verdict = Verdict{Action: ActionDecline, Reason: reason}
}

// Review marks the offer for manual review.
func (c *TradeContext) Review(reason reason.TradeReason) {
	c.Verdict = Verdict{Action: ActionReview, Reason: reason}
}
