// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package friends

import (
	"time"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/protocol"
)

// PersonaState represents cached information about the user.
type PersonaState struct {
	PlayerName      string
	AvatarHash      []byte
	LastLogoff      time.Time
	LastLogon       time.Time
	LastSeenOnline  time.Time
	AvatarURLIcon   string
	AvatarURLMedium string
	AvatarURLFull   string
	RichPresence    map[string]string
}

// GetBadgesResponse describes the response from the IPlayerService/GetBadges WebAPI.
type GetBadgesResponse struct {
	PlayerLevel int `json:"player_level"`
}

// RelationshipChangedEvent is fired when someone adds us as a friend,
// removes us, or we accept the request.
type RelationshipChangedEvent struct {
	bus.BaseEvent
	SteamID id.ID
	Old     protocol.EFriendRelationship
	New     protocol.EFriendRelationship
}

func (e *RelationshipChangedEvent) Topic() string { return "friends.relationship_changed" }

// PersonaStateUpdatedEvent is called when a friend changes their nickname, avatar, or status (enters the game).
type PersonaStateUpdatedEvent struct {
	bus.BaseEvent
	SteamID id.ID
	State   *PersonaState
}

func (e *PersonaStateUpdatedEvent) Topic() string { return "friends.persona_updated" }
