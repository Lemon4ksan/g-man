// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package service provides transport-agnostic execution engines for Steam WebAPI, Unified Services, and Legacy protocols.
// It decouples API call logic from underlying socket or HTTP transport systems. It automatically
// injects auth parameters like API keys or access tokens, and validates standard Steam [enums.EResult] errors.
//
// The core contract is the [Doer] interface. The primary implementation is the [Client] decorator,
// which wraps a [tr.Transport] instance. It provides high-level generic helpers like [Unified],
// [WebAPI], and [Legacy] to execute pre-configured requests.
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
