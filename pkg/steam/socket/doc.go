// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package socket provides a high-level, decoupled engine for managing persistent
connections to Steam's Connection Manager (CM) servers.

It acts as a Facade, orchestrating four specialized subsystems to provide a
seamless Request-Response and Event-driven API.

# Architecture

The socket layer is divided into four distinct pillars:

  - Connector: Manages the raw network lifecycle (TCP/WS), handles exponential
    backoff, and provides a stream of raw decrypted messages.
  - Processor: The concurrency layer. It manages a fixed worker pool that
    parses raw bytes into protocol packets, decoupling network I/O from logic.
  - Dispatcher: The routing brain. It handles JobID tracking (Request-Response),
    Unified Service methods, and unpacks nested Multi-packets.
  - Session: The source of truth for the account's state (SteamID, Tokens).
    It is independent of the network connection, allowing state to persist
    across reconnections.

# Packet Handling Flow

Data flows through the system in a linear, non-cyclic pipeline:

	Steam -> [Connector] -> (chan bytes) -> [Processor (Workers)] -> [Dispatcher] -> Handlers/Jobs

# Concurrency and Reliability

Socket uses a fixed worker pool in the Processor to prevent goroutine leaks.
It implements natural backpressure: if the logic layer is too slow, the
connector stops reading from the network, signaling Steam to slow down.

# Basic Usage

	s := socket.NewSocket(cfg)

	// Connect with automatic background reconnection
	err := s.Connect(ctx, socket.CMServer{Endpoint: "...", Type: "tcp"})

	// Send a message and wait for response (Job system)
	resp, err := s.SendSync(ctx, socket.Proto(enums.EMsg_ClientLogon, msg))

For low-level control, the underlying subsystems (Connector, Dispatcher, etc.)
can be accessed directly, though the Facade methods are recommended for most cases.
*/
package socket
