// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trading

import (
	"context"

	"github.com/lemon4ksan/g-man/pkg/trading"
	"github.com/lemon4ksan/g-man/pkg/trading/engine"
)

// TradeTester test harness for evaluating trade offers against middleware pipelines.
type TradeTester[T any] struct {
	prices      map[string]int
	priceModels T
	middlewares []engine.Middleware
}

func NewTradeTester[T any]() *TradeTester[T] {
	return &TradeTester[T]{
		prices:      make(map[string]int),
		middlewares: make([]engine.Middleware, 0),
	}
}

func (t *TradeTester[T]) WithPrices(prices map[string]int) *TradeTester[T] {
	t.prices = prices
	return t
}

func (t *TradeTester[T]) WithPriceModels(models T) *TradeTester[T] {
	t.priceModels = models
	return t
}

func (t *TradeTester[T]) AddMiddleware(mw engine.Middleware) *TradeTester[T] {
	t.middlewares = append(t.middlewares, mw)
	return t
}

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
