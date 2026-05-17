// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package pricedb provides a high-performance, strictly typed client for the
PriceDB.io API. It serves as the consolidated, primary price authority for
Team Fortress 2 trading bots within the G-man framework.

The package is designed to work seamlessly with the 'sku' package for item
identification and the 'rest' package for robust HTTP communication.

# Key Features:

  - Real-Time Updates: Built-in Socket.IO client ('SocketManager') for receiving
    instant price changes via WebSockets, enabling immediate bot reactions to
    market volatility.
  - Efficient Bulk Fetching: Retrieve the latest prices for hundreds of items
    in a single HTTP request using the /api/items-bulk endpoint.
  - Historical Analysis: Access full price history for any item to calculate
    trends or verify price stability.
  - Fuzzy Search: Search for items by human-readable names and retrieve their
    corresponding SKUs and current market values.
  - SKU Service: Integration with the specialized SKU service for resolving
    item names to internal properties and metadata.
  - Middleware Integration: The definitive source of truth for the Trade
    Middleware Engine (TME). By aggregating data from backpack.tf,
    marketplace.tf, and other markets, it eliminates the need for
    redundant external API calls.

# Real-Time Integration:

The 'SocketManager' allows the bot to maintain a "live" price cache. By
subscribing to the "price" event, the bot can update its internal valuations
without polling the API.

	sm := pricedb.NewSocketManager("ws://ws.pricedb.io/", logger)
	sm.OnPrice(func(p *pricedb.Price) {
	    fmt.Printf("Price changed for %s: %v\n", p.SKU, p.Sell)
	})
	go sm.Run(ctx)

# Architecture:

The package consists of two primary components:
 1. The Client: A standard HTTP client for RESTful interactions (Bulk fetch, history, search).
 2. The SocketManager: A long-lived WebSocket client for event-driven updates.

All methods are thread-safe and designed for high-concurrency environments.
*/
package pricedb
