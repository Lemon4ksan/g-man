// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package pricedb provides a high-performance, strictly typed HTTP client for the
PriceDB.io API. It serves as the primary price oracle for Team Fortress 2
trading bots within the G-man framework.

The package is designed to work seamlessly with the 'sku' package for item
identification and the 'rest' package for robust HTTP communication.

# Key Features:

  - Efficient Bulk Fetching: Retrieve the latest prices for hundreds of items
    in a single HTTP request using the /api/items-bulk endpoint.
  - Historical Analysis: Access full price history for any item to calculate
    trends or verify price stability.
  - Fuzzy Search: Search for items by human-readable names and retrieve their
    corresponding SKUs and current market values.
  - SKU Service: Integration with the specialized SKU service for resolving
    item names to internal properties and metadata.
  - Middleware Integration: Ideally suited for use within a Trade Middleware
    Engine (TME) to enrich trade contexts with financial data.

# Usage Example:

	// Initialize the client
	client := pricedb.NewClient(nil)

	// Fetch a single item price
	price, err := client.GetItem(ctx, "5021;6")
	if err == nil {
	    fmt.Printf("Key Buy: %.2f ref, Sell: %.2f ref\n", price.Buy.Metal, price.Sell.Metal)
	}

	// Fetch multiple prices at once (Bulk)
	skus := []string{"5021;6", "5002;6", "5000;6"}
	prices, err := client.GetItemsBulk(ctx, skus)

# Architecture:

The 'Client' struct maintains two internal 'rest.Client' instances: one for the
main pricing API and another for the SKU metadata service.

All methods are thread-safe and accept 'context.Context' for proper timeout
handling and cancellation.
*/
package pricedb
