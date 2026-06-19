// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package steam provides the high-level, unified orchestrator (Client) for interacting
// with all aspects of the Steam ecosystem.
//
// # Key Components
//
//   - [Client]: The central coordinator that manages socket connections, session lifecycles, and module registration.
//   - [SocketProvider]: Defines the minimal socket operations required to maintain a connection to Steam CMs.
//   - [Config]: Aggregates configurations for all underlying systems (Socket, Storage, HTTP).
//   - [State]: Represents the current lifecycle stage of the high-level client.
//
// # Architecture
//
// The [Client] acts as an orchestrator, connecting the low-level socket, auth manager,
// web session, and individual domain modules. It implements [service.Doer],
// dynamically routing requests over TCP/WebSockets or HTTP depending on connection state.
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
//		"github.com/lemon4ksan/g-man/pkg/storage/memory"
//	)
//
//	func main() {
//		ctx := context.Background()
//		logger := log.New(log.DefaultConfig(log.LevelInfo))
//
//		// Build the standard configuration
//		cfg := steam.DefaultConfig()
//		cfg.Storage = memory.New()
//
//		// Create a new Steam client
//		client, err := steam.NewClient(cfg, steam.WithLogger(logger))
//		if err != nil {
//			fmt.Println("Failed to create client:", err)
//			return
//		}
//		defer client.Close()
//
//		// Run the client's internal background systems
//		if err := client.Run(); err != nil {
//			fmt.Println("Failed to run client:", err)
//			return
//		}
//	}
package steam
