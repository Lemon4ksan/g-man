// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package coordinator provides a multiplexing gateway for communicating with
Steam's Game Coordinators (GC).

A Game Coordinator is a dedicated backend server for a specific game (like
TF2, CS2, or Dota 2). This module acts as a "smart pipe", wrapping raw game
messages in the required Steam CM envelope (`ClientToGC`) and parsing incoming
GC messages (`ClientFromGC`).

# Architectural Role:

The `coordinator` module itself is game-agnostic. It knows how to route
messages based on `AppID` and manage GC-level job IDs, but it does not
understand the content of the messages. Game-specific modules (like `tf2`)
subscribe to the `GCMessageEvent` published by this module and handle the
domain-specific logic.

# Key Features:

  - Multiplexes messages for multiple AppIDs over a single CM connection.
  - Manages a separate `jobs.Manager` for GC request-response cycles.
  - Abstracts the `ProtoMask` and GC packet serialization.
  - Publishes a generic `GCMessageEvent` for any game-specific module to consume.
*/
package gc
