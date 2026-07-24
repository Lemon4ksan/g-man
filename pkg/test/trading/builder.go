// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package trading provides utilities for creating test TradeOffer objects.
package trading

import (
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/trading"
)

// OfferBuilder is a fluent builder for creating mock TradeOffer objects during tests.
type OfferBuilder struct {
	offer *trading.TradeOffer
}

// NewOfferBuilder instantiates a new OfferBuilder with pre-initialized items slices.
func NewOfferBuilder() *OfferBuilder {
	return &OfferBuilder{
		offer: &trading.TradeOffer{
			ItemsToGive:    make([]*trading.Item, 0),
			ItemsToReceive: make([]*trading.Item, 0),
		},
	}
}

// WithPartner configures the other Steam trade partner's ID.
func (b *OfferBuilder) WithPartner(partnerID id.ID) *OfferBuilder {
	b.offer.OtherSteamID = partnerID
	return b
}

// AddGiveItem appends a specific number of identical mock items to the outgoing trade side by SKU.
func (b *OfferBuilder) AddGiveItem(sku string, amount int) *OfferBuilder {
	for range amount {
		b.offer.ItemsToGive = append(b.offer.ItemsToGive, &trading.Item{SKU: sku})
	}
	return b
}

// AddGiveItemFull appends a pre-configured, complete trading.Item structure to the outgoing trade side.
func (b *OfferBuilder) AddGiveItemFull(item *trading.Item) *OfferBuilder {
	b.offer.ItemsToGive = append(b.offer.ItemsToGive, item)
	return b
}

// AddReceiveItem appends a specific number of identical mock items to the incoming trade side by SKU.
func (b *OfferBuilder) AddReceiveItem(sku string, amount int) *OfferBuilder {
	for range amount {
		b.offer.ItemsToReceive = append(b.offer.ItemsToReceive, &trading.Item{SKU: sku})
	}
	return b
}

// AddReceiveItemFull appends a pre-configured, complete trading.Item structure to the incoming trade side.
func (b *OfferBuilder) AddReceiveItemFull(item *trading.Item) *OfferBuilder {
	b.offer.ItemsToReceive = append(b.offer.ItemsToReceive, item)
	return b
}

// Build finalizes and returns the constructed TradeOffer instance.
func (b *OfferBuilder) Build() *trading.TradeOffer {
	return b.offer
}
