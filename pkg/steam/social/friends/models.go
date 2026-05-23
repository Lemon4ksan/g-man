// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package friends

import (
	"time"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
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
	Old     enums.EFriendRelationship
	New     enums.EFriendRelationship
}

// PersonaStateUpdatedEvent is called when a friend changes their nickname, avatar, or status (enters the game).
type PersonaStateUpdatedEvent struct {
	bus.BaseEvent
	SteamID id.ID
	State   *PersonaState
}

// Comment represents a profile comment.
type Comment struct {
	ID            string    `json:"id"`
	AuthorSteamID id.ID     `json:"author_steam_id"`
	AuthorName    string    `json:"author_name"`
	AuthorAvatar  string    `json:"author_avatar"`
	Date          time.Time `json:"date"`
	Text          string    `json:"text"`
}
