// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package friends

import (
	"time"

	"github.com/lemon4ksan/miyako/bus"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

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

type GetBadgesResponse struct {
	PlayerLevel int `json:"player_level"`
}

type RelationshipChangedEvent struct {
	bus.BaseEvent
	SteamID id.ID
	Old     enums.EFriendRelationship
	New     enums.EFriendRelationship
}

type PersonaStateUpdatedEvent struct {
	bus.BaseEvent
	SteamID id.ID
	State   *PersonaState
}

type Comment struct {
	ID            string    `json:"id"`
	AuthorSteamID id.ID     `json:"author_steam_id"`
	AuthorName    string    `json:"author_name"`
	AuthorAvatar  string    `json:"author_avatar"`
	Date          time.Time `json:"date"`
	Text          string    `json:"text"`
}

type FriendGroup struct {
	GroupID int32
	Name    string
	Members []id.ID
}

type GroupListEvent struct {
	bus.BaseEvent
	Groups map[int32]FriendGroup
}

type NicknameListEvent struct {
	bus.BaseEvent
	Nicknames map[id.ID]string
}

type NicknameChangedEvent struct {
	bus.BaseEvent
	SteamID  id.ID
	Nickname string
}
