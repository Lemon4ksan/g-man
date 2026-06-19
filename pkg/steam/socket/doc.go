// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package socket provides a high-level, decoupled engine for managing persistent
// connections to Steam's Connection Manager (CM) servers.
//
// It acts as a Facade, orchestrating the connector, processor, dispatcher, and session
// subsystems to provide a seamless Request-Response and Event-driven API.
//
// # Key Components
//
//   - [Socket]: The central facade wrapping all socket-related operations.
//   - [Config]: Aggregates configuration parameters for all underlying subsystems.
//   - [Session]: The thread-safe state container representing the active connection session.
//
// # Basic Usage Example
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"time"
//		"github.com/lemon4ksan/g-man/pkg/log"
//		"github.com/lemon4ksan/g-man/pkg/steam/socket"
//	)
//
//	func main() {
//		ctx := context.Background()
//		logger := log.New(log.DefaultConfig(log.LevelInfo))
//
//		// Initialize a new Socket facade
//		s := socket.NewSocket(socket.DefaultConfig(), logger)
//		defer s.Close()
//
//		// Define a target Connection Manager server
//		server := socket.CMServer{
//			Endpoint: "cm1-ams1.steamcontent.com:27017",
//			Type:     "tcp",
//		}
//
//		// Connect to the CM server
//		err := s.Connect(ctx, server)
//		if err != nil {
//			fmt.Println("Connection failed:", err)
//			return
//		}
//
//		// Start the periodic heartbeat loop
//		err = s.StartHeartbeat(10 * time.Second)
//		if err != nil {
//			fmt.Println("Failed to start heartbeat:", err)
//			return
//		}
//
//		fmt.Println("Successfully connected and heartbeating!")
//	}
package socket
