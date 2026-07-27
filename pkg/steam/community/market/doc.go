// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package market implements interactions with the Steam Community Market, buy/sell order creation,
// price overview queries, and Steam Gem (Goo) crafting mechanics.
//
// # 1. Steam Community Market Fee Calculation
//
// Steam market transactions incur two separate fees paid by the buyer:
//  1. Steam Fee (Valve Fee): 5% (minimum $0.01 / 1 cent).
//  2. Publisher Fee: Game-specific percentage (usually 10% for Valve games like TF2/CS2, minimum $0.01 / 1 cent).
//
// Fee Formula (in integer minimum currency units / Cents):
//
//	SteamFee     = Max(1, Floor(SellerReceivesCents * 0.05))
//	PublisherFee = Max(1, Floor(SellerReceivesCents * 0.10))
//	TotalFee     = SteamFee + PublisherFee
//	BuyerPays    = SellerReceivesCents + TotalFee
//
// # 2. Steam Gem (Goo) Mechanics
//
// Items eligible for grinding (such as unwanted trading cards, emoticons, or profile backgrounds)
// can be converted into Steam Gems (internal name "Goo"):
//   - Item Grinding: TurnItemIntoGems sends an ajaxgrindintogoo request and returns yield counts.
//   - Gem Packing: 1000 raw Gems can be packed into 1 "Sack of Gems" asset (AssetID conversion via GemExchange).
//   - Booster Pack Crafting: Gems can be spent to craft game booster packs via CreateBoosterPack.
package market
