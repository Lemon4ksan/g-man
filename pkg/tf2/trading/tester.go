// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trading

import (
	"context"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/trading"
	"github.com/lemon4ksan/g-man/pkg/trading/engine"
)

// TF2TradeTester provides a fluent API for testing TF2 trade middlewares.
type TF2TradeTester struct {
	engine *engine.Engine
	logger log.Logger
}

// NewTF2TradeTester creates a new testing harness.
func NewTF2TradeTester() *TF2TradeTester {
	return &TF2TradeTester{
		engine: engine.New(),
		logger: log.Discard,
	}
}

// WithPrices sets up a mock pricer middleware.
func (t *TF2TradeTester) WithPrices(prices map[string]int) *TF2TradeTester {
	t.engine.Use(func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			for sku, price := range prices {
				ctx.Set("price_"+sku, price) // Mocking the internal context state
			}

			return next(ctx)
		}
	})

	return t
}

// AddMiddleware allows injecting custom middlewares for testing specific segments.
func (t *TF2TradeTester) AddMiddleware(m engine.Middleware) *TF2TradeTester {
	t.engine.Use(m)
	return t
}

// Run executes the trade offer through the configured middleware chain.
func (t *TF2TradeTester) Run(ctx context.Context, offer *trading.TradeOffer) (engine.Verdict, error) {
	verdict, err := t.engine.Process(ctx, offer)
	if err != nil {
		return engine.Verdict{}, err
	}

	return *verdict, nil
}
