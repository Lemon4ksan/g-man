// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trading

import (
	"context"

	"github.com/lemon4ksan/g-man/pkg/trading"
	"github.com/lemon4ksan/g-man/pkg/trading/engine"
)

// TradeTester is a helper for testing trading engines and middlewares.
// It uses a completely decoupled interface to allow injection of arbitrary,
// game-specific pricing structures without hard-dependency on non-core packages.
type TradeTester[T any] struct {
	prices      map[string]int
	priceModels T // Holds arbitrary game-specific price maps
	middlewares []engine.Middleware
}

// NewTradeTester creates a new TradeTester.
func NewTradeTester[T any]() *TradeTester[T] {
	return &TradeTester[T]{
		prices:      make(map[string]int),
		middlewares: make([]engine.Middleware, 0),
	}
}

// WithPrices sets the base flat pricing data for legacy middlewares.
func (t *TradeTester[T]) WithPrices(prices map[string]int) *TradeTester[T] {
	t.prices = prices
	return t
}

// WithPriceModels sets the game-specific price models for the tester.
func (t *TradeTester[T]) WithPriceModels(models T) *TradeTester[T] {
	t.priceModels = models
	return t
}

// AddMiddleware registers a middleware to be executed during the trade process.
func (t *TradeTester[T]) AddMiddleware(mw engine.Middleware) *TradeTester[T] {
	t.middlewares = append(t.middlewares, mw)
	return t
}

// Run executes the trade offer through the middleware chain under a mocked pricing feed.
func (t *TradeTester[T]) Run(ctx context.Context, offer *trading.TradeOffer) (*engine.Verdict, error) {
	eng := engine.New()

	eng.Use(func(next engine.Handler) engine.Handler {
		return func(c *engine.TradeContext) error {
			for sku, price := range t.prices {
				c.Set("price_"+sku, price)
			}
			c.Set("prices", t.priceModels)
			return next(c)
		}
	})

	eng.Use(t.middlewares...)

	return eng.Process(ctx, offer)
}
