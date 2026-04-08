// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package steam provides the high-level, unified orchestrator (Client) for interacting
with all aspects of the Steam ecosystem.

It acts as the "brain" of the library, seamlessly integrating three distinct worlds:
  - Persistent CM Connections: Low-latency communication via Socket (TCP/WebSockets).
  - WebAPI Services: Standardized RPC calls over HTTP (Unified and Legacy services).
  - Steam Community: Scraping and interacting with the steamcommunity.com website.

# Core Concepts

The central piece of this package is the Client. Unlike lower-level packages, the
Client manages the full lifecycle of a Steam session, from initial handshake to
automatic WebAPI key registration.

1. Smart Routing:
The Client implements the service.Doer interface. When a request is made, the Client
automatically decides whether to send it via an active Socket connection (if the
message is compatible) or fallback to a standard HTTP WebAPI call. This ensures
maximum performance without manual transport management.

2. Automated Authentication:
The login process is fully orchestrated. Calling ConnectAndLogin performs a
multi-step sequence:
  - Establishes a connection to a Steam Connection Manager (CM).
  - Performs a secure LogOn (handling 2FA if required).
  - Automatically exchanges Refresh Tokens for modern Access Tokens.
  - Establishes a Web Session (cookies) for the Community site.
  - Automatically fetches or registers a WebAPI Key if one is missing.

3. Extensible Module System:
The library is designed to be extended through Modules. Modules can hook into
the Client's lifecycle (Init, Start, Close) and react to authentication events
(ModuleAuth). This allows for clean separation of logic like trading, chat, or
market management.

4. Event-Driven Architecture:
Built on a central Event Bus, the Client broadcasts state changes, network errors,
and incoming Steam packets, allowing decoupled components to react to global events.

# Basic Usage

	cfg := steam.DefaultConfig()
	client := steam.NewClient(cfg)

	// Perform a full login (Socket + Web)
	details := &auth.LogOnDetails{
		Username: "steam_user",
		Password: "steam_password",
	}

	err := client.ConnectAndLogin(ctx, cmServer, details)
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		// Handle guard events...
	}()

	// Use the Smart Proxy to call a service
	req := &pb.CPlayer_GetNickname_Request{Steamid: proto.Uint64(7656119...)}
	res, err := service.Unified[pb.CPlayer_GetNickname_Response](ctx, client.Service(), req)

# Concurrency and Thread Safety

The Client is designed to be fully thread-safe. All state transitions use atomic
operations, and internal maps are protected by RWMutexes. It is safe to use a
single Client instance across multiple goroutines.
*/
package steam
