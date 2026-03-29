# G-man SDK Packages

This directory contains the public API surface of the **G-man** framework.

These packages form a modular ecosystem. You can import the entire suite to build a full-featured Steam bot, or cherry-pick specific packages (like `tf2/sku` or `steam/crypto/totp`) to integrate into your own existing applications.

## 📦 Package Overview

The architecture is divided into logical layers: **Core**, **Modules**, **Storage**, **Game Domains**, **Business Logic**, and **Utilities**.

### 1. ⚙️ Core (`pkg/steam`)

The foundation of the framework. It handles the low-level heavy lifting: network communication, protocol serialization, event routing, and API orchestration.

| Package | Description |
| :--- | :--- |
| **`steam`** | The `Client` orchestrator. Manages the lifecycle of modules, storage, and authenticated sessions. |
| **`steam/socket`** | TCP/WebSocket connection manager. Handles packet fragmentation, encryption, and heartbeats. |
| **`steam/network`** | Low-level physical layer. Implements raw TCP and WebSocket dialing and message framing. |
| **`steam/bus`** | High-performance, type-safe **Event Bus**. Enables decoupled communication between modules. |
| **`steam/transport`** | Protocol-agnostic transport layer. Seamlessly switches between HTTP (WebAPI) and TCP (CM). |
| **`steam/service`** | RPC-like commander. Translates Go method calls into WebAPI or Unified (Protobuf) requests. |
| **`steam/community`** | Specialized HTTP client for `steamcommunity.com`. Handles session cookies and HTML error scraping. |

### 2. 🧩 Modules (`pkg/modules`)

Pluggable logic blocks that implement the actual functionality of a Steam user. All modules embed `BaseModule` for standardized logging, context, and goroutine management.

| Package | Description |
| :--- | :--- |
| **`modules/auth`** | Handles login flows (Password/JWT), 2FA challenges, and `WebSession` cookie management. |
| **`modules/guard`** | Steam Guard Mobile Authenticator. Handles confirmation polling and auto-acceptance. |
| **`modules/trading`** | Asynchronous Trade Offer manager. Polls for offers and manages state transitions. |
| **`modules/offers`** | Real-time "Live Trade" invitations manager via the CM socket. |
| **`modules/coordinator`** | Game Coordinator (GC) gateway. Routes messages to specific games (TF2, CS2, Dota 2). |
| **`modules/apps`** | Application state manager. Controls "In-Game" status and tracks player counts. |
| **`modules/friends`** | Manages friends list, persona states, and group invitations. |

### 3. 💾 Storage (`pkg/storage`)

Provides persistence capabilities to allow bots to recover state after restarts.

| Package | Description |
| :--- | :--- |
| **`storage`** | Core interfaces for persistence: `AuthStore` (tokens) and `KVStore` (generic data). |
| **`storage/memory`** | Default ephemeral storage implementation using in-memory maps. |

### 4. 🧠 Business Logic (`pkg/offer`)

High-level logic for implementing complex trading behaviors.

| Package | Description |
| :--- | :--- |
| **`offer/engine`** | **Trade Middleware Engine**. Implements an "Onion Architecture" chain for offer validation. |
| **`offer/processor`** | High-level orchestrator. Coordinates the entire trade lifecycle: check -> decide -> act -> notify. |
| **`offer/notifications`** | Template-based chat responder for sending trade status updates to partners. |
| **`offer/review`** | Formatter and reporter for sending detailed trade alerts to administrators. |

### 5. 🎮 Game Domains (`pkg/tf2`, etc.)

Game-specific logic and schemas.

| Package | Description |
| :--- | :--- |
| **`tf2`** | Main TF2 module. Implements **SOCache** for real-time inventory mirroring. |
| **`tf2/schema`** | Schema Manager. Downloads and indexes `items_game.txt` for O(1) item lookups. |
| **`tf2/sku`** | Stock Keeping Unit conversion for TF2 items. |

---

## 🏗 Architecture & Philosophy

G-man is built on **Declarative Configuration**, **Dependency Injection**, and **Event-Driven Design**.

1. **Declarative Config**: You don't "add" modules; you describe them in `steam.Config`. The client automatically instantiates and wires them together.
2. **BaseModule Pattern**: Every module is a "clean citizen". They don't touch global state and manage their own background workers using a provided `context.Context`.
3. **Middleware-First**: Trade logic is not a mess of `if-else` statements. It's a clean chain of responsibilities (Middlewares) like `Recover` -> `Logger` -> `Blacklist` -> `Pricer`.
4. **Persistent by Design**: With the `storage` layer, the bot can save Refresh Tokens and offer states, allowing for seamless auto-relogin after a crash or update.

## 🤝 Contributing

When adding new packages or modifying existing ones in `pkg/`:

1. **Public by Default:** Code in `pkg/` is considered public API. Keep interfaces stable and document exported functions.
2. **No Global State:** Avoid global variables. Use structs and constructors (`New...`).
3. **Context Aware:** All blocking operations must respect `context.Context` for proper shutdown.
4. **Embed BaseModule:** All new functional modules in `pkg/modules` should embed `modules.BaseModule`.
5. **Use `rest` for HTTP:** Avoid raw `http.Client`. Leverage the `pkg/rest` or `pkg/steam/community` wrappers.
