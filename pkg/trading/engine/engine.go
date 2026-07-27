// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package engine evaluates trade offers using a composable, short-circuiting middleware pipeline.
package engine

import (
	"context"

	"github.com/lemon4ksan/g-man/pkg/trading"
)

type Handler func(ctx *TradeContext) error

type Middleware func(next Handler) Handler

// Engine manages registration and sequential execution of trade offer evaluation middlewares.
type Engine struct {
	middlewares []Middleware
}

func New() *Engine {
	return &Engine{
		middlewares: make([]Middleware, 0),
	}
}

func (e *Engine) Use(mws ...Middleware) {
	e.middlewares = append(e.middlewares, mws...)
}

func (e *Engine) Process(ctx context.Context, offer *trading.TradeOffer) (*Verdict, error) {
	tCtx := NewTradeContext(ctx, offer)

	handler := func(c *TradeContext) error {
		return nil
	}

	for i := len(e.middlewares) - 1; i >= 0; i-- {
		handler = e.middlewares[i](handler)
	}

	err := handler(tCtx)

	return &tCtx.Verdict, err
}
