// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package websession provides a high-level interface for managing Steam web sessions.
//
// It automates the process of obtaining and synchronizing authentication cookies
// ('steamLoginSecure' and 'sessionid') across multiple Steam domains such as
// steamcommunity.com and steampowered.com.
//
// # Key Components
//
// - [WebSession]: The primary manager of cookie jars and session-authenticated HTTP clients.
//
// # Basic Usage Example
//
//	package main
//
//	import (
//		"fmt"
//		"github.com/lemon4ksan/g-man/pkg/log"
//		"github.com/lemon4ksan/g-man/pkg/steam/auth/websession"
//		"github.com/lemon4ksan/g-man/pkg/steam/id"
//	)
//
//	func main() {
//		logger := log.New(log.DefaultConfig(log.LevelInfo))
//		steamID := id.Parse("76561197960265728")
//
//		// Create a new web session
//		ws := websession.New(steamID, logger, nil)
//
//		// Retrieve the initialized sessionid cookie value
//		sessionID := ws.SessionID("https://steamcommunity.com")
//		fmt.Println("Initial SessionID:", sessionID)
//	}
package websession
