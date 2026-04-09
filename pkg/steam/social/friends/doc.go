// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package friends manages the user's friends list, persona states, and group interactions.

This module listens for real-time updates from the Steam Connection Manager (CM)
to maintain an in-memory cache of the user's social graph. It tracks relationship
changes (e.g., friend added/removed) and persona updates (e.g., name change,
online/offline status).

# Key Features:

  - Maintain a real-time cache of friend relationships and persona states.
  - Send, accept, and remove friend invitations.
  - Invite friends to Steam groups.
  - Publish detailed events (`RelationshipChangedEvent`, `PersonaStateUpdatedEvent`)
    to the global event bus.
  - Calculate the maximum friend limit based on Steam level.
*/
package friends
