# G-man SDK Packages

This directory contains the public API surface of the **G-man** framework.

These packages form a modular ecosystem. You can import the entire suite to build a full-featured Steam bot, or cherry-pick specific packages (like `tf2/sku` or `steam/crypto/totp`) to integrate into your own existing applications.

## 📦 Package Overview

The architecture is divided into logical layers: **Core**, **Modules**, **Game Domains**, **Business Logic**, and **Utilities**.

### 1. ⚙️ Core (`pkg/steam`)

The foundation of the framework. It handles the low-level heavy lifting: network communication, protocol serialization, event routing, and HTTP sessions.

| Package | Description |
| :--- | :--- |
| **`steam`** | Contains the `Client` orchestrator. It ties the socket, auth, and modules together, managing their lifecycles. |
| **`steam/socket`** | The TCP/WebSocket connection manager. Handles packet fragmentation, encryption, job tracking, and heartbeats. |
| **`steam/network`** | The lowest-level physical layer. Implements raw TCP and WebSocket dialing and message framing. |
| **`steam/bus`** | A high-performance, type-safe **Event Bus**. Allows modules to communicate asynchronously without tight coupling. |
| **`steam/transport`** | A protocol-agnostic transport layer. Allows seamless switching between HTTP (WebAPI) and TCP (CM) for API calls. |
| **`steam/api`** | Unified API wrapper. Handles Steam's specific JSON/VDF/Protobuf quirks and `input_protobuf_encoded` logic. |
| **`steam/protocol`** | Defines packet structures, headers (`MsgHdr`, `MsgHdrProtoBuf`), `EMsg` constants, and Protobuf compilation logic. |
| **`steam/crypto`** | Implements Steam-specific cryptography (AES-256-CBC, RSA-OAEP for session keys) and TOTP generation. |
| **`steam/community`** | A specialized HTTP client for `steamcommunity.com`. Handles session cookies, CSRF tokens, and scrapes HTML errors (like Family View or "Sorry!" pages). |
| **`steam/service`** | The RPC-like commander for official Steam APIs. Translates Go method calls into WebAPI or Unified (Protobuf) requests. |

### 2. 🧩 Modules (`pkg/modules`)

Pluggable logic blocks that implement the `Module` interface (`Init`, `StartAuthed`, `Close`). These provide the actual functionality of a Steam user. All modules embed `modules.BaseModule` for zero-boilerplate lifecycle management.

| Package | Description |
| :--- | :--- |
| **`modules/auth`** | Handles the entire login sequence (password/JWT), 2FA challenges, and `WebSession` creation for community access. |
| **`modules/guard`** | Steam Guard Mobile Authenticator logic. Handles mobile confirmation polling and auto-acceptance. |
| **`modules/trading`** | Trade Offer manager. Polls for new offers, manages state changes, and exposes `Accept`/`Decline`/`Cancel` methods. |
| **`modules/offers`** | Live Trade session manager. Handles real-time trade invitations (proposing, accepting, canceling) via the CM socket. |
| **`modules/coordinator`** | Game Coordinator (GC) gateway. Handles routing and job management for messages to games like TF2, CS2, or Dota 2. |
| **`modules/apps`** | Application state manager. Controls the "In-Game" status, tracks player counts, and can kick other playing sessions. |
| **`modules/friends`** | Manages the friends list, persona state changes (online/offline, avatars), and group invitations. |
| **`modules/market`** | Steam Community Market integration. Handles creating/canceling buy and sell orders with precise currency formatting. |
| **`modules/directory`** | Connection Manager Service. Discovers and selects the optimal CM server for the initial connection. |

### 3. 🎮 Game Domains (`pkg/tf2`, etc.)

Game-specific logic, schemas, and mathematics. These packages depend on `modules/coordinator` to communicate with the game's internal network.

| Package | Description |
| :--- | :--- |
| **`tf2`** | The main TF2 module. Handles the GC "Hello" handshake, inventory synchronization (`SOCache`), and game-specific actions. |
| **`tf2/sku`** | **Stock Keeping Unit**. Converts complex item objects into standardized string identifiers (e.g., `5021;6`) and back. |
| **`tf2/schema`** | A powerful Schema Manager. Downloads, parses, and indexes `items_game.txt` for O(1) item lookups and normalization. |
| **`tf2/econ`** | Contains currency arithmetic (Keys/Ref/Rec/Scrap) and trade value difference calculations. |
| **`tf2/crafting`** | Provides high-level abstractions for crafting metal and weapons based on inventory state. |

