// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/trading"
	"github.com/lemon4ksan/g-man/pkg/trading/reason"
)

func TestEngine_Process_Basic(t *testing.T) {
	t.Parallel()

	e := New()

	var order []string

	mw1 := func(next Handler) Handler {
		return func(ctx *TradeContext) error {
			order = append(order, "mw1_before")
			err := next(ctx)

			order = append(order, "mw1_after")

			return err
		}
	}

	mw2 := func(next Handler) Handler {
		return func(ctx *TradeContext) error {
			order = append(order, "mw2_before")
			err := next(ctx)

			order = append(order, "mw2_after")

			return err
		}
	}

	e.Use(mw1, mw2)

	offer := &trading.TradeOffer{ID: 100}
	verdict, err := e.Process(t.Context(), offer)

	require.NoError(t, err)
	assert.Equal(t, trading.ActionSkip, verdict.Action) // Default is ActionSkip
	assert.Equal(t, []string{"mw1_before", "mw2_before", "mw2_after", "mw1_after"}, order)
}

func TestEngine_Process_EarlyTermination(t *testing.T) {
	t.Parallel()

	e := New()

	mw1 := func(next Handler) Handler {
		return func(ctx *TradeContext) error {
			ctx.Decline(reason.DeclineBlacklisted)
			return nil // early exit, do not call next(ctx)
		}
	}

	mw2 := func(next Handler) Handler {
		return func(ctx *TradeContext) error {
			t.Fatal("mw2 should not be reached")
			return next(ctx)
		}
	}

	e.Use(mw1, mw2)

	offer := &trading.TradeOffer{ID: 100}
	verdict, err := e.Process(t.Context(), offer)

	require.NoError(t, err)
	assert.Equal(t, trading.ActionDecline, verdict.Action)
	assert.Equal(t, reason.DeclineBlacklisted, verdict.Reason)
}

func TestTradeContext_VerdictMutations(t *testing.T) {
	t.Parallel()

	ctx := NewTradeContext(t.Context(), &trading.TradeOffer{ID: 100})

	// Test default
	assert.Equal(t, trading.ActionSkip, ctx.Verdict.Action)

	// Test Accept
	ctx.Accept(reason.AcceptDonation)
	assert.Equal(t, trading.ActionAccept, ctx.Verdict.Action)
	assert.Equal(t, reason.AcceptDonation, ctx.Verdict.Reason)

	// Test Decline
	ctx.Decline(reason.DeclineBegging)
	assert.Equal(t, trading.ActionDecline, ctx.Verdict.Action)
	assert.Equal(t, reason.DeclineBegging, ctx.Verdict.Reason)

	// Test Review
	ctx.Review(reason.ReviewOverstocked)
	assert.Equal(t, trading.ActionReview, ctx.Verdict.Action)
	assert.Equal(t, reason.ReviewOverstocked, ctx.Verdict.Reason)

	// Test Counter
	params := &trading.CounterParams{}
	ctx.Counter(reason.ReviewInvalidItems, params)
	assert.Equal(t, trading.ActionCounter, ctx.Verdict.Action)
	assert.Equal(t, reason.ReviewInvalidItems, ctx.Verdict.Reason)
	assert.Equal(t, params, ctx.Verdict.Data)

	t.Run("metadata_operations", func(t *testing.T) {
		t.Parallel()
		ctx.Set("test-key", "test-val")
		val, ok := ctx.Get("test-key")
		assert.True(t, ok)
		assert.Equal(t, "test-val", val)

		_, ok = ctx.Get("non-existent")
		assert.False(t, ok)
	})

	t.Run("empty_metadata_key", func(t *testing.T) {
		t.Parallel()
		ctx.Set("", "value")
		_, ok := ctx.Get("")
		assert.False(t, ok)
	})
}

