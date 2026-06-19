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

// PersonaState represents cached information about the user.
type PersonaState struct {
	// PlayerName is the user's current Steam nickname.
	PlayerName string
	// AvatarHash is the unique hash identifier of the user's avatar image.
	AvatarHash []byte
	// LastLogoff is the timestamp when the user was last seen logging off.
	LastLogoff time.Time
	// LastLogon is the timestamp when the user was last seen logging on.
	LastLogon time.Time
	// LastSeenOnline is the timestamp when the user was last seen online.
	LastSeenOnline time.Time
	// AvatarURLIcon is the direct URL to the small icon avatar image.
	AvatarURLIcon string
	// AvatarURLMedium is the direct URL to the medium-sized avatar image.
	AvatarURLMedium string
	// AvatarURLFull is the direct URL to the full-sized avatar image.
	AvatarURLFull string
	// RichPresence maps custom rich presence keys to their current string values.
	RichPresence map[string]string
}

// GetBadgesResponse describes the response from the IPlayerService/GetBadges WebAPI.
type GetBadgesResponse struct {
	// PlayerLevel is the user's current Steam profile level.
	PlayerLevel int `json:"player_level"`
}

// RelationshipChangedEvent is fired when someone adds us as a friend,
// removes us, or we accept the request.
type RelationshipChangedEvent struct {
	bus.BaseEvent
	// SteamID is the unique 64-bit identifier of the target user.
	SteamID id.ID
	// Old is the previous relationship status.
	Old enums.EFriendRelationship
	// New is the updated relationship status.
	New enums.EFriendRelationship
}

// PersonaStateUpdatedEvent is called when a friend changes their nickname, avatar, or status (enters the game).
type PersonaStateUpdatedEvent struct {
	bus.BaseEvent
	// SteamID is the unique 64-bit identifier of the updated user.
	SteamID id.ID
	// State is the updated cached persona details.
	State *PersonaState
}

// Comment represents a profile comment.
type Comment struct {
	// ID is the unique string identifier of the comment.
	ID string `json:"id"`
	// AuthorSteamID is the 64-bit identifier of the comment's author.
	AuthorSteamID id.ID `json:"author_steam_id"`
	// AuthorName is the display name of the comment's author.
	AuthorName string `json:"author_name"`
	// AuthorAvatar is the direct URL to the author's avatar image.
	AuthorAvatar string `json:"author_avatar"`
	// Date is the timestamp when the comment was posted.
	Date time.Time `json:"date"`
	// Text is the plain-text content of the comment.
	Text string `json:"text"`
}

// FriendGroup represents a group of friends.
type FriendGroup struct {
	GroupID int32
	Name    string
	Members []id.ID
}

// GroupListEvent is emitted when our entire friends group list is loaded.
type GroupListEvent struct {
	bus.BaseEvent
	Groups map[int32]FriendGroup
}

// NicknameListEvent is emitted when our entire friends nickname list is loaded.
type NicknameListEvent struct {
	bus.BaseEvent
	Nicknames map[id.ID]string
}

// NicknameChangedEvent is emitted when a friend's nickname is changed.
type NicknameChangedEvent struct {
	bus.BaseEvent
	SteamID  id.ID
	Nickname string // empty if removed
}
