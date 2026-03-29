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

The socket layer revolves around three main pillars:

 1. Connection Management: Handles dialers, handshakes, and heartbeats. It supports
    multiple transport protocols (TCP, WebSockets) through the ConnectionDialer interface.

 2. Message Dispatching: Routes incoming packets to registered handlers based on
    the EMsg (message type) or the Service Method name (for Unified Services).

 3. Job Tracking: Implements an asynchronous request-response pattern using Job IDs.
    Methods like CallProto and CallUnified return results via callbacks, mapping
    Steam's asynchronous responses back to the original callers.

# Concurrency Model

Socket uses a worker-pool pattern to process incoming messages. This ensures
that slow packet handlers or complex decompression tasks do not block the
underlying network read loop. State is managed using a mix of atomic
operations for lifecycle status and RWMutexes for configuration data.

# Packet Handling Flow

	Network -> inboundHandler -> processSingle -> msgCh -> Workers -> routePacket -> Handler/Job

The router automatically detects special Steam messages, such as EMsg_Multi,
which are transparently decompressed (GZIP) and re-dispatched as individual
packets.
*/
package socket
