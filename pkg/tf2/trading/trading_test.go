// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trading_test

import (
	"context"
	"reflect"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/tf2"
	"github.com/lemon4ksan/g-man/pkg/tf2/backpack"
	tf2schema "github.com/lemon4ksan/g-man/pkg/tf2/schema"
	tf2trading "github.com/lemon4ksan/g-man/pkg/tf2/trading"
	"github.com/lemon4ksan/g-man/pkg/trading"
	"github.com/lemon4ksan/g-man/pkg/trading/engine"
	"github.com/lemon4ksan/g-man/pkg/trading/reason"
	tradingtest "github.com/lemon4ksan/g-man/test/trading"
)

// mockEscrowChecker implements trading.EscrowChecker
type mockEscrowChecker struct {
	hasEscrow bool
}

func (m *mockEscrowChecker) CheckEscrow(ctx context.Context, offer *trading.TradeOffer) (bool, error) {
	return m.hasEscrow, nil
}

// mockBackpackCache is a mock for backpack.ItemCache interface.
type mockBackpackCache struct {
	items []*tf2.Item
}

func (m *mockBackpackCache) GetItems() []*tf2.Item { return m.items }
func (m *mockBackpackCache) GetItem(id uint64) (*tf2.Item, bool) {
	for _, it := range m.items {
		if it.ID == id {
			return it, true
		}
	}

	return nil, false
}
func (m *mockBackpackCache) GetMaxSlots() int { return 3000 }

// mockSchemaProvider returns a nil schema to gracefully mock Backpack SKU fetching.
type mockSchemaProvider struct{}

func (m *mockSchemaProvider) Get() *tf2schema.Schema { return nil }

// helper to inject private fields into backpack.Backpack
func setUnexportedField(target any, fieldName string, value any) {
	val := reflect.ValueOf(target).Elem()
	field := val.FieldByName(fieldName)
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(value))
}

func TestComplexTrades(t *testing.T) {
	logger := log.Discard

	// Value checking mock middleware to simulate SmartCounter or Value checker.
	// It accepts if total received value >= total given value.
	valueChecker := func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			giveValue := 0
			recvValue := 0

			for _, it := range ctx.Offer.ItemsToGive {
				val, ok := ctx.Get("price_" + it.SKU)
				if ok {
					giveValue += val.(int)
				}
			}

			for _, it := range ctx.Offer.ItemsToReceive {
				val, ok := ctx.Get("price_" + it.SKU)
				if ok {
					recvValue += val.(int)
				}
			}

			if recvValue >= giveValue {
				ctx.Accept(reason.TradeReason("good_value"))
				return nil
			}

			ctx.Decline(reason.TradeReason("low_value"))

			return nil
		}
	}

	t.Run("Clean Trade - Accepted", func(t *testing.T) {
		bp := backpack.New()
		cache := &mockBackpackCache{items: []*tf2.Item{}}
		setUnexportedField(bp, "cache", cache)
		setUnexportedField(bp, "manager", &mockSchemaProvider{})

		tester := tf2trading.NewTF2TradeTester().
			WithPrices(map[string]int{
				"Key": 60,
				"Ref": 1,
			}).
			AddMiddleware(tf2trading.EscrowMiddleware(&mockEscrowChecker{hasEscrow: false}, logger)).
			AddMiddleware(tf2trading.StockLimitMiddleware(bp, tf2trading.StockConfig{MaxTotal: 100, DefaultMax: 100}, logger)).
			AddMiddleware(valueChecker)

		offer := tradingtest.NewOfferBuilder().
			AddGiveItem("Key", 1).
			AddReceiveItem("Ref", 60).
			Build()

		verdict, err := tester.Run(context.Background(), offer)
		assert.NoError(t, err)
		assert.Equal(t, trading.ActionAccept, verdict.Action)
	})

	t.Run("Escrow Trade - Declined", func(t *testing.T) {
		bp := backpack.New()
		cache := &mockBackpackCache{items: []*tf2.Item{}}
		setUnexportedField(bp, "cache", cache)
		setUnexportedField(bp, "manager", &mockSchemaProvider{})

		tester := tf2trading.NewTF2TradeTester().
			WithPrices(map[string]int{
				"Key": 60,
				"Ref": 1,
			}).
			AddMiddleware(tf2trading.EscrowMiddleware(&mockEscrowChecker{hasEscrow: true}, logger)).
			AddMiddleware(tf2trading.StockLimitMiddleware(bp, tf2trading.StockConfig{MaxTotal: 100, DefaultMax: 100}, logger)).
			AddMiddleware(valueChecker)

		offer := tradingtest.NewOfferBuilder().
			AddGiveItem("Key", 1).
			AddReceiveItem("Ref", 60).
			Build()

		verdict, err := tester.Run(context.Background(), offer)
		assert.NoError(t, err)
		assert.Equal(t, trading.ActionDecline, verdict.Action)
		assert.Equal(t, reason.DeclineEscrow, verdict.Reason)
	})

	t.Run("Overstock Trade - Declined by Global Limit", func(t *testing.T) {
		bp := backpack.New()
		cache := &mockBackpackCache{items: []*tf2.Item{}}
		setUnexportedField(bp, "cache", cache)
		setUnexportedField(bp, "manager", &mockSchemaProvider{})

		tester := tf2trading.NewTF2TradeTester().
			WithPrices(map[string]int{
				"Key": 60,
				"Ref": 1,
			}).
			AddMiddleware(tf2trading.EscrowMiddleware(&mockEscrowChecker{hasEscrow: false}, logger)).
			// Max total is 5, but we receive 6 items.
			AddMiddleware(tf2trading.StockLimitMiddleware(bp, tf2trading.StockConfig{MaxTotal: 5, DefaultMax: 10}, logger)).
			AddMiddleware(valueChecker)

		offer := tradingtest.NewOfferBuilder().
			AddReceiveItem("Ref", 6).
			Build()

		verdict, err := tester.Run(context.Background(), offer)
		assert.NoError(t, err)
		assert.Equal(t, trading.ActionDecline, verdict.Action)
		assert.Equal(t, reason.ReviewOverstocked, verdict.Reason)
	})

	t.Run("Underpaid Trade - Declined by Value Checker", func(t *testing.T) {
		bp := backpack.New()
		cache := &mockBackpackCache{items: []*tf2.Item{}}
		setUnexportedField(bp, "cache", cache)
		setUnexportedField(bp, "manager", &mockSchemaProvider{})

		tester := tf2trading.NewTF2TradeTester().
			WithPrices(map[string]int{
				"Key": 60,
				"Ref": 1,
			}).
			AddMiddleware(tf2trading.EscrowMiddleware(&mockEscrowChecker{hasEscrow: false}, logger)).
			AddMiddleware(tf2trading.StockLimitMiddleware(bp, tf2trading.StockConfig{MaxTotal: 100, DefaultMax: 100}, logger)).
			AddMiddleware(valueChecker)

		// Give 1 Key (60), receive 50 Ref (50). Loss of 10.
		offer := tradingtest.NewOfferBuilder().
			AddGiveItem("Key", 1).
			AddReceiveItem("Ref", 50).
			Build()

		verdict, err := tester.Run(context.Background(), offer)
		assert.NoError(t, err)
		assert.Equal(t, trading.ActionDecline, verdict.Action)
		assert.Equal(t, reason.TradeReason("low_value"), verdict.Reason)
	})
}
