// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package bptf provides a comprehensive, high-performance Go client for the
backpack.tf API, specifically designed for Team Fortress 2 trading automation.

The package bridges the gap between raw HTTP responses and the G-man framework's
internal logic, focusing on listing management, reputation auditing, and
real-time market presence.

# Authentication

Backpack.tf uses two different authentication methods depending on the endpoint:
  - API Key: Used for legacy WebAPI endpoints (e.g., GetUsers).
  - User Token: Used for modern v2 endpoints (e.g., Classifieds, Alerts, Agent Pulse).

The 'Client' handles both by injecting the required headers ('X-Api-Key' and
'X-Auth-Token') into the underlying 'rest.Client'.

# Core Subsystems

The package is organized around the primary backpack.tf API sections:

 1. Classifieds (Classifieds & Listings):
    Full lifecycle management of trading listings. Create buy/sell orders,
    batch delete listings, and manage listing alerts. This is the primary
    mechanism for the bot to broadcast its intent to the market.

 2. Reputation (Users):
    Query user data including community bans, trust scores, and inventory
    values. This is essential for building safety-first trading logic.

 3. Agent (Pulse):
    Implementation of the "User Agent" heartbeat. Keeping the agent active
    ensures the bot appears "Online" on the site and automatically bumps
    its listings to the top of search results.

# Integration with Trade Engine

The package includes ready-to-use Middlewares for the 'engine' package:
  - SafetyMiddleware: Automatically declines offers from banned or low-trust users.
  - DupeCheckMiddleware: Checks high-value items against backpack.tf history to detect duplicates.

Note: While backpack.tf provides pricing data, the G-man framework uses the
'pricedb' package as its primary, consolidated price authority to reduce
external API dependency and ensure consistency.
*/
package bptf