### 4. 🧠 Business Logic (`pkg/offer`)

High-level logic for implementing automated trading bots, separated from the core modules.

| Package | Description |
| :--- | :--- |
| **`offer/reason`** | Defines a comprehensive set of constants for trade acceptance, rejection, or review reasons. |
| **`offer/review`** | A sophisticated offer reviewer that processes trade metadata to generate human-readable decline messages and admin alerts. |
| **`offer/notifications`** | A customizable chat responder for sending status updates to trade partners. |

### 5. 🛠 Utilities

General-purpose libraries and clients for internal and external services.

| Package | Description |
| :--- | :--- |
| **`rest`** | A generic, robust HTTP client wrapper using Go Generics. Features JSON helpers and RequestModifier chaining. |
| **`jobs`** | Concurrency primitives. A thread-safe Job Manager for tracking asynchronous request-response cycles with timeouts. |
| **`log`** | A structured, asynchronous logging interface with beautiful, zero-allocation console output. |
| **`openid`** | Implements the Steam OpenID login flow for third-party websites using existing active session cookies. |

---

## 🏗 Architecture & Philosophy

G-man is built on **Dependency Injection**, **Event-Driven Architecture**, and **Composition over Inheritance**.

1. **Event-Driven**: Modules rarely call each other directly. Instead, they publish and subscribe to events via the `steam/bus`. For example, `trading` publishes a `NewOfferEvent`, which your bot's logic subscribes to.
2. **BaseModule Pattern**: To prevent DRY violations, all standard modules embed `modules.BaseModule`, which automatically provides them with a scoped `context.Context`, a pre-configured `log.Logger`, and safe goroutine management (`Wg`).
3. **Transport Agnosticism**: The `service` package doesn't care if it's talking to Steam via HTTP or a TCP Socket. The `transport` layer abstracts this away.

### Example: Building a Client

In G-man, modules are initialized declaratively via the central `steam.Config`.

```go
package main

import (
    "context"

    "github.com/lemon4ksan/g-man/pkg/log"
    "github.com/lemon4ksan/g-man/pkg/modules/guard"
    "github.com/lemon4ksan/g-man/pkg/modules/trading"
    "github.com/lemon4ksan/g-man/pkg/steam"
    "github.com/lemon4ksan/g-man/pkg/modules/directory"
)

func main() {
    logger := log.New(log.DefaultConfig(log.DebugLevel))

    // Define the configuration for the core client and its modules.
    // If a module's config is nil, that module is simply not loaded.
    cfg := steam.Config{
        Guard: &guard.Config{
            IdentitySecret: "base64_identity_secret",
            DeviceID:       "android:12345678-1234-1234-1234-123456789012",
            AutoAccept:     true,
            RateLimit:      2 * time.Second,
        },
        Trading: &trading.Config{
            PollInterval: 15 * time.Second,
            Language:     "english",
        },
    }

    // Create the Client. It will automatically instantiate, Init(), 
    // and inject dependencies into the Guard and Trading modules.
    client := steam.NewClient(cfg, steam.WithLogger(logger))

    // Find an optimal server
    server, err := directory.GetOptimalCMServer(context.Background())
    if err != nil {
        logger.Error("Failed to get CM server", log.Err(err))
        return
    }

    // Connect and Authenticate
    err = client.ConnectAndLogin(context.Background(), server, steam.LogOnDetails{
        AccountName: "username",
        Password:    "password",
        // G-man will prompt for 2FA via the Event Bus if needed.
    })
    if err != nil {
        logger.Error("Login failed", log.Err(err))
        return
    }

    // Block main thread until the client is disconnected or closed.
    client.Wait()
}
```

## 🤝 Contributing

When adding new packages or modifying existing ones in `pkg/`:

1. **Public by Default:** Code in `pkg/` is considered public API. Keep interfaces stable and document exported functions.
2. **No Global State:** Avoid global variables (`var Client *steam.Client`). Use structs and constructors (`New...`).
3. **Context Aware:** All blocking operations (network I/O, delays, long loops) must accept and respect `context.Context`.
4. **Interface Segregation:** Define interfaces where the logic is *used* (as a dependency requirement), not where the concrete struct is *defined*.
5. **Use `rest` for HTTP:** Do not use raw `http.Client`. Use the `pkg/rest` or `pkg/steam/community` wrappers to ensure consistent header injection and error handling.
