// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package market provides an interface for interacting with the Steam Community Market.

This module handles the creation and cancellation of buy and sell orders. It
leverages the `community` client to perform authenticated AJAX requests,
automating the process of listing items for sale or placing orders to purchase
items at a specific price.

# Key Features:

  - Create buy orders (`CreateBuyOrder`) for automated item purchasing.
  - Create sell orders (`CreateSellOrder`) to list inventory items for sale.
  - Cancel existing buy or sell orders.
  - Handles currency-specific price formatting required by the Steam API.
  - Automatically injects required HTTP headers (Referer, SessionID) for market actions.
*/
package market
