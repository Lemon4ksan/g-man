// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package trading provides mock implementations for testing TradeOffer instances.
package trading

import (
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/trading"
)

// OfferBuilder provides a fluent builder for constructing mock TradeOffer instances in unit tests.
type OfferBuilder struct {
	offer *trading.TradeOffer
}

func NewOfferBuilder() *OfferBuilder {
	return &OfferBuilder{
		offer: &trading.TradeOffer{
			ItemsToGive:    make([]*trading.Item, 0),
			ItemsToReceive: make([]*trading.Item, 0),
		},
	}
}

func (b *OfferBuilder) WithPartner(partnerID id.ID) *OfferBuilder {
	b.offer.OtherSteamID = partnerID
	return b
}

func (b *OfferBuilder) AddGiveItem(sku string, amount int) *OfferBuilder {
	for range amount {
		b.offer.ItemsToGive = append(b.offer.ItemsToGive, &trading.Item{SKU: sku})
	}

	return b
}

func (b *OfferBuilder) AddGiveItemFull(item *trading.Item) *OfferBuilder {
	b.offer.ItemsToGive = append(b.offer.ItemsToGive, item)
	return b
}

func (b *OfferBuilder) AddReceiveItem(sku string, amount int) *OfferBuilder {
	for range amount {
		b.offer.ItemsToReceive = append(b.offer.ItemsToReceive, &trading.Item{SKU: sku})
	}

	return b
}

func (b *OfferBuilder) AddReceiveItemFull(item *trading.Item) *OfferBuilder {
	b.offer.ItemsToReceive = append(b.offer.ItemsToReceive, item)
	return b
}

func (b *OfferBuilder) Build() *trading.TradeOffer {
	return b.offer
}
