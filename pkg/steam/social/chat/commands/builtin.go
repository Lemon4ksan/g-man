// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/social/friends"
)

var ErrFriendsModuleMissing = errors.New("chat_commands: friends module not available")

// RegisterBuiltinCommands registers standard utility commands (!status, !steamid, !profile) on m.
func RegisterBuiltinCommands(m *Manager, friendsMgr *friends.Manager, started time.Time) {
	m.Register("status", func(_ context.Context, _ []string) (string, error) {
		uptime := time.Since(started).Truncate(time.Second)

		return fmt.Sprintf("Bot is online. Uptime: %s", uptime), nil
	},
		WithDescription("Shows bot status and uptime"),
	)

	m.Register("steamid", func(_ context.Context, args []string) (string, error) {
		if friendsMgr == nil {
			return "", ErrFriendsModuleMissing
		}

		query := strings.ToLower(args[0])

		var matches []string

		friendsList := friendsMgr.GetFriends()
		for _, friendID := range friendsList {
			persona, ok := friendsMgr.GetFriend(friendID)
			if ok && strings.Contains(strings.ToLower(persona.PlayerName), query) {
				matches = append(matches, fmt.Sprintf("%s (%s)", persona.PlayerName, friendID.String()))
			}
		}

		if len(matches) == 0 {
			return "No matching users found.", nil
		}

		if len(matches) > 10 {
			return fmt.Sprintf("Found %d matches (showing first 10):\n%s",
				len(matches), strings.Join(matches[:10], "\n")), nil
		}

		return fmt.Sprintf("Found %d match(es):\n%s",
			len(matches), strings.Join(matches, "\n")), nil
	},
		WithDescription("Looks up a user by persona name"),
		WithArgsSchema(Required[string]("name")),
	)

	m.Register("profile", func(_ context.Context, args []any) (string, error) {
		if friendsMgr == nil {
			return "", ErrFriendsModuleMissing
		}

		steamID := args[0].(id.ID)
		persona, ok := friendsMgr.GetFriend(steamID)

		if !ok {
			return fmt.Sprintf("No cached data for %s.", steamID.String()), nil
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Profile: %s\n", persona.PlayerName)
		fmt.Fprintf(&sb, "SteamID: %s\n", steamID.String())
		fmt.Fprintf(&sb, "Steam3: %s\n", steamID.Steam3())

		return sb.String(), nil
	},
		WithDescription("Shows profile info for a SteamID"),
		WithArgsSchema(Required[id.ID]("steam_id")),
	)
}
