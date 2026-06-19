// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package profile implements profile, privacy settings, and avatar management for Steam Community accounts.
//
// It abstracts the process of scraping edit configurations and updating profile details.
//
// # Key Components
//
//   - [Settings]: Holds customizable profile details (such as nickname, real name, summary, and location).
//   - [PrivacySettings]: Holds customizable profile privacy details (such as inventory or game details visibility).
//
// # Basic Usage Example
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"github.com/lemon4ksan/g-man/pkg/steam/community"
//		"github.com/lemon4ksan/g-man/pkg/steam/community/profile"
//		"github.com/lemon4ksan/g-man/pkg/steam/id"
//	)
//
//	func main() {
//		ctx := context.Background()
//
//		// Mock sessionID extractor
//		sessionFunc := func(url string) string {
//			return "mock_session_id_12345"
//		}
//
//		// Create a new Community Client
//		client := community.NewClient(nil, sessionFunc)
//		steamID := id.Parse("76561197960265728")
//
//		// Prepare changes (nil values are ignored, keeping existing Steam values)
//		newName := "NewNickname"
//		settings := profile.Settings{
//			Name: &newName,
//		}
//
//		// Apply changes
//		err := profile.EditProfile(ctx, client, steamID, settings)
//		if err != nil {
//			fmt.Println("Failed to update profile:", err)
//			return
//		}
//
//		fmt.Println("Profile updated successfully")
//	}
package profile
