// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package gc provides a multiplexing gateway for communicating with Steam's Game Coordinators (GC).
// It acts as a smart pipe, wrapping raw game messages in the required Steam CM envelope,
// routing incoming responses, and coordinating request-response job cycles.
//
// The primary orchestrator is the [Coordinator] module, which can be registered on a [steam.Client] using [WithModule] or retrieved via [From].
// Received messages are dispatched to custom registered handlers via [Coordinator.RegisterGCHandler],
// matched to pending callbacks, or published as [MessageEvent] payloads onto the internal client event bus.
//
// Basic usage example:
//
//	package main
//
//	import (
//		"fmt"
//
//		"github.com/lemon4ksan/g-man/pkg/steam"
//		"github.com/lemon4ksan/g-man/pkg/steam/protocol"
//		"github.com/lemon4ksan/g-man/pkg/steam/sys/gc"
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
//		// Initialize the module
//		g := gc.New()
//		client.RegisterModule(g)
//
//		// Run client systems
//		if err := client.Run(); err != nil {
//			fmt.Println("Failed to run client:", err)
//			return
//		}
//
//		// Retrieve the coordinator instance
//		g := gc.From(client)
//
//		// Register a GC message handler for Team Fortress 2 (AppID: 440)
//		// and custom message type (e.g. 1001)
//		g.RegisterGCHandler(440, 1001, func(packet *protocol.GCPacket) {
//			fmt.Printf("Received GC packet of type %d, length: %d\n", packet.MsgType, len(packet.Payload))
//		})
//	}
package gc
