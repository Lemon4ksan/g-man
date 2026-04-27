// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package socket provides the core, stateful, event-driven engine for managing a
persistent connection to Steam's Connection Manager (CM) servers.

It acts as a high-level orchestrator that sits between raw network connections
(TCP/WebSocket) and the application modules. The package handles message
serialization, routing, job tracking (RPC-style calls), and automatic
decompression of Steam "Multi" messages.

# Core Concepts

The socket layer revolves around four main pillars:

 1. Connection Management: Handles dialers, handshakes, and heartbeats. It supports
    multiple transport protocols (TCP, WebSockets) through the ConnectionDialer interface.
    It includes a robust ReconnectPolicy with exponential backoff.

 2. Session & Security: Manages the lifecycle of a Steam session, including SteamID,
    SessionID, and OAuth tokens. It transparently handles channel encryption (AES/RSA)
    upgrades during the initial handshake.

 3. Message Dispatching: Routes incoming packets to registered handlers based on
    the EMsg (message type) or the Service Method name (for Unified Services).

 4. Job Tracking: Implements an asynchronous request-response pattern using Job IDs.
    Methods like SendSync wait for a response, while Send with a callback allows
    non-blocking interaction.

# Event System

The socket is tightly integrated with an event bus. Instead of polling, users should
subscribe to events to react to connection changes:

	sub := sock.Bus().Subscribe(socket.StateEvent{}, socket.ConnectedEvent{})
	go func() {
	    for ev := range sub.C() {
	        switch e := ev.(type) {
	        case *socket.StateEvent:
	            fmt.Printf("State changed: %s -> %s\n", e.Old, e.New)
	        }
	    }
	}()

# Concurrency Model

Socket uses a worker-pool pattern to process incoming messages. This ensures
that slow packet handlers or complex decompression tasks do not block the
underlying network read loop. State is managed using a mix of atomic
operations for lifecycle status and RWMutexes for configuration data.

IMPORTANT: Handlers should be non-blocking. For heavy tasks (DB I/O, complex
calculations), start a new goroutine inside the handler to avoid starving
the worker pool.

# Packet Handling Flow

	Network -> inboundHandler -> processSingle -> msgCh -> Workers -> routePacket -> Handler/Job

The router automatically detects special Steam messages, such as EMsg_Multi,
which are transparently decompressed (GZIP) and re-dispatched as individual
packets.

# Basic Usage

	cfg := socket.DefaultConfig()
	sock := socket.NewSocket(cfg)

	// Connect to a CM server (non-blocking)
	err := sock.Connect(socket.CMServer{Endpoint: "1.2.3.4:27017", Type: "tcp"})
	if err != nil {
	    log.Fatal(err)
	}

	// Send a protobuf message synchronously
	resp, err := sock.SendSync(ctx, socket.Proto(enums.EMsg_ClientLogon, logonMsg))
*/
package socket
