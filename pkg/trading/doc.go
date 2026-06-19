// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package trading provides a generic, game-agnostic trade reasoning engine for Steam.
//
// # Key Components
//
//   - [TradeOffer]: Represents a snapshot of a trade offer, including items, states, and escrow information.
//   - [Item]: Represents a specific Steam inventory item defined by its asset and class identifiers.
//   - [OfferParams]: Configures parameters for creating and sending a new trade offer.
//   - [CounterParams]: Holds parameters required to send a counter-offer.
//
// # Basic Usage Example
//
//	package main
//
//	import (
//		"github.com/lemon4ksan/g-man/pkg/steam/id"
//		"github.com/lemon4ksan/g-man/pkg/trading"
//	)
//
//	func main() {
//		partnerID := id.FromAccountID(12345678)
//
//		// Create parameters for a new trade offer
//		_ = trading.OfferParams{
//			PartnerID: partnerID,
//			Token:     "trade_token_abc",
//			Message:   "Here is your traded item!",
//			ItemsToGive: []*trading.Item{
//				{
//					AppID:     440,
//					ContextID: 2,
//					AssetID:   123456789,
//					Amount:    1,
//				},
//			},
//			ItemsToReceive: []*trading.Item{},
//		}
//	}
package trading
