// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steam

import (
	"context"

	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/community/market"
	"github.com/lemon4ksan/g-man/pkg/steam/guard"
	"github.com/lemon4ksan/g-man/pkg/steam/social/chat"
	"github.com/lemon4ksan/g-man/pkg/steam/social/friends"
	"github.com/lemon4ksan/g-man/pkg/steam/social/status"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/account"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/apps"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/directory"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/gc"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/notifications"
	"github.com/lemon4ksan/g-man/pkg/trading/live"
	"github.com/lemon4ksan/g-man/pkg/trading/web"
)

// Market returns the Steam Community Market module from registered modules.
func Market(c *Client) *market.Market {
	return market.From(c)
}

// Friends returns the Friends module from registered modules.
func Friends(c *Client) *friends.Manager {
	return friends.From(c)
}

// Chat returns the Chat module from registered modules.
func Chat(c *Client) *chat.Chat {
	return chat.From(c)
}

// Status returns the Status module from registered modules.
func Status(c *Client) *status.Manager {
	return status.From(c)
}

// GC returns the Game Coordinator module from registered modules.
func GC(c *Client) *gc.Coordinator {
	return gc.From(c)
}

// Notifications returns the Notifications module from registered modules.
func Notifications(c *Client) *notifications.Notifications {
	return notifications.From(c)
}

// Account returns the Account module from registered modules.
func Account(c *Client) *account.Account {
	return account.From(c)
}

// Apps returns the Apps module from registered modules.
func Apps(c *Client) *apps.Apps {
	return apps.From(c)
}

// Directory returns a Directory client for discovering Connection Manager servers.
func Directory(c *Client) *directory.Service {
	return directory.New(c)
}

// Guard returns the Guardian mobile 2FA/confirmation module from registered modules.
func Guard(c *Client) *guard.Guardian {
	return guard.From(c)
}

// LiveTrading returns the Live Trade invitations module from registered modules.
func LiveTrading(c *Client) *live.Manager {
	return live.From(c)
}

// WebTrading returns the Web Trade Offers manager module from registered modules.
func WebTrading(c *Client) *web.Manager {
	return web.From(c)
}

// AuthFlow returns a configured declarative authentication flow.
func AuthFlow(c *Client, opts ...auth.FlowOption) *auth.Flow {
	allOpts := make([]auth.FlowOption, 0, 1+len(opts))
	allOpts = append(allOpts, auth.WithServerFinder(func(ctx context.Context) (socket.CMServer, error) {
		return directory.New(c).GetOptimalCMServer(ctx)
	}))
	allOpts = append(allOpts, opts...)

	return auth.NewFlow(c, allOpts...)
}
