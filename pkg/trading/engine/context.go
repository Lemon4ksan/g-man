// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"context"
	"sync"

	"github.com/lemon4ksan/foundation/generic"

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

// Key defines a typed key for accessing TradeContext metadata with compile-time type safety.
type Key[T any] struct {
	name string
}

// NewKey creates a new typed context key.
func NewKey[T any](name string) Key[T] {
	return Key[T]{name: name}
}

// Name returns the underlying key name.
func (k Key[T]) Name() string {
	return k.name
}

// GetKey retrieves a typed value from the TradeContext without manual type assertions.
func GetKey[T any](c *TradeContext, k Key[T]) (T, bool) {
	if c == nil || k.name == "" {
		var zero T
		return zero, false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	val, ok := c.data[k.name]
	if !ok {
		var zero T
		return zero, false
	}

	typed, ok := val.(T)
	if !ok {
		var zero T
		return zero, false
	}

	return typed, true
}

// SetKey stores a typed value into the TradeContext with compile-time type safety.
func SetKey[T any](c *TradeContext, k Key[T], val T) {
	if c == nil || k.name == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.data[k.name] = val
}

// TradeContext flows through the middleware chain, carrying offer details, context, and shared metadata.
//
// Thread Safety:
//   - Safe for concurrent use across middleware goroutines.
type TradeContext struct {
	context.Context

	Offer   *trading.TradeOffer
	Verdict Verdict

	mu     sync.RWMutex
	data   map[string]any
	locked bool
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
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.locked {
		return
	}
	c.Verdict = Verdict{Action: trading.ActionAccept, Reason: reason}
}

func (c *TradeContext) Decline(reason reason.TradeReason) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.locked {
		return
	}
	c.Verdict = Verdict{Action: trading.ActionDecline, Reason: reason}
}

func (c *TradeContext) Review(reason reason.TradeReason) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.locked {
		return
	}
	c.Verdict = Verdict{Action: trading.ActionReview, Reason: reason}
}

func (c *TradeContext) Counter(reason reason.TradeReason, params *trading.CounterParams) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.locked {
		return
	}
	c.Verdict = Verdict{Action: trading.ActionCounter, Reason: reason, Data: params}
}

// Lock prevents any subsequent middleware from altering the current verdict.
func (c *TradeContext) Lock() {
	c.mu.Lock()
	c.locked = true
	c.mu.Unlock()
}

// IsLocked reports whether the current verdict is locked against modifications.
func (c *TradeContext) IsLocked() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.locked
}

// Finalize sets the verdict and immediately locks it against downstream overwrites.
func (c *TradeContext) Finalize(action trading.ActionType, reason reason.TradeReason) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.locked {
		return false
	}
	c.Verdict = Verdict{Action: action, Reason: reason}
	c.locked = true
	return true
}

// Reset clears the trade context state preparing it for pool recycling.
func (c *TradeContext) Reset() {
	c.Context = nil
	c.Offer = nil
	c.Verdict = Verdict{}

	c.mu.Lock()
	clear(c.data)
	c.locked = false
	c.mu.Unlock()
}
