// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package tf2 implements the primary module for interacting with Team Fortress 2.
It acts as the central hub for all TF2-specific logic, from managing the Game
Coordinator (GC) connection to maintaining a real-time inventory snapshot.

# Architectural Role

This module is designed to be the single source of truth for all things TF2
within the G-man framework. It provides the core primitives (Items, SKUs,
GC actions) used by the high-level trading engine.

The package is supported by several specialized sub-packages:
  - 'backpack': High-level management of the bot's inventory (via SOCache) and external auditing.
  - 'bptf': Integration with backpack.tf for listings and reputation.
  - 'pricedb': Real-time consolidated price authority.
  - 'schema': High-fidelity item schema manager and SKU generator.
  - 'trading': The business logic and middleware engine for automated trades.

# Core Components

 1. The GC Client (TF2 struct):
    The state machine responsible for managing the connection to the TF2
    Game Coordinator. It handles the 'ClientHello' handshake, manages
    connection lifecycle, and provides high-level actions like 'Craft()'
    or 'UseItem()'.

 2. The SOCache (SOCache struct):
    The "live" in-memory inventory manager. It subscribes to GC Shared Object
    (SO) updates to maintain an up-to-the-millisecond representation of
    the bot's backpack. It is the primary source of inventory data for
    all internal logic.

# Event-Driven Integration

The module communicates its state to the rest of the application via the global
Event Bus. Key events include:

  - 'ConnectedEvent': Fired when the GC handshake is complete.
  - 'BackpackLoadedEvent': Fired after the SOCache has finished its initial sync.
  - 'ItemAcquiredEvent'/'ItemRemovedEvent': Fired in real-time as the inventory changes.

By subscribing to 'BackpackLoadedEvent', other modules can safely begin operations
that depend on knowing the current inventory state, such as price seeding or
listing synchronization.
*/
package tf2
