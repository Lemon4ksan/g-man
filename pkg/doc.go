// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package pkg contains the public API surface of the G-man framework.
//
// The G-man SDK is a modular, high-performance ecosystem for building automated
// Steam entities, ranging from simple WebAPI-based tools to complex, stateful
// trading bots and autonomous behaviors.
//
// # Architecture Layers
//
// The framework is organized into logical layers to ensure separation of
// concerns and scalability:
//
//  1. Core Layer (pkg/steam & pkg/protobuf): The foundation. Handles connection management,
//     protocol serialization (Protobuf/VDF/BVDF/JSON), and authentication. Key packages
//     include 'socket' for CM connections, 'transport' for protocol agnosticism, and 'encoding'.
//
//  2. System Layer (pkg/steam/sys): Manages internal Steam subsystems, such as
//     the Game Coordinator (GC) gateway, Account safety, Apps status, Notifications,
//     and the Steam Directory client for dynamic server discovery.
//
//  3. Trading & Domain Logic (pkg/trading & pkg/behavior): High-level engines for automated trading
//     and account behavior simulation. Trading features a middleware-based "Onion" architecture
//     for request validation, while behavior orchestrates routines like achievements and Guard confirmations.
//
//  4. Infrastructure: Common utilities like 'storage' (JSON/Memory adapters), 'network' (base TCP/WS sockets),
//     'command' (reflection-based CLI parser), 'log' (contextual structured logger), and 'crypto' (Steam TOTP).
//     Event-driven pub/sub communication is powered by 'miyako/bus'.
//
// # Core Philosophy
//
// Interface-Driven Design: Most components interact through interfaces like
// service.Doer or transport.Target. This allows for easy mocking in tests
// and flexible implementation swaps.
//
// Protocol Agnosticism: Through the service.Client, the SDK automatically decides
// the best route for a request—preferring an active Socket connection for speed,
// but falling back to HTTP if necessary.
//
// Concurrency and Atomic State: Core subsystems are designed for high-throughput.
// State management (sessions, job IDs, connection status) relies on atomic
// operations and RWMutexes to ensure thread safety without sacrificing performance.
//
// Smart Error Handling: The SDK distinguishes between transport-level failures
// and Steam "Soft Errors" (e.g., an HTML error page returning a 200 OK status).
// These are automatically converted into typed Go errors for reliable handling.
//
// # Usage Guidelines
//
//   - Context Support: Every blocking or network-bound operation accepts a
//     context.Context for proper cancellation and timeout management.
//   - No Global State: Subsystems must be initialized via constructors (e.g., NewClient)
//     with explicit dependency injection and functional options.
//   - Module System: Register custom modules with the orchestrator using options like WithModule.
//
// For examples and getting started guides, see the /examples directory in the
// repository root.
package pkg
