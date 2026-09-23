// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trading

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
)

func TestChallenger_GlitchedOffer_EdgeCases(t *testing.T) {
	t.Parallel()

	partnerID := id.ID(76561197960265728)

	t.Run("zero_items_with_custom_message_is_glitched", func(t *testing.T) {
		t.Parallel()

		offer := &TradeOffer{
			OtherSteamID:   partnerID,
			Message:        "Here is a free trade for you!",
			ItemsToGive:    []*Item{},
			ItemsToReceive: []*Item{},
		}
		assert.True(t, offer.IsGlitched(), "0 items with non-empty message must be flagged as glitched")
	})

	t.Run("zero_items_both_nil_slices_is_glitched", func(t *testing.T) {
		t.Parallel()

		offer := &TradeOffer{
			OtherSteamID:   partnerID,
			Message:        "Another empty trade",
			ItemsToGive:    nil,
			ItemsToReceive: nil,
		}
		assert.True(t, offer.IsGlitched(), "Nil items slices must be flagged as glitched")
	})

	t.Run("missing_partner_steamid_is_glitched", func(t *testing.T) {
		t.Parallel()

		offer := &TradeOffer{
			OtherSteamID: 0,
			ItemsToGive:  []*Item{{AssetID: 1, Name: "Key", MarketHashName: "Key"}},
		}
		assert.True(t, offer.IsGlitched(), "Partner SteamID 0 must be flagged as glitched")
	})

	t.Run("nil_item_pointer_in_give_is_glitched", func(t *testing.T) {
		t.Parallel()

		offer := &TradeOffer{
			OtherSteamID: partnerID,
			ItemsToGive:  []*Item{nil},
		}
		assert.True(t, offer.IsGlitched(), "Nil item pointer in ItemsToGive must be flagged as glitched")
	})

	t.Run("nil_item_pointer_in_receive_is_glitched", func(t *testing.T) {
		t.Parallel()

		offer := &TradeOffer{
			OtherSteamID:   partnerID,
			ItemsToReceive: []*Item{nil},
		}
		assert.True(t, offer.IsGlitched(), "Nil item pointer in ItemsToReceive must be flagged as glitched")
	})

	t.Run("both_name_and_markethashname_empty_is_glitched", func(t *testing.T) {
		t.Parallel()

		offer := &TradeOffer{
			OtherSteamID: partnerID,
			ItemsToReceive: []*Item{
				{AssetID: 101, Name: "", MarketHashName: ""},
			},
		}
		assert.True(t, offer.IsGlitched(), "Item with both empty Name and MarketHashName must be glitched")
	})

	t.Run("one_way_gift_receive_only_is_valid", func(t *testing.T) {
		t.Parallel()

		offer := &TradeOffer{
			OtherSteamID: partnerID,
			ItemsToGive:  []*Item{},
			ItemsToReceive: []*Item{
				{AssetID: 201, Name: "Mann Co. Supply Crate Key", MarketHashName: "Mann Co. Supply Crate Key"},
			},
		}
		assert.False(t, offer.IsGlitched(), "One-way incoming gift offer is not glitched")
	})

	t.Run("one_way_donation_give_only_is_valid", func(t *testing.T) {
		t.Parallel()

		offer := &TradeOffer{
			OtherSteamID: partnerID,
			ItemsToGive: []*Item{
				{AssetID: 202, Name: "Refined Metal", MarketHashName: "Refined Metal"},
			},
			ItemsToReceive: []*Item{},
		}
		assert.False(t, offer.IsGlitched(), "One-way outgoing donation offer is not glitched")
	})

	// Parity challenge against node-steam-tradeoffer-manager:
	// In Node TradeOffer.js:78:
	//   this.itemsToGive.concat(this.itemsToReceive).some(item => !item.name || !item.market_hash_name)
	// Steam item descriptions ALWAYS include both name and market_hash_name when loaded.
	// If either is missing, Steam failed to load the description.
	t.Run("adversarial_item_empty_name_nonempty_markethashname", func(t *testing.T) {
		t.Parallel()

		offer := &TradeOffer{
			OtherSteamID: partnerID,
			ItemsToReceive: []*Item{
				{AssetID: 301, Name: "", MarketHashName: "Mann Co. Supply Crate Key"},
			},
		}
		assert.True(
			t,
			offer.IsGlitched(),
			"Empty Name must be flagged as glitched matching @tf2autobot/tradeoffer-manager",
		)
	})

	t.Run("adversarial_item_nonempty_name_empty_markethashname", func(t *testing.T) {
		t.Parallel()

		offer := &TradeOffer{
			OtherSteamID: partnerID,
			ItemsToGive: []*Item{
				{AssetID: 302, Name: "Mann Co. Supply Crate Key", MarketHashName: ""},
			},
		}
		assert.True(
			t,
			offer.IsGlitched(),
			"Empty MarketHashName must be flagged as glitched matching @tf2autobot/tradeoffer-manager",
		)
	})
}
