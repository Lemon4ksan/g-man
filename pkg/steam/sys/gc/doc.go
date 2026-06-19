// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package gc provides a multiplexing gateway for communicating with Steam's Game Coordinators (GC).
//
// It acts as a smart pipe, wrapping raw game messages in the required Steam CM envelope
// and routing incoming responses.
//
// # Key Components
//
//   - [Coordinator]: The central module manager that coordinates sending and calling GC-level requests.
//   - [GCPacket]: Represents a parsed Game Coordinator message.
//   - [MessageEvent]: Emitted when an unhandled Game Coordinator packet is received.
//
// # Basic Usage Example
//
//	package main
//
//	import (
//		"fmt"
//		"github.com/lemon4ksan/g-man/pkg/steam/protocol"
//		"github.com/lemon4ksan/g-man/pkg/steam/sys/gc"
//	)
//
//	func main() {
//		g := gc.New()
//
//		// Register a GC message handler for Team Fortress 2 (AppID: 440)
//		// and custom message type (e.g. 1001)
//		g.RegisterGCHandler(440, 1001, func(packet *protocol.GCPacket) {
//			fmt.Printf("Received GC packet of type %d, length: %d\n", packet.MsgType, len(packet.Payload))
//		})
//
//		fmt.Println("GC Handler registered successfully")
//	}
package gc
