// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package trading provides a specialized implementation of the generic trading engine (pkg/trading) for Team Fortress 2.
It defines the TF2-specific business logic, price valuation strategies, and safety protocols for automated trading.

# Decision Algorithm

The bot uses a strict, deterministic middleware pipeline to process incoming trade offers:

 1. Inventory & Stock Limits (StockManager):
    Ensures that accepting the trade won't exceed global inventory capacity or specific SKU limits.
    Integrates with 'backpack.tf' listings to automatically balance stock by creating or
    adjusting buy/sell orders in real-time.

 2. Price Authority (PriceDB):
    Enriches the context with market data. PriceDB is the exclusive source of truth.
    If an item is missing from the pricelist, the trade is flagged for manual review to prevent
    accidental loss of high-value items.

 3. Safety & Reputation (BPTF/Rep):
    Checks the partner's reputation across multiple platforms (SteamRep, backpack.tf, Rep.tf).
    Verified scammers or banned users are automatically declined.

 4. Duplication Check (History):
    For high-value items (Unusuals, Australiums, etc.), the bot queries item history via
    the 'backpack' package to identify duplicates (duped items).

 5. Value Calculation & Smart Counter-offers:
    The bot calculates the total value of items given vs. items received using Scrap metal units.
    - Underpaid: Automatically attempts to counter-offer by retrieving missing currency (keys/ref/reclaimed/scrap)
    from the partner's inventory to fulfill the price requirement.
    - Overpaid: Automatically adds the correct change from the bot's own inventory.

# Price Aggregation & Real-Time Reactivity

G-man relies on the PriceDB microservice, which aggregates data from backpack.tf,
marketplace.tf, and other markets. This allows the bot to:
  - Eliminate redundant traffic to external APIs.
  - Rely on a single, validated, and normalized price feed.
  - React instantly to market volatility via Socket.IO notifications, triggering
    re-evaluation of active listings and pending trades.

# Safety Philosophy

The bot follows the "Strict Price Authority" principle: a trade is only accepted automatically
if every item is accurately priced in the internal database. When in doubt, the bot defaults
to "Review" rather than taking a risky trade based on stale or global fallback prices.
*/
package trading