func TestTradeContext_Decision(t *testing.T) {
	t.Parallel()

	t.Run("accept", func(t *testing.T) {
		t.Parallel()

		v := Verdict{Action: trading.ActionAccept, Reason: reason.AcceptDonation}
		d := v.Decision()
		assert.Equal(t, trading.ActionAccept, d.Action)
		assert.Equal(t, reason.AcceptDonation.String(), d.Reason)
	})

	t.Run("decline", func(t *testing.T) {
		t.Parallel()

		v := Verdict{Action: trading.ActionDecline, Reason: reason.DeclineBegging}
		d := v.Decision()
		assert.Equal(t, trading.ActionDecline, d.Action)
		assert.Equal(t, reason.DeclineBegging.String(), d.Reason)
	})

	t.Run("counter", func(t *testing.T) {
		t.Parallel()

		params := &trading.CounterParams{}
		v := Verdict{Action: trading.ActionCounter, Reason: reason.ReviewInvalidItems, Data: params}
		d := v.Decision()
		assert.Equal(t, trading.ActionCounter, d.Action)
		assert.Equal(t, params, d.CounterParams)
		assert.Equal(t, reason.ReviewInvalidItems.String(), d.Reason)
	})

	t.Run("counter_invalid_data_type", func(t *testing.T) {
		t.Parallel()

		v := Verdict{Action: trading.ActionCounter, Reason: reason.ReviewInvalidItems, Data: "invalid_type"}
		d := v.Decision()
		assert.Equal(t, trading.ActionCounter, d.Action)
		assert.Nil(t, d.CounterParams)
	})

	t.Run("skip_mapping", func(t *testing.T) {
		t.Parallel()
		// Review/Ignore/Undecided should map to Skip
		for _, action := range []trading.ActionType{trading.ActionReview, trading.ActionIgnore, ""} {
			v := Verdict{Action: action, Reason: reason.ReviewOverstocked}
			d := v.Decision()
			assert.Equal(t, trading.ActionSkip, d.Action)
		}
	})
}

func TestRecoverMiddleware(t *testing.T) {
	t.Parallel()

	logger := log.New(log.DefaultConfig(log.LevelError))
	e := New()
	e.Use(RecoverMiddleware(logger))

	mwPanic := func(next Handler) Handler {
		return func(ctx *TradeContext) error {
			panic("something went terribly wrong")
		}
	}
	e.Use(mwPanic)

	offer := &trading.TradeOffer{ID: 100}
	verdict, err := e.Process(t.Context(), offer)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "panic in trade engine: something went terribly wrong")
	assert.Equal(t, trading.ActionReview, verdict.Action)
	assert.Equal(t, reason.ReviewEngineError, verdict.Reason)
}

func TestLoggerMiddleware(t *testing.T) {
	t.Parallel()

	logger := log.New(log.DefaultConfig(log.LevelError))
	e := New()
	e.Use(LoggerMiddleware(logger))

	offer := &trading.TradeOffer{ID: 100}
	verdict, err := e.Process(t.Context(), offer)

	require.NoError(t, err)
	assert.Equal(t, trading.ActionSkip, verdict.Action)
}

func TestBlacklistMiddleware(t *testing.T) {
	t.Parallel()

	blacklist := map[id.ID]struct{}{
		id.New(12345): {},
	}

	e := New()
	e.Use(BlacklistMiddleware(blacklist))

	t.Run("blacklisted", func(t *testing.T) {
		t.Parallel()

		offer := &trading.TradeOffer{ID: 100, OtherSteamID: id.New(12345)}
		verdict, err := e.Process(t.Context(), offer)
		require.NoError(t, err)
		assert.Equal(t, trading.ActionDecline, verdict.Action)
		assert.Equal(t, reason.DeclineBlacklisted, verdict.Reason)
	})

	t.Run("allowed", func(t *testing.T) {
		t.Parallel()

		offer := &trading.TradeOffer{ID: 100, OtherSteamID: id.New(99999)}
		verdict, err := e.Process(t.Context(), offer)
		require.NoError(t, err)
		assert.Equal(t, trading.ActionSkip, verdict.Action) // Allowed passes through
	})
}

