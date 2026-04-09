// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"context"

	"github.com/lemon4ksan/g-man/pkg/trading/web"
)

// Handler represents a function that processes a TradeContext.
type Handler func(ctx *TradeContext) error

// Middleware represents a component in the processing chain.
// It takes the *next* handler in the chain and returns a *new* handler
// that wraps its own logic around the next one.
type Middleware func(next Handler) Handler

// Engine orchestrates the execution of trade middlewares.
type Engine struct {
	middlewares []Middleware
}

// New creates a new Trade Middleware Engine.
func New() *Engine {
	return &Engine{
		middlewares: make([]Middleware, 0),
	}
}

// Use appends one or more middlewares to the execution chain.
// Order matters: middlewares added first will execute first.
func (e *Engine) Use(mws ...Middleware) {
	e.middlewares = append(e.middlewares, mws...)
}

// Process passes the TradeOffer through the entire middleware chain.
// It returns the final Verdict reached by the chain.
func (e *Engine) Process(ctx context.Context, offer *trading.TradeOffer) (*Verdict, error) {
	tCtx := NewTradeContext(ctx, offer)

	// The innermost handler: reached only if no middleware sets a definitive verdict
	// and stops the chain.
	handler := func(c *TradeContext) error {
		return nil
	}

	// Build the chain from the inside out.
	// This ensures the first middleware added is the first one executed.
	for i := range len(e.middlewares) {
		handler = e.middlewares[i](handler)
	}

	// Execute the chain
	err := handler(tCtx)

	return &tCtx.Verdict, err
}
