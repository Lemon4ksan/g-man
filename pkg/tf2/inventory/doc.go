// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package inventory provides a mechanism for fetching, parsing, and auditing
Team Fortress 2 inventories via Steam's public WebAPI.

Unlike the real-time 'tf2.SOCache' (which uses the Game Coordinator socket to
manage the bot's own inventory), this package is designed for "scouting" and
"auditing" external profiles, such as potential trade partners.

# Key Use Cases:

  - Anti-Scam Validation: Verifying if a partner's high-value items (Unusuals,
    Australiums) are "clean" or "duplicated" by checking their history.
  - Public Scouting: Inspecting the inventory of any user with a public profile
    to assess their wealth or specific item holdings.
  - Historical Auditing: Accessing an item's 'original_id' (its permanent
    fingerprint) which is often not available via standard trade offer data.

# Pluggable Duplicate Detection:

The package features a 'DupeChecker' interface, allowing the inventory to
delegate item history verification to multiple external services.
A standard implementation for 'backpack.tf' (via HTML scraping) is provided,
but users can easily plug in other services like Marketplace.tf or Reps.tf.

# Lazy Loading & Thread Safety:

Inventory data is loaded lazily upon the first request (e.g., calling 'IsDuped').
Internal synchronization ensures that multiple concurrent requests for the same
inventory will not trigger redundant WebAPI calls.

# Limitations:

  - Privacy: This package cannot view inventories of users with private profiles.
  - Caching: Steam's WebAPI is subject to aggressive caching (up to 15 minutes).
    For immediate updates of the bot's own inventory, always use 'tf2.SOCache'.
  - Rate Limits: Excessive use may lead to temporary IP bans from Steam. Use
    judiciously during high-frequency trading.
*/
package inventory
