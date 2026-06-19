// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package community provides a high-level client for the steamcommunity.com website.
//
// Unlike the structured service package which uses official WebAPIs, this
// package interacts with the web side of Steam. This includes parsing HTML pages,
// performing AJAX calls, and handling legacy form-encoded data.
//
// # Key Components
//
//   - [Client]: The primary client used to communicate with Steam Community pages.
//   - [Requester]: An interface representing objects capable of executing HTTP requests with session support.
//
// # Basic Usage Example
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"github.com/lemon4ksan/g-man/pkg/steam/community"
//	)
//
//	func main() {
//		ctx := context.Background()
//
//		// Mock sessionID extractor returning a dummy token
//		sessionFunc := func(url string) string {
//			return "mock_session_id_12345"
//		}
//
//		// Create a new Community Client wrapping the default HTTP transport
//		client := community.NewClient(nil, sessionFunc)
//
//		// Perform a GET request to fetch raw HTML content
//		htmlBytes, err := community.GetHTML(ctx, client, "dev")
//		if err != nil {
//			fmt.Println("Request failed:", err)
//			return
//		}
//
//		fmt.Printf("Successfully retrieved %d bytes of HTML\n", len(htmlBytes))
//	}
package community
