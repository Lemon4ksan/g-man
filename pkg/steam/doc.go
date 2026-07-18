// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package steam implements a unified orchestrator for connecting, authenticating,
// and communicating with the Steam network.
//
// The central type is [Client], which manages the socket connection, web session
// lifecycles, and user authentication.
//
// # Request Routing
//
// The client implements [service.Doer], transparently selecting the best transport route:
//   - Socket: Fast, bi-directional messages over TCP/WebSocket (preferred when connected).
//   - WebAPI: Standard HTTP requests when disconnected or performing web-only actions.
//
// # Quick Start
//
// Initialize a client and start its background routines:
//
//	client, err := steam.NewClient(steam.DefaultConfig())
//	if err != nil {
//		return err
//	}
//	defer client.Close()
//
//	if err := client.Run(); err != nil {
//		return err
//	}
//
//	client.Wait()
package steam
