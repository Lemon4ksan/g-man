# G-man SDK Packages

This directory contains the public API surface of the **G-man** framework.

These packages form a modular ecosystem. You can import the entire suite to build a full-featured Steam bot, or cherry-pick specific packages (like `tf2/sku` for price parsing or `steam/community/openid` for third-party logins) to integrate into your own existing applications.

## 📦 Package Overview

The architecture is divided into logical layers: **Core**, **System**, **Game Domains**, **Trading Logic**, and **Infrastructure**.

### 1. ⚙️ Core Layer (`pkg/steam`)

The foundation of the framework. It handles low-level heavy lifting: network communication, protocol serialization, and API orchestration.

| Package               | Description                                                                                                                                           |
|:----------------------|:------------------------------------------------------------------------------------------------------------------------------------------------------|
| **`steam`**           | The main **Orchestrator**. Connects Socket, Auth, and Modules into a single `Client`.                                                                 |
| **`steam/api`**       | HTTP target implementation, common Steam error types (`EResult`) and universal response unmarshalers (VDF, JSON, Proto).                              |
| **`steam/auth`**      | Modern authentication flows. Supports JWT and Refresh Tokens with persistent storage.                                                                 |
| **`steam/community`** | Advanced Web client for `steamcommunity.com`. Includes a specialized sub-client for the **Inventory**, **Market** and **OpenID** for 3rd-party sites. |
| **`steam/guard`**     | Steam Guard Mobile Authenticator confirmations and 2FA codes.                                                                                         |
| **`steam/id`**        | Robust `SteamID` parser and formatter (supports SID2, SID3, and 64-bit formats).                                                                      |
| **`steam/socket`**    | Stateful CM connection manager. Handles packet routing, job tracking, and GZIP decompression.                                                         |
| **`steam/service`**   | RPC Commander. Translates Protobuf messages into Unified or Legacy Steam service calls.                                                               |
| **`steam/social`**    | Implements the **Friends** list and **Chat** (Persona states, messaging, and relationships).                                                          |
| **`steam/transport`** | The **Architectural Bridge**. Unifies HTTP (WebAPI) and Socket (CM) into a single `Do` interface.                                                     |
| **`steam/webapi`**    | Generated wrappers for the Steam Web API.                                                                                                             |

### 2. 🔌 System & GC (`pkg/steam/sys`)

Handles internal Steam subsystems and the gateway to game-specific coordinators.

| Package             | Description                                                                                   |
|:--------------------|:----------------------------------------------------------------------------------------------|
| **`sys/gc`**        | Base **Game Coordinator** implementation. Handles GC-Hello handshakes and nested job routing. |
| **`sys/directory`** | Client for `ISteamDirectory`. Dynamically fetches the best CM server list from Steam.         |
| **`sys/apps`**      | Manages "In-Game" status and handles app-specific socket notifications.                       |

### 3. 🧠 Trading Logic (`pkg/trading`)

High-level engine for implementing complex, automated trading behaviors.

| Package                 | Description                                                                                                  |
|:------------------------|:-------------------------------------------------------------------------------------------------------------|
| **`trading/engine`**    | **Trade Middleware Engine**. Implements an "Onion" chain for request validation (e.g., Blacklist -> Pricer). |
| **`trading/processor`** | Core lifecycle manager. Coordinates: *Check -> Decide -> Act -> Notify*.                                     |
| **`trading/review`**    | Decision logging and administrative reporting for high-value trades.                                         |
| **`trading/live`**      | Handles real-time "Live Trade" (proto-based) invitations and state.                                          |
| **`trading/web`**       | Handles standard Web-based Trade Offers via the Community API.                                               |

### 4. ⚔️ TF2 Domain (`pkg/tf2`)

Specialized logic for Team Fortress 2 economy and automation.

| Package             | Description                                                                                    |
|:--------------------|:-----------------------------------------------------------------------------------------------|
| **`tf2/currency`**  | Metal and Key manager. Handles pure currency formatting, counting, and backpack consolidation. |
| **`tf2/inventory`** | Unified inventory manager. Syncs state via both **WebAPI** and **Game Coordinator** (SOCache). |
| **`tf2/pricedb`**   | PriceDB integration for fetching item prices.                                                  |
| **`tf2/schema`**    | Item Schema indexer. Maps `defindex` to names, qualities, and rarities with O(1) lookups.      |
| **`tf2/sku`**       | String-based SKU generator and parser for unique item identification.                          |
| **`tf2/bptf`**      | Integration for the **Backpack.tf** API (Pricing, Listings, and Heartbeats).                   |
| **`tf2/crafting`**  | Logic for automated smelting, combining, and item crafting.                                    |

### 5. 🛠 Infrastructure & Storage

Common utilities and persistence layers used across the SDK.

| Package       | Description                                                                                           |
|:--------------|:------------------------------------------------------------------------------------------------------|
| **`storage`** | Interface-first persistence. Includes implementations for **SQLite**, **JSON Files**, and **Memory**. |
| **`bus`**     | Internal high-performance **Event Bus** for decoupled module communication.                           |
| **`jobs`**    | Generic asynchronous callback manager for tracking request-response cycles.                           |
| **`rest`**    | A robust HTTP client wrapper with built-in retry logic and struct-to-param mapping.                   |
| **`crypto`**  | Steam-specific cryptography (ECC, RSA) and **TOTP** (2FA) generation.                                 |
| **`log`**     | Structured, module-aware logging system.                                                              |

---

## 🏗 Architecture & Philosophy

G-man is built on **Interface-Driven Design**, **Concurrency Safety**, and **Protocol Agnosticism**.

1. **Protocol Agnosticism**: Use `steam.Client.Do()`. The SDK automatically decides whether to send a request via an active Socket (for speed) or fallback to HTTP (if disconnected).
2. **Atomic State**: All core components (Socket, Session, Auth) use `sync/atomic` and `RWMutex` to ensure they are safe for use in high-throughput concurrent bots.
3. **Smart Error Handling**: The `community` package detects "Soft Errors" (HTML error pages returning 200 OK) and converts them into typed Go errors like `ErrNotLoggedIn`.
4. **Middleware-First Trading**: Don't write `if/else` hell. Use `trading/engine` to chain validation logic: `Recover -> Logger -> Blacklist -> InventoryCheck -> PriceCheck`.

## 🤝 Contributing

When adding new packages or modifying existing ones:

1. **Keep `pkg/steam` Lean:** Only core Steam logic belongs here. Game-specific logic goes into `pkg/<game>`.
2. **Context-Aware:** Every blocking or network operation **must** accept a `context.Context`.
3. **No Global State:** Use constructors (`NewClient`, `NewSocket`) and inject dependencies.
4. **Document Exports:** All public-facing functions in `pkg/` must have a docstring for IDE IntelliSense support.
5. **Interface over Implementation:** Depend on `Requester` or `Doer` interfaces rather than concrete `Client` structs where possible.
