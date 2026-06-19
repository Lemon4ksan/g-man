// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package service provides a high-level RPC-like commander for interacting with Steam's official interfaces.
//
// It abstracts the three primary ways to communicate with Steam: WebAPI, Unified Services,
// and legacy EMsg-based socket requests.
//
// # Key Components
//
//   - [Client]: The primary entry point that decorates a [tr.Transport] with session credentials and validations.
//   - [Doer]: An interface representing objects capable of executing transport-agnostic requests.
//   - [UnifiedTarget]: Represents a modern Protobuf-based Steam Service method call.
//   - [WebAPITarget]: Represents a classic JSON/VDF WebAPI call.
//   - [LegacyTarget]: Represents a raw EMsg-based message used in socket connections.
//   - [HTTPTarget]: A basic implementation of the transport Target interface for HTTP-based calls.
//   - [SteamAPIError]: A structured error container that captures raw API response failures.
//   - [EResultError]: An error wrapper around Steam's internal EResult enum codes.
//
// # Basic Usage Example
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"github.com/lemon4ksan/g-man/pkg/steam/service"
//		tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
//	)
//
//	type ResolveVanityURLResponse struct {
//		SteamID string `json:"steamid" url:"steamid"`
//		Success int    `json:"success" url:"success"`
//	}
//
//	func main() {
//		ctx := context.Background()
//
//		// Initialize HTTP transport with a base URL
//		transport := tr.NewHTTPTransport(nil, service.WebAPIBase)
//
//		// Create a Service Client wrapping the transport
//		client := service.New(transport).WithAPIKey("WEB_API_KEY")
//
//		// Prepare query parameters
//		reqMsg := struct {
//			VanityURL string `url:"vanityurl"`
//		}{VanityURL: "lemon4ksan"}
//
//		// Call the classic WebAPI method
//		resp, err := service.WebAPI[ResolveVanityURLResponse](
//			ctx, client, "GET", "ISteamUser", "ResolveVanityURL", 1, reqMsg,
//		)
//		if err != nil {
//			fmt.Println("API call failed:", err)
//			return
//		}
//
//		fmt.Println("Resolved SteamID:", resp.SteamID)
//	}
package service
