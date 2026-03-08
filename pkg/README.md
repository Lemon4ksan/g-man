# G-man SDK Packages

This directory contains the public API surface of the **G-man** framework.

These packages form a modular ecosystem. You can import the entire suite to build a full-featured bot, or cherry-pick specific packages (like `tf2/sku` or `steam/totp`) to integrate into your own existing applications.

## 📦 Package Overview

The architecture is divided into three logical layers: **Core**, **Modules**, and **Game Domains**.

### 1. ⚙️ Core (`pkg/steam`)

The foundation of the framework. It handles the low-level heavy lifting: network communication, protocol serialization, and event routing.

| Package | Description |
| :--- | :--- |
| **`steam`** | Contains the `Client` orchestrator. It ties the socket, auth, and event bus together. Acts as the Service Locator for modules. |
| **`steam/socket`** | The TCP/WebSocket connection manager. Handles packet fragmentation, encryption, and heartbeats. |
| **`steam/bus`** | A high-performance, reflection-based **Event Bus**. Allows modules to communicate asynchronously without tight coupling. |
| **`steam/transport`** | Protocol-agnostic transport layer. Allows seamless switching between HTTP (WebAPI) and TCP (CM) for API calls. |
| **`steam/api`** | Unified API client wrappers. Handles Steam's specific JSON/Protobuf quirks and `input_protobuf_encoded` logic. |
| **`steam/protocol`** | Generated Protobuf definitions and packet headers (`EMsg`). |

### 2. 🧩 Modules (`pkg/modules`)

Pluggable logic blocks that implement the `Module` interface (`Init` & `Start`). These provide the actual functionality of a Steam user.

| Package | Description |
| :--- | :--- |
| **`modules/auth`** | Handles the login sequence, JWT token management, and session refreshing. |
| **`modules/guard`** | Steam Guard Mobile Authenticator logic. Handles generating codes and confirming trades/market listings. |
| **`modules/econ`** | Economy subsystem. Manages **Trade Offers** via WebAPI and inventory context logic. |
| **`modules/coordinator`** | Game Coordinator (GC) gateway. Handles routing messages to games like TF2, CS2, or Dota 2. |
| **`modules/apps`** | Application state manager. Controls "In-Game" status and kicks other playing sessions. |

### 3. Game Domains (`pkg/tf2`, etc.)

Game-specific logic, schemas, and mathematics. These packages depend on `modules/coordinator` to talk to the game network.

| Package | Description |
| :--- | :--- |
| **`tf2`** | The main TF2 module. Handles the "Hello" handshake and inventory synchronization (`SOCache`). |
| **`tf2/sku`** | **Stock Keeping Unit**. Converts complex item objects into string identifiers (e.g., `5027;6`) and back. |
| **`tf2/schema`** | Schema Manager. Downloads, parses, and indexes `items_game.txt` for O(1) item lookups. |
| **`tf2/currencies`** | arithmetic for metal trading (Ref/Rec/Scrap). |

### 4. 🛠 Utilities & Integrations

General-purpose libraries and clients for external services.

| Package | Description |
| :--- | :--- |
| **`rest`** | A generic, robust HTTP client wrapper with JSON helpers and middleware support. |
| **`bans`** | Reputation checker. Integrates with SteamRep, Backpack.tf, and Marketplace.tf bans. |
| **`pricedb`** | Client for external pricing APIs (e.g., Prices.tf). |
| **`jobs`** | Concurrency primitives. A generic Job Manager for tracking request-response cycles. |
| **`log`** | Structured logging interface. |

---

## Usage Philosophy

G-man is built on **Dependency Injection** and **Composition**.

### Example: Building a Minimal Client

If you only need a client that stays online and handles Steam Guard:

```go
package main

import (
    "context"
    "github.com/lemon4ksan/g-man/pkg/steam"
    "github.com/lemon4ksan/g-man/pkg/modules/auth"
    "github.com/lemon4ksan/g-man/pkg/modules/guard"
    "github.com/lemon4ksan/g-man/pkg/modules/apps"
)

func main() {
    // 1. Create the Core Client
    client := steam.NewClient(steam.DefaultConfig())

    // 2. Register desired modules
    // The client will call Init() and Start() on them automatically.
    client.AddModule(apps.New())

    cfg := guard.DefaultConfig()
    cfg.IdentitySecret = "..."
    client.AddModule(guard.New(cfg))

    // 3. Login
    client.ConnectAndLogin(context.Background(), server, &auth.LogOnDetails{
        Username: "...",
        Password: "...",
    })
    
    client.Wait()
}
```

## 🏗 Contributing

When adding new packages to `pkg/`:

1. **Public by Default:** Code in `pkg/` is considered public API. Keep interfaces stable.
2. **No Global State:** Avoid global variables. Use structs and constructors.
3. **Context Aware:** All blocking operations (network I/O) must accept `context.Context`.
4. **Interface Segregation:** Define interfaces where the logic is *used*, not where it is *defined*.

### Architecture Notes

* **Events:** We use `steam/bus` for communication. Do not hard-link modules (e.g., `TF2` should not import `TradeManager` directly). Use events.
* **Transport:** Prefer `transport.Transport` interface over raw `http.Client` to allow switching between WebAPI and Socket.
