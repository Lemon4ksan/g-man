// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steam_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/community/market"
	"github.com/lemon4ksan/g-man/pkg/steam/guard"
	"github.com/lemon4ksan/g-man/pkg/steam/social/chat"
	"github.com/lemon4ksan/g-man/pkg/steam/social/friends"
	"github.com/lemon4ksan/g-man/pkg/steam/social/status"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/account"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/apps"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/gc"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/notifications"
	"github.com/lemon4ksan/g-man/pkg/trading/live"
	"github.com/lemon4ksan/g-man/pkg/trading/web"
)

func TestAccessors_AllServicesInstantiated(t *testing.T) {
	c, err := steam.NewClient(
		steam.Config{DisableSocket: true},
		market.WithModule(market.Config{}),
		guard.WithModule(guard.Config{
			SharedSecret:   "AAAAAAAAAAAAAAAAAAAAAAAAAAA=",
			IdentitySecret: "AAAAAAAAAAAAAAAAAAAAAAAAAAA=",
			DeviceID:       "android:01234567-89ab-cdef-0123-456789abcdef",
		}),
		friends.WithModule(),
		chat.WithModule(),
		status.WithModule(),
		gc.WithModule(),
		notifications.WithModule(),
		account.WithModule(),
		apps.WithModule(),
		live.WithModule(),
		web.WithModule(web.Config{}),
	)
	require.NoError(t, err)
	defer c.Close()

	// 1. Community & trading services
	require.NotNil(t, steam.Market(c))
	require.NotNil(t, steam.Guard(c))
	require.NotNil(t, steam.LiveTrading(c))
	require.NotNil(t, steam.WebTrading(c))

	// 2. Social services
	require.NotNil(t, steam.Friends(c))
	require.NotNil(t, steam.Chat(c))
	require.NotNil(t, steam.Status(c))

	// 3. Sys services
	require.NotNil(t, steam.GC(c))
	require.NotNil(t, steam.Notifications(c))
	require.NotNil(t, steam.Account(c))
	require.NotNil(t, steam.Apps(c))
	require.NotNil(t, steam.Directory(c))

	// 4. Auth flow
	flow := steam.AuthFlow(c, auth.WithCredentials("demo_user", "demo_pass"))
	require.NotNil(t, flow)
}
