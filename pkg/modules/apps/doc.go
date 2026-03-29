// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package apps manages the user's "In-Game" presence on Steam.

This module allows the bot to appear as if it's playing one or more games,
including non-Steam shortcuts with custom names. This is a critical component
for interacting with Game Coordinators (like for TF2, CS2, or Dota 2), as
the GC will not send game-specific data unless the user is "in-game".

# Key Features:

  - Set playing status for multiple Steam and non-Steam games.
  - Query the current number of players for any AppID.
  - Handle playing session conflicts by kicking other active sessions.
  - Publish events (`AppLaunchedEvent`, `AppQuitEvent`, `PlayingStateEvent`)
    to the global event bus.

# Integration:

The `apps` module is a dependency for game-specific modules (e.g., `tf2`),
which call `PlayGames()` to initiate the GC handshake sequence.
*/
package apps
