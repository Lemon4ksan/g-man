# G-man SDK Packages

This directory contains the public API surface of the **G-man** framework.

These packages form a modular ecosystem. You can import the entire suite to build a full-featured bot, or cherry-pick specific packages (like `tf2/sku` or `steam/crypto/totp`) to integrate into your own existing applications.

## 📦 Package Overview

The architecture is divided into logical layers: **Core**, **Modules**, **Game Domains**, **Business Logic**, and **Utilities**.

### 1. ⚙️ Core (`pkg/steam`)

The foundation of the framework. It handles the low-level heavy lifting: network communication, protocol serialization, and event routing.

| Package | Description |
| :--- | :--- |
| **`steam`** | Contains the `Client` orchestrator. It ties the socket, auth, and modules together, acting as a Service Locator. |
| **`steam/socket`** | The TCP/WebSocket connection manager. Handles packet fragmentation, encryption, job tracking, and heartbeats. |
| **`steam/bus`** | A high-performance, type-based **Event Bus**. Allows modules to communicate asynchronously without tight coupling. |
| **`steam/transport`** | A protocol-agnostic transport layer. Allows seamless switching between HTTP (WebAPI) and TCP (CM) for API calls. |
| **`steam/api`** | Unified API client wrappers. Handles Steam's specific JSON/Protobuf quirks and `input_protobuf_encoded` logic. |
| **`steam/protocol`** | Defines packet structures, headers (`MsgHdr`, `MsgHdrProtoBuf`), `EMsg` constants, and GC packet logic. |
| **`steam/crypto`** | Implements Steam-specific cryptography (AES-256-CBC, RSA-OAEP for session keys) and TOTP generation. |

### 2. 🧩 Modules (`pkg/modules`)

Pluggable logic blocks that implement the `Module` interface (`Init` & `Start`). These provide the actual functionality of a Steam user.

| Package | Description |
| :--- | :--- |
| **`modules/auth`** | Handles the entire login sequence (password/JWT), session refreshing, and `WebSession` creation for community access. |
| **`modules/guard`** | Steam Guard Mobile Authenticator logic. Handles 2FA code generation and mobile confirmation polling/acceptance. |
| **`modules/econ`** | Economy subsystem. Contains sub-packages for managing **Trade Offers** (`trading`) and live trade invitations (`offers`). |
| **`modules/coordinator`** | Game Coordinator (GC) gateway. Handles routing and job management for messages to games like TF2, CS2, or Dota 2. |
| **`modules/apps`** | Application state manager. Controls the "In-Game" status and can kick other playing sessions. |
| **`modules/directory`** | Connection Manager Service and optimal server selection. |
| **`modules/friends`** | Manages friends and persona state changes. |
| **`modules/market`** | Steam community market integration and item price api. |

### 3. 🎮 Game Domains (`pkg/tf2`, etc.)

Game-specific logic, schemas, and mathematics. These packages often depend on `modules/coordinator` to communicate with the game network.

| Package | Description |
| :--- | :--- |
| **`tf2`** | The main TF2 module. Handles the GC "Hello" handshake, inventory synchronization (`SOCache`), and exposes game-specific actions. |
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
| **`rest`** | A generic, robust HTTP client wrapper with JSON helpers and request modifier support. |
| **`jobs`** | Concurrency primitives. A generic Job Manager for tracking asynchronous request-response cycles. |
| **`log`** | A structured, asynchronous logging interface with beautiful console output. |
| **`openid`** | Implements the Steam OpenID login flow for third-party websites using existing session cookies. |

---

## Usage Philosophy

G-man is built on **Dependency Injection** and **Composition over Inheritance**.

### Example: Building a Minimal Client

Modules are not added to a running client; they are provided during initialization using the `WithModule` functional option.

```go
package main

import (
    "context"
    "github.com/lemon4ksan/g-man/pkg/modules/apps"
    "github.com/lemon4ksan/g-man/pkg/modules/auth"
    "github.com/lemon4ksan/g-man/pkg/modules/guard"
    "github.com/lemon4ksan/g-man/pkg/steam"
    "github.com/lemon4ksan/g-man/pkg/steam/socket"
)

func main() {
    // Define which modules you need.
    appsMod := apps.New()

    guardCfg := guard.DefaultConfig()
    guardCfg.IdentitySecret = "..." // Your base64 identity_secret
    guardCfg.SharedSecret = "..."  // Your base64 shared_secret
    guardCfg.DeviceID = "android:..."
    guardMod, err := guard.New(guardCfg)
    if err != nil {
        panic(err)
    }

    // Create the Core Client with the desired modules.
    // The client will call Init() and Start() on them automatically.
    client := steam.NewClient(
        steam.DefaultConfig(),
        steam.WithModule(appsMod),
        steam.WithModule(guardMod),
    )

    // Login
    server, err := directory.GetOptimalCMServer(context.Background())
    if err != nil {
        logger.Error("Failed to get cm server", log.Err(err))
        return
    }

    err = client.ConnectAndLogin(context.Background(), server, &auth.LogOnDetails{
        AccountName: "...",
        Password:    "...",
    })
    if err != nil {
        panic(err)
    }

    // Block until the client is closed.
    client.Wait()
}
```

## 🏗 Contributing

When adding new packages to `pkg/`:

1. **Public by Default:** Code in `pkg/` is considered public API. Keep interfaces stable.
2. **No Global State:** Avoid global variables. Use structs and constructors (`New...`).
3. **Context Aware:** All blocking operations (network I/O, long loops) must accept `context.Context`.
4. **Interface Segregation:** Define interfaces where the logic is *used* (as a dependency), not where it is *defined*.
