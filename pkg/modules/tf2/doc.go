// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package tf2 implements the primary module for interacting with Team Fortress 2.
It acts as the central hub for all TF2-specific logic, from managing the Game
Coordinator (GC) connection to maintaining a real-time inventory snapshot.

# Architectural Role

This module is designed to be the single source of truth for all things TF2
within the G-man framework. It is composed of two main sub-systems that work
in tandem:

 1. The GC Client (TF2 struct):
    This is the state machine responsible for managing the connection to the TF2
    Game Coordinator. Its duties include:
    - Sending a `ClientHello` message to initiate the GC session after the
    main Steam Client has logged on and launched the "game" (AppID 440).
    - Periodically re-sending `ClientHello` until a `ClientWelcome` is received.
    - Handling `ServerGoodbye` messages and managing the auto-reconnect logic.
    - Providing high-level, game-specific actions such as `Craft()`,
    `UseItem()`, and `InviteToTrade()`.

 2. The SOCache (SOCache struct):
    This is the "live" in-memory inventory manager. It subscribes to all incoming
    GC messages and listens for Shared Object (SO) updates to maintain an
    up-to-the-millisecond representation of the bot's backpack. It handles:
    - Parsing the initial `k_ESOMsg_CacheSubscribed` message to perform a full
    inventory sync.
    - Processing incremental updates (`Create`, `Update`, `Destroy`) to reflect
    changes from trades, crafting, or item drops.
    - Responding to `k_ESOMsg_CacheSubscriptionCheck` to ensure data consistency
    and trigger a full refresh if desynchronization is detected.

# Event-Driven Integration

The module communicates its state to the rest of the application via the global
Event Bus. Key events include:

  - `GCConnectedEvent`: Fired when the GC handshake is complete.
  - `BackpackLoadedEvent`: Fired after the SOCache has finished its initial sync.
  - `ItemAcquiredEvent`/`ItemRemovedEvent`: Fired in real-time as the inventory changes.

By subscribing to `BackpackLoadedEvent`, other modules or business logic can
safely begin operations that depend on knowing the current inventory state,
such as pricing or automated trading.
*/
package tf2
