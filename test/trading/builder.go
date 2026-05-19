// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package trading provides utilities for creating test TradeOffer objects.
package trading

import (
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/trading"
)

// OfferBuilder is a fluent builder for creating test TradeOffer objects.
type OfferBuilder struct {
	offer *trading.TradeOffer
}

// NewOfferBuilder creates a new OfferBuilder with an empty TradeOffer.
func NewOfferBuilder() *OfferBuilder {
	return &OfferBuilder{
		offer: &trading.TradeOffer{
			ItemsToGive:    make([]*trading.Item, 0),
			ItemsToReceive: make([]*trading.Item, 0),
		},
	}
}

// WithPartner sets the partner ID for the trade offer.
func (b *OfferBuilder) WithPartner(partnerID id.ID) *OfferBuilder {
	b.offer.OtherSteamID = partnerID
	return b
}

// AddGiveItem adds items to the 'ItemsToGive' slice by SKU.
func (b *OfferBuilder) AddGiveItem(sku string, amount int) *OfferBuilder {
	for range amount {
		b.offer.ItemsToGive = append(b.offer.ItemsToGive, &trading.Item{SKU: sku})
	}
	return b
}

// AddGiveItemFull adds a pre-configured item to the 'ItemsToGive' slice.
func (b *OfferBuilder) AddGiveItemFull(item *trading.Item) *OfferBuilder {
	b.offer.ItemsToGive = append(b.offer.ItemsToGive, item)
	return b
}

// AddReceiveItem adds items to the 'ItemsToReceive' slice by SKU.
func (b *OfferBuilder) AddReceiveItem(sku string, amount int) *OfferBuilder {
	for range amount {
		b.offer.ItemsToReceive = append(b.offer.ItemsToReceive, &trading.Item{SKU: sku})
	}
	return b
}

// AddReceiveItemFull adds a pre-configured item to the 'ItemsToReceive' slice.
func (b *OfferBuilder) AddReceiveItemFull(item *trading.Item) *OfferBuilder {
	b.offer.ItemsToReceive = append(b.offer.ItemsToReceive, item)
	return b
}

// Build returns the constructed TradeOffer.
func (b *OfferBuilder) Build() *trading.TradeOffer {
	return b.offer
}
