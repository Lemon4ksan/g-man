// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package schema provides a powerful, multi-source manager for the Team
Fortress 2 item schema.

It acts as the "encyclopedia" for all TF2 item logic, translating raw numeric
data (like defindexes and quality IDs) into human-readable names and
standardized SKU strings.

# Data Sources:

The manager aggregates data from multiple authoritative sources to ensure
maximum completeness:
  - Steam WebAPI (IEconItems_440): Provides core item definitions, qualities,
    and particle effects.
  - GitHub (SteamDatabase/GameTracking): Provides the 'items_game.txt' and
    localized paint kit names, which are essential for modern TF2 items.

# Architectural Role:

The SchemaManager is a foundational module. Because other modules (like 'tf2'
and 'trading') depend on the schema to correctly identify items, the
SchemaManager performs a blocking initial fetch during its 'Start' phase.
Once loaded, it publishes a 'SchemaReadyEvent' and transitions to a
background refresh loop.

# High-Performance Indexing:

To support high-frequency trading and inventory processing, the package builds
multiple O(1) lookup maps upon every schema update. It allows near-instant
resolution of:
  - Item by Defindex or Name.
  - Unusual Effects and Paint Kits by ID.
  - Crate Series and Paint Decimal values.

# Memory Optimization (LiteMode):

Steam's 'items_game' file is massive (often dozens of megabytes). When
'LiteMode' is enabled in the configuration, the manager prunes non-essential
VDF fields (like color definitions and quest conditions) to significantly
reduce the RAM footprint—crucial for large bot farms.

# Key Features:

  - Parallel Fetching: Uses 'errgroup' to download all schema components
    simultaneously, reducing startup time.
  - VDF Parsing: Native support for Valve Data Format (KeyValues) parsing.
  - Events: Broadcaster for schema lifecycle changes (Ready, Updated, Failed).
  - Normalization: Provides logic to fix Steam's inconsistent quality/defindex
    combinations (e.g., Strange Unusuals or Promo versions).
*/
package schema
