// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package engine evaluates trade offers using a composable, short-circuiting middleware pipeline.
package engine

import (
	"context"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/trading"
)

type Handler func(ctx *TradeContext) error

type Middleware func(next Handler) Handler

// Engine manages registration and sequential execution of trade offer evaluation middlewares.
type Engine struct {
	mu          sync.RWMutex
	middlewares []Middleware
	compiled    Handler
	contextPool sync.Pool
}

func New() *Engine {
	e := &Engine{
		middlewares: make([]Middleware, 0, 8),
	}
	e.contextPool.New = func() any {
		return &TradeContext{
			data: make(map[string]any, 4),
		}
	}
	e.compileLocked()

	return e
}

func (e *Engine) Use(mws ...Middleware) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.middlewares = append(e.middlewares, mws...)
	e.compileLocked()
}

func (e *Engine) compileLocked() {
	handler := func(c *TradeContext) error {
		return nil
	}

	for i := len(e.middlewares) - 1; i >= 0; i-- {
		handler = e.middlewares[i](handler)
	}

	e.compiled = handler
}

func (e *Engine) Process(ctx context.Context, offer *trading.TradeOffer) (*Verdict, error) {
	e.mu.RLock()
	handler := e.compiled
	e.mu.RUnlock()

	tCtx := e.acquireContext(ctx, offer)
	defer e.releaseContext(tCtx)

	err := handler(tCtx)

	verdict := tCtx.Verdict

	return &verdict, err
}

func (e *Engine) acquireContext(ctx context.Context, offer *trading.TradeOffer) *TradeContext {
	tCtx := e.contextPool.Get().(*TradeContext)
	tCtx.Context = ctx
	tCtx.Offer = offer
	tCtx.Verdict = Verdict{Action: trading.ActionSkip}

	return tCtx
}

func (e *Engine) releaseContext(tCtx *TradeContext) {
	if tCtx == nil {
		return
	}

	tCtx.Reset()
	e.contextPool.Put(tCtx)
}
