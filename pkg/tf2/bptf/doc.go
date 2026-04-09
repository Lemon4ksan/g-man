// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package bptf provides a comprehensive, high-performance Go client for the
backpack.tf API, specifically designed for Team Fortress 2 trading automation.

The package bridges the gap between raw HTTP responses and the G-man framework's
internal logic, integrating with the Trade Middleware Engine (TME) and the SKU system.

# Authentication

Backpack.tf uses two different authentication methods depending on the endpoint:
  - API Key: Used for legacy WebAPI endpoints (e.g., GetPrices v4).
  - User Token: Used for modern v2 endpoints (e.g., Classifieds, Alerts, Agent Pulse).

The 'Client' handles both by injecting the required headers ('X-Api-Key' and
'X-Auth-Token') into the underlying 'rest.Client'.

# High-Performance Pricing (PriceManager)

The backpack.tf price schema (IGetPrices/v4) is a massive JSON document that
can exceed 20MB. Parsing this on every trade offer is inefficient.

The 'PriceManager' solves this by:
 1. Fetching the full pricelist periodically (e.g., every 30 minutes).
 2. Building a "flat" in-memory reverse index (map[string]PriceEntry).
 3. Providing O(1) lookups by SKU string (e.g., "5021;6" for Keys).

This allows the bot to evaluate hundreds of items in a trade offer in
microseconds without any additional network overhead.

# Core Subsystems

The package is organized around the primary backpack.tf API sections:

 1. Economy:
    Access to the global price schema, price history for specific items,
    and internal currency data (Keys/Ref conversion rates).

 2. Classifieds (Classifieds & Listings):
    Full lifecycle management of trading listings. Create buy/sell orders,
    batch delete listings, and manage listing alerts.

 3. Reputation (Users):
    Query user data including community bans, trust scores, and inventory
    values. This is essential for building safety-first trading logic.

 4. Agent (Pulse):
    Implementation of the "User Agent" heartbeat. Keeping the agent active
    ensures the bot appears "Online" on the site and automatically bumps
    its listings to the top of search results.

# Integration with Trade Engine

The package includes ready-to-use Middlewares for the 'engine' package:
  - SafetyMiddleware: Automatically declines offers from banned or low-trust users.
  - BptfFallbackPricer: Uses the PriceManager to provide prices if the primary
    pricer (e.g., PriceDB) lacks data for a specific SKU.
*/
package bptf
