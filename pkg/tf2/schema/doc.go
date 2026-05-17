// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package schema provides a high-fidelity, performance-optimized manager for the Team Fortress 2 item schema.

It serves as the definitive logic layer for TF2 item metadata, designed for 100% parity with the industry-standard
Node.js 'tf2-schema' (used by TF2Autobot) while providing significant improvements in performance and type safety.

# Architectural Role:

The Schema acts as an "encyclopedia", translating raw numeric data from Steam APIs into structured models.
It is a foundational dependency for the 'trading' and 'tf2' packages, providing the O(1) indices required
for high-load inventory and trade processing.

# Parity & Industry Standards:

The package is a production-grade port of the Node.js schema logic, ensuring that item identification,
SKU generation, and name building are identical across the trading ecosystem. This includes:
  - Complex Name Parsing: High-fidelity implementation of 'GetItemObjectFromName' with all legacy edge cases
    (e.g., specific skip conditions for effects like Stardust vs Starduster, Showstopper taunt logic, etc.).
  - Special Mappings: Identical handling of unusual effects (e.g., Eerie Orbiting Fire) and skin-to-defindex
    mapping for Decorated weapons.

# Normalization & Trade Standards:

Steam's schema is notoriously inconsistent. This package provides a centralized normalization layer:
  - GlobalNormalizationMap: Maps legacy/retired defindexes (like event-specific keys) to their canonical IDs.
  - NormalizeItem: A robust method to "fix" item objects, correcting quality combinations
    (e.g., Strange Unusuals, Elevated Qualities, and Promo versions) to meet trading bot standards.
  - SKU Integration: Deep integration with the 'sku' package for bidirectional conversion.

# Data-Driven Identification:

For internal bot logic (Game Coordinator events), the package emphasizes direct, data-driven identification.
The SOCache provides high-performance SKU generation by mapping internal item attributes and
defindexes directly to SKU objects, bypassing the fragility of string-based name parsing.

# WebAPI Integration (Fallback Processing):

While direct data is preferred, the package maintains a high-fidelity integration layer for
the Steam WebAPI (Community Inventories). The 'GetSKUFromEconItem' method provides a robust
fallback pipeline for converting generic items into strict TF2 SKUs using:
  - Item tags (Exterior, Quality).
  - MarketHashName heuristic parsing.
  - Description attribute extraction via virtual proxy defindexes (Spells and Strange Parts).

# Memory Management (LiteMode):

Steam's 'items_game.txt' can exceed 30MB. When 'LiteMode' is enabled, the manager aggressively prunes
non-essential VDF fields after index building to minimize the RAM footprint, which is critical
for scaling large-scale bot deployments.
*/
package schema
