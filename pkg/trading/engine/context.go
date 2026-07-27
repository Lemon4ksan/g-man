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

type Verdict struct {
	Action trading.ActionType
	Reason reason.TradeReason
	Data   any
}

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

	if d.Action == "" || d.Action == trading.ActionReview || d.Action == trading.ActionIgnore {
		d.Action = trading.ActionSkip
	}

	return d
}

// TradeContext flows through the middleware chain, carrying offer details, context, and shared metadata.
//
// Thread Safety:
//   - Safe for concurrent use across middleware goroutines.
type TradeContext struct {
	context.Context

	Offer   *trading.TradeOffer
	Verdict Verdict

	mu   sync.RWMutex
	data map[string]any
}

func NewTradeContext(ctx context.Context, offer *trading.TradeOffer) *TradeContext {
	return &TradeContext{
		Context: ctx,
		Offer:   offer,
		Verdict: Verdict{Action: trading.ActionSkip},
		data:    make(map[string]any),
	}
}

func (c *TradeContext) Set(key string, val any) {
	if key == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.data[key] = val
}

func (c *TradeContext) Get(key string) generic.Optional[any] {
	c.mu.RLock()
	defer c.mu.RUnlock()

	val, ok := c.data[key]
	if !ok {
		return generic.None[any]()
	}

	return generic.Some(val)
}

func (c *TradeContext) Accept(reason reason.TradeReason) {
	c.Verdict = Verdict{Action: trading.ActionAccept, Reason: reason}
}

func (c *TradeContext) Decline(reason reason.TradeReason) {
	c.Verdict = Verdict{Action: trading.ActionDecline, Reason: reason}
}

func (c *TradeContext) Review(reason reason.TradeReason) {
	c.Verdict = Verdict{Action: trading.ActionReview, Reason: reason}
}

func (c *TradeContext) Counter(reason reason.TradeReason, params *trading.CounterParams) {
	c.Verdict = Verdict{Action: trading.ActionCounter, Reason: reason, Data: params}
}
