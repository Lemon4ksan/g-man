// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package backpack provides a dual-purpose system for managing Team Fortress 2
inventories. It handles both the bot's own real-time backpack state and
the auditing of external profiles.

# 1. The Backpack Module (Bot's Inventory)

The 'Backpack' struct is a high-level steam.Module designed for the bot's
own inventory management. It acts as a lightweight view over the 'tf2.SOCache',
providing:
  - SKU-based filtering and stock counting.
  - Item locking for active trades to prevent redundant usage.
  - Automated layout application to keep the backpack sorted.
  - Pure metal stock management for the trading engine.

It remains perfectly synchronized with the Game Coordinator and is the
primary interface for local inventory business logic.

# 2. External Auditing (Remote Inventories)

The 'Remote' struct is designed for "scouting" and "auditing" external
profiles, such as potential trade partners.

Key Use Cases:
  - Anti-Scam Validation: Verifying if a partner's high-value items (Unusuals,
    Australiums) are "clean" or "duplicated" by checking history.
  - Public Scouting: Inspecting public profiles to assess wealth or holdings.
  - Historical Auditing: Accessing an item's 'original_id' (permanent fingerprint).

# Pluggable Duplicate Detection:

The package features a 'DupeChecker' interface, allowing the inventory to
delegate item history verification to external services. A standard
implementation for 'backpack.tf' (via history auditing) is supported.

# Lazy Loading & Thread Safety:

Remote inventory data is loaded lazily upon the first request (e.g., calling
'IsDuped'). Internal synchronization ensures that concurrent requests for
the same inventory do not trigger redundant WebAPI calls.

# Limitations:

  - Privacy: Cannot view inventories of users with private profiles.
  - Caching: Steam's WebAPI is subject to caching (up to 15 minutes).
  - Rate Limits: Excessive use may lead to temporary IP bans. Use judiciously.
*/
package backpack
