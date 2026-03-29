// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package trading manages asynchronous trade offers via the Steam WebAPI.

This is one of the most critical modules for any trading bot. It periodically
polls the `IEconService/GetTradeOffers` endpoint to discover new and updated
trade offers, manages their state, and provides methods to accept, decline,
or cancel them.

# Architectural Role:

The `trading` module acts as the primary "sensor" for trade-related activity.
It publishes events (`NewOfferEvent`, `OfferChangedEvent`) that trigger the
bot's business logic, which is typically handled by a higher-level `Processor`.

# Key Features:

  - Periodically polls for sent and received trade offers.
  - Tracks the state of each offer to detect changes (e.g., from Active to Accepted).
  - Provides `AcceptOffer`, `DeclineOffer`, and `CancelOffer` methods.
  - Includes a `SetOfferHandler` hook to integrate with a custom business logic
    processor (like the Trade Middleware Engine).
  - Fetches detailed offer contents and escrow duration information.
*/
package trading
