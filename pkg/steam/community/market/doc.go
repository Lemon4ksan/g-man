// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package market provides an interface for interacting with the Steam Community Market.
//
// This module handles the creation, retrieval, and cancellation of buy and sell orders.
//
// # Key Components
//
//   - [Market]: The central coordinator that manages buy/sell orders, listings, and gem crafting.
//   - [Config]: Aggregates configuration parameters such as currency codes and languages.
//   - [Asset]: Represents a standardized Steam asset traded on the market.
//   - [ItemOrdersHistogram]: Represents the order book data for a specific marketplace item.
//
// # Basic Usage Example
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"github.com/lemon4ksan/g-man/pkg/log"
//		"github.com/lemon4ksan/g-man/pkg/steam"
//		"github.com/lemon4ksan/g-man/pkg/steam/community/market"
//	)
//
//	func main() {
//		ctx := context.Background()
//		logger := log.New(log.DefaultConfig(log.LevelInfo))
//
//		// Build default client config
//		clientCfg := steam.DefaultConfig()
//		client, err := steam.NewClient(clientCfg, steam.WithLogger(logger))
//		if err != nil {
//			fmt.Println("Failed to create client:", err)
//			return
//		}
//		defer client.Close()
//
//		// Initialize the market module
//		m := market.New(market.DefaultConfig())
//		client.RegisterModule(m)
//
//		// Fetch a price overview for TF2 Mann Co. Supply Crate Key (AppID: 440)
//		overview, err := m.GetPriceOverview(ctx, 440, "Mann Co. Supply Crate Key")
//		if err != nil {
//			fmt.Println("Failed to fetch price overview:", err)
//			return
//		}
//
//		fmt.Println("Lowest Price:", overview.LowestPrice)
//	}
package market
