// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package friends manages the user's friends list, persona states, and group interactions.
//
// This module listens for real-time updates from the Steam Connection Manager (CM)
// to maintain an in-memory cache of the user's social graph.
//
// # Key Components
//
//   - [Manager]: The central module manager that coordinates friendship operations, profile comments, and status caching.
//   - [PersonaState]: Holds cached real-time profile details for a specific user (such as nickname, avatar, or rich presence).
//   - [RelationshipChangedEvent]: Emitted when a friendship status changes (such as friend requests added or accepted).
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
//		"github.com/lemon4ksan/g-man/pkg/steam/social/friends"
//	)
//
//	func main() {
//		ctx := context.Background()
//		logger := log.New(log.DefaultConfig(log.LevelInfo))
//
//		// Build the standard configuration
//		cfg := steam.DefaultConfig()
//
//		// Create a new Steam client
//		client, err := steam.NewClient(cfg, steam.WithLogger(logger))
//		if err != nil {
//			fmt.Println("Failed to create client:", err)
//			return
//		}
//		defer client.Close()
//
//		// Register friends module explicitly (if not done by client)
//		f := friends.New()
//		client.RegisterModule(f)
//
//		// Run client systems
//		if err := client.Run(); err != nil {
//			fmt.Println("Failed to run client:", err)
//			return
//		}
//
//		// Print active friends list
//		for _, friendID := range f.GetFriends() {
//			state := f.GetFriend(friendID)
//			if state != nil {
//				fmt.Printf("Friend %s (ID: %d)\n", state.PlayerName, friendID)
//			} else {
//				fmt.Printf("Friend ID: %d (Loading...)\n", friendID)
//			}
//		}
//	}
package friends