func TestEmptyOfferMiddleware(t *testing.T) {
	t.Parallel()

	t.Run("begging", func(t *testing.T) {
		t.Parallel()

		e := New()
		e.Use(EmptyOfferMiddleware(nil))

		offer := &trading.TradeOffer{
			ItemsToGive: []*trading.Item{
				{SKU: "5021;6", Tradable: true},
			},
			ItemsToReceive: nil,
		}

		verdict, err := e.Process(t.Context(), offer)
		require.NoError(t, err)
		assert.Equal(t, trading.ActionDecline, verdict.Action)
		assert.Equal(t, reason.DeclineBegging, verdict.Reason)
	})

	t.Run("donation_untradable_item", func(t *testing.T) {
		t.Parallel()

		e := New()
		e.Use(EmptyOfferMiddleware(nil))

		offer := &trading.TradeOffer{
			ItemsToGive: nil,
			ItemsToReceive: []*trading.Item{
				{SKU: "5021;6", Tradable: false}, // Untradable junk!
			},
		}

		verdict, err := e.Process(t.Context(), offer)
		require.NoError(t, err)
		assert.Equal(t, trading.ActionDecline, verdict.Action)
		assert.Equal(t, reason.DeclineBegging, verdict.Reason)
	})

	t.Run("donation_pure_junk_default", func(t *testing.T) {
		t.Parallel()

		e := New()
		e.Use(EmptyOfferMiddleware(nil)) // uses default (if it has SKU, it's not junk)

		offer := &trading.TradeOffer{
			ItemsToGive: nil,
			ItemsToReceive: []*trading.Item{
				{SKU: "", Tradable: true},
			},
		}

		verdict, err := e.Process(t.Context(), offer)
		require.NoError(t, err)
		assert.Equal(t, trading.ActionDecline, verdict.Action)
		assert.Equal(t, reason.DeclineJunkDonation, verdict.Reason)
	})

	t.Run("donation_pure_junk_custom_function", func(t *testing.T) {
		t.Parallel()

		isJunk := func(item *trading.Item) bool {
			return item.SKU == "junk_sku"
		}

		e := New()
		e.Use(EmptyOfferMiddleware(isJunk))

		offer := &trading.TradeOffer{
			ItemsToGive: nil,
			ItemsToReceive: []*trading.Item{
				{SKU: "junk_sku", Tradable: true},
			},
		}

		verdict, err := e.Process(t.Context(), offer)
		require.NoError(t, err)
		assert.Equal(t, trading.ActionDecline, verdict.Action)
		assert.Equal(t, reason.DeclineJunkDonation, verdict.Reason)
	})

	t.Run("donation_not_junk_custom_function", func(t *testing.T) {
		t.Parallel()

		isJunk := func(item *trading.Item) bool {
			return item.SKU == "junk_sku"
		}

		e := New()
		e.Use(EmptyOfferMiddleware(isJunk))

		offer := &trading.TradeOffer{
			ItemsToGive: nil,
			ItemsToReceive: []*trading.Item{
				{SKU: "valuable_sku", Tradable: true}, // not junk
			},
		}

		verdict, err := e.Process(t.Context(), offer)
		require.NoError(t, err)
		assert.Equal(t, trading.ActionAccept, verdict.Action)
		assert.Equal(t, reason.AcceptDonation, verdict.Reason)
	})

	t.Run("donation_valid_donation", func(t *testing.T) {
		t.Parallel()

		e := New()
		e.Use(EmptyOfferMiddleware(nil))

		offer := &trading.TradeOffer{
			ItemsToGive: nil,
			ItemsToReceive: []*trading.Item{
				{SKU: "5021;6", Tradable: true},
			},
		}

		verdict, err := e.Process(t.Context(), offer)
		require.NoError(t, err)
		assert.Equal(t, trading.ActionAccept, verdict.Action)
		assert.Equal(t, reason.AcceptDonation, verdict.Reason)
	})

	t.Run("two_way_offer_passes_through", func(t *testing.T) {
		t.Parallel()

		e := New()
		e.Use(EmptyOfferMiddleware(nil))

		offer := &trading.TradeOffer{
			ItemsToGive: []*trading.Item{
				{SKU: "5002;6", Tradable: true},
			},
			ItemsToReceive: []*trading.Item{
				{SKU: "5021;6", Tradable: true},
			},
		}

		verdict, err := e.Process(t.Context(), offer)
		require.NoError(t, err)
		assert.Equal(t, trading.ActionSkip, verdict.Action) // Two-way passes through
	})
}

func TestBotHandler(t *testing.T) {
	t.Parallel()

	t.Run("process_offer_success", func(t *testing.T) {
		t.Parallel()

		e := New()
		e.Use(func(next Handler) Handler {
			return func(ctx *TradeContext) error {
				ctx.Accept(reason.AcceptDonation)
				return nil
			}
		})

		h := NewBotHandler(e, log.Discard)
		offer := &trading.TradeOffer{ID: 100}
		decision, err := h.ProcessOffer(t.Context(), offer)
		require.NoError(t, err)
		assert.Equal(t, trading.ActionAccept, decision.Action)
	})

	t.Run("process_offer_error", func(t *testing.T) {
		t.Parallel()

		e := New()
		e.Use(func(next Handler) Handler {
			return func(ctx *TradeContext) error {
				return errors.New("engine processing failed")
			}
		})

		h := NewBotHandler(e, log.Discard)
		offer := &trading.TradeOffer{ID: 100}
		decision, err := h.ProcessOffer(t.Context(), offer)
		assert.Error(t, err)
		assert.Equal(t, trading.ActionSkip, decision.Action)
	})

	t.Run("on_action_failed", func(t *testing.T) {
		t.Parallel()

		h := NewBotHandler(New(), log.Discard)
		offer := &trading.TradeOffer{ID: 100}
		assert.NotPanics(t, func() {
			h.OnActionFailed(t.Context(), offer, trading.ActionAccept, "reason", errors.New("network fail"))
		})
	})
}
