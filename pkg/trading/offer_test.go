// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trading

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
)

func TestTradeOffer_TimeMethods(t *testing.T) {
	t.Parallel()

	offer := &TradeOffer{
		TimeCreated:    1700000000,
		TimeUpdated:    1700000100,
		ExpirationTime: 1700000200,
	}

	assert.Equal(t, time.Unix(1700000000, 0), offer.CreatedAt())
	assert.Equal(t, time.Unix(1700000100, 0), offer.UpdatedAt())
	assert.Equal(t, time.Unix(1700000200, 0), offer.ExpiresAt())
}

func TestTradeOffer_IsActive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		state  OfferState
		active bool
	}{
		{
			name:   "active_state",
			state:  OfferStateActive,
			active: true,
		},
		{
			name:   "accepted_state",
			state:  OfferStateAccepted,
			active: false,
		},
		{
			name:   "declined_state",
			state:  OfferStateDeclined,
			active: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			offer := &TradeOffer{State: tt.state}
			assert.Equal(t, tt.active, offer.IsActive())
		})
	}
}

func TestTradeOffer_IsGlitched(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		offer    *TradeOffer
		glitched bool
	}{
		{
			name: "missing_partner_steamid",
			offer: &TradeOffer{
				OtherSteamID: 0,
				ItemsToGive:  []*Item{{AssetID: 1}},
			},
			glitched: true,
		},
		{
			name: "empty_items_both_sides",
			offer: &TradeOffer{
				OtherSteamID:   id.ID(76561197960265728),
				ItemsToGive:    []*Item{},
				ItemsToReceive: []*Item{},
			},
			glitched: true,
		},
		{
			name: "valid_offer",
			offer: &TradeOffer{
				OtherSteamID: id.ID(76561197960265728),
				ItemsToGive:  []*Item{{AssetID: 100}},
			},
			glitched: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.glitched, tt.offer.IsGlitched())
		})
	}
}
