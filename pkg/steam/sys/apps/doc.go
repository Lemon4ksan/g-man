// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package apps manages the user's "In-Game" presence on Steam.
//
// This module allows the bot to appear as if it is playing one or more games,
// including non-Steam shortcuts with custom names.
//
// # Key Components
//
//   - [Apps]: The central module manager that coordinates playing status updates and player count queries.
//   - [AppLaunchedEvent]: Emitted when the client starts playing a new game.
//   - [AppQuitEvent]: Emitted when the client stops playing a game.
//   - [PlayingStateEvent]: Emitted when Steam notifies about a change in playing session state.
//
// # Basic Usage Example
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"github.com/lemon4ksan/g-man/pkg/log"
//		"github.com/lemon4ksan/g-man/pkg/steam"
//		"github.com/lemon4ksan/g-man/pkg/steam/sys/apps"
//	)
//
//	func main() {
//		ctx := context.Background()
//		logger := log.New(log.DefaultConfig(log.LevelInfo))
//
//		// Build standard client config
//		clientCfg := steam.DefaultConfig()
//		client, err := steam.NewClient(clientCfg, steam.WithLogger(logger))
//		if err != nil {
//			fmt.Println("Failed to create client:", err)
//			return
//		}
//		defer client.Close()
//
//		// Initialize the apps module
//		a := apps.New()
//		client.RegisterModule(a)
//
//		// Run client systems
//		if err := client.Run(); err != nil {
//			fmt.Println("Failed to run client:", err)
//			return
//		}
//
//		// Set playing status to Team Fortress 2 (AppID: 440)
//		err = a.PlayGames(ctx, []uint32{440}, false)
//		if err != nil {
//			fmt.Println("Failed to set playing status:", err)
//			return
//		}
//
//		fmt.Println("Successfully set status to In-Game!")
//	}
package apps
