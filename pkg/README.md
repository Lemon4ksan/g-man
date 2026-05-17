<div align="center">

# 📦 G-MAN SDK Packages

### Modular, Interface-Driven Components for Steam & Game Coordinator Automation

#### 🇺🇸 [English](README.md) • 🇷🇺 [Русский](README_RU.md)

</div>

This directory contains the public API surface of the **G-man** framework. These packages form a highly decoupled, modular ecosystem. You can import the entire suite to build a full-featured Steam bot, or cherry-pick individual packages (e.g., `tf2/sku` for price parsing, `steam/community` for inventory scraping, or `crypto` for mobile TOTP generation) to integrate directly into your existing Go applications.

## 🏗 Package Dependency Hierarchy

To maintain high performance and prevent circular imports (a common Go pitfall), G-man enforces a strict **layered import hierarchy**. Lower layers must never import higher layers:

```mermaid
flowchart TD
    classDef l4_node fill:#24273a,stroke:#cba6f7,stroke-width:2px,color:#cdd6f4,rx:8,ry:8;
    classDef l3_node fill:#1e1e2e,stroke:#89b4fa,stroke-width:2px,color:#cdd6f4,rx:8,ry:8;
    classDef l2_node fill:#181825,stroke:#a6e3a1,stroke-width:2px,color:#cdd6f4,rx:8,ry:8;
    classDef l1_node fill:#11111b,stroke:#f38ba8,stroke-width:2px,color:#cdd6f4,rx:8,ry:8;

    subgraph L4 ["🚀 Layer 4: Domain & Execution (Business Logic)"]
        direction LR
        TF2["<b>🎒 tf2</b><br/>Game Coordinator, Autopricer<br/>Economy & Item Schema"]
        Trading["<b>🤝 trading</b><br/>Onion Middleware Engine<br/>Offers Handling"]
        Client["<b>🤖 steam.Client</b><br/>Central Orchestrator<br/>Lifecycle Management"]
        
        TF2 -->|Triggers| Trading
        Trading ~~~ Client
    end
    class L4,TF2,Trading,Client l4_node;

    subgraph L3 ["🌐 Layer 3: Steam Networking & Social (Active Services)"]
        direction LR
        Socket["<b>🔌 steam/socket</b><br/>Stateful CM TCP/WSS<br/>EMsg Dispatcher"]
        Auth["<b>🔐 steam/auth</b><br/>OAuth2 & Tokens<br/>Silent Re-auth"]
        Comm["<b>🛡️ steam/community</b><br/>Defensive Scrapers<br/>Market & Inventory"]
        Social["<b>💬 steam/social</b><br/>Friends List, Chat<br/>Persona States"]
        
        Socket ~~~ Auth
        Auth ~~~ Comm
        Comm ~~~ Social
    end
    class L3,Socket,Auth,Comm,Social l3_node;

    subgraph L2 ["📜 Layer 2: Steam Base & Serialization (Data Formats)"]
        direction LR
        Protocol["<b>📦 steam/protocol</b><br/>Compiled Protobufs<br/>VDF Parser & EMsgs"]
        API["<b>⚙️ steam/api</b><br/>WebAPI Endpoints<br/>Typed REST Wrappers"]
        SID["<b>🆔 steam/id</b><br/>SteamID Math & Formats<br/>Account Types"]
        
        Protocol ~~~ API
        API ~~~ SID
    end
    class L2,Protocol,API,SID l2_node;

    subgraph L1 ["🛠️ Layer 1: Infrastructure Utilities (Foundational Layer)"]
        direction LR
        Log["<b>📊 log</b><br/>Structured Logger"]
        Bus["<b>🚌 bus</b><br/>Thread-Safe Pub/Sub"]
        Rest["<b>🌍 rest</b><br/>HTTP Retries & Client"]
        Storage["<b>💾 storage</b><br/>State Persistence (JSON/DB)"]
        Crypto["<b>🔑 crypto</b><br/>RSA/AES & Hashing"]
        
        Log ~~~ Bus
        Bus ~~~ Rest
        Rest ~~~ Storage
        Storage ~~~ Crypto
    end
    class L1,Log,Bus,Rest,Storage,Crypto l1_node;

    L4 ==>|Uses Services| L3
    L3 ==>|Serializes via| L2
    L2 ==>|Relies on| L1

    style L4 fill:#2b1836,stroke:#cba6f7,stroke-width:2px,stroke-dasharray: 5 5,color:#cba6f7
    style L3 fill:#1a2235,stroke:#89b4fa,stroke-width:2px,stroke-dasharray: 5 5,color:#89b4fa
    style L2 fill:#182823,stroke:#a6e3a1,stroke-width:2px,stroke-dasharray: 5 5,color:#a6e3a1
    style L1 fill:#301820,stroke:#f38ba8,stroke-width:2px,stroke-dasharray: 5 5,color:#f38ba8
```

## 📦 Package Overview

### 1. ⚙️ Core Layer (`pkg/steam`)
The foundation of the framework. It handles low-level heavy lifting: network communication, protocol serialization, and API orchestration.

| Package | Description |
| :--- | :--- |
| 🔌 **[steam](steam/)** | The main **Orchestrator**. Connects Socket, Auth, and Modules into a thread-safe `Client`. |
| 🌐 **[steam/api](steam/api/)** | Target specifications, Steam error types (`EResult`), and response unmarshalers (VDF, JSON, Proto). |
| 🔑 **[steam/auth](steam/auth/)** | Modern OAuth2 flows. Supports JWT, Refresh Tokens, and background re-auth cycles. |
| 🕵️‍♂️ **[steam/community](steam/community/)** | Defensive Web Client for scraping `steamcommunity.com` inventories, market, and OpenID. |
| 🛡️ **[steam/guard](steam/guard/)** | Mobile Authenticator confirmations, 2FA codes, and mobile session state. |
| 🆔 **[steam/id](steam/id/)** | Robust `SteamID` parser and formatter (SID2, SID3, and 64-bit formats). |
| 🔄 **[steam/socket](steam/socket/)** | Stateful Connection Manager (CM) client. Handles heartbeats, routing, and job tracking. |
| 📡 **[steam/service](steam/service/)** | RPC commander translating Protobuf messages into unified service calls. |
| 💬 **[steam/social](steam/social/)** | Social features: real-time persona states, friend lists, and chat. |
| 🌉 **[steam/transport](steam/transport/)** | Dual-stack transport bridge unifying CM Socket and HTTP into a single execution layer. |
| 🛠 **[steam/webapi](steam/webapi/)** | Auto-generated wrappers for Steam's Web APIs. |

### 2. 🔌 System & Game Coordinators (`pkg/steam/sys`)
Gateways to Steam's internal systems and individual game servers.

| Package | Description |
| :--- | :--- |
| 🕹 **[sys/gc](steam/sys/gc/)** | Base Game Coordinator client. Handles handshakes and multiplexed message routing. |
| 🗺 **[sys/directory](steam/sys/directory/)** | ISteamDirectory API client for dynamic retrieval of active Steam CM server IP lists. |
| 📦 **[sys/apps](steam/sys/apps/)** | In-game status manager and socket app notification handler. |

### 3. 🧠 Trading Logic (`pkg/trading`)
The high-level request-response engine for automated trading behaviors.

| Package | Description |
| :--- | :--- |
| 🧅 **[trading/engine](trading/engine/)** | The **Onion Middleware Engine**. Chains trade validation steps using context propagation. |
| ⚙️ **[trading/processor](trading/processor/)** | Core transaction lifecycle manager (*Check $\rightarrow$ Decide $\rightarrow$ Act $\rightarrow$ Notify*). |
| 📋 **[trading/review](trading/review/)** | High-value transaction auditing, trade logging, and administrative reviews. |
| 🤝 **[trading/live](trading/live/)** | Support for GC-based real-time "Live Trade" sessions. |
| 🌐 **[trading/web](trading/web/)** | Traditional Web-based Trade Offer operations via the Community API. |

### ⚔️ 4. TF2 Domain (`pkg/tf2`)
Specialized packages built for Team Fortress 2 item schemas, pricing, and coordinate automation.

| Package | Description |
| :--- | :--- |
| 🏷 **[tf2/sku](tf2/sku/)** | String-based SKU parser and generator for unique TF2 item identifiers. |
| 📈 **[tf2/bptf](tf2/bptf/)** | Interface for backpack.tf snapshot, pricing, and active listing managers. |
| 🪙 **[tf2/currency](tf2/currency/)** | Metal & Key arithmetic. Safe math for Refined, Reclaimed, Scrap, and Key consolidation. |
| 🎒 **[tf2/inventory](tf2/inventory/)** | Unified inventory coordinator syncing Web and Game Coordinator (`SOCache`) inventories. |
| 💲 **[tf2/pricedb](tf2/pricedb/)** | Pluggable local price manager synchronized with real-time Socket.IO pricing providers. |
| 📖 **[tf2/schema](tf2/schema/)** | Dynamically updated TF2 item schema indexer for O(1) attribute and defindex lookups. |
| 🔨 **[tf2/crafting](tf2/crafting/)** | Automated crafting routines: weapon combining, smelting, and key refining. |

### 🛠 5. Infrastructure & Storage
Utilities and core persistent storage providers used across the SDK.

| Package | Description |
| :--- | :--- |
| 🎭 **[behavior](behavior/)** | Universal autonomous routines, including human-mimicking achievements and stats simulation. |
| 🚌 **[bus](bus/)** | High-performance, lock-free **Event Bus** for decoupled pub/sub modules. |
| 🔐 **[crypto](crypto/)** | ECC, RSA cryptography, and TOTP algorithms for security operations. |
| ⏳ **[jobs](jobs/)** | Thread-safe asynchronous job tracker for matching socket requests with responses. |
| 📝 **[log](log/)** | Contextual, structured, module-aware logger. |
| 🚀 **[rest](rest/)** | HTTP client wrapper featuring automatic retries, exponential backoffs, and parameters serialization. |
| 💾 **[storage](storage/)** | Interface-first storage provider with SQLite, JSON, and in-memory backends. |

## 🏗 Architecture & Philosophy

G-man packages are engineered with **Go best practices** in mind:

1. **Protocol Agnosticism**: Applications communicate with Steam via `steam.Client.Do()`. The internal routing engine automatically selects either the active CM Socket (for real-time speed) or HTTP WebAPI (as a fallback) depending on connectivity status.
2. **Interface-First Design**: Components communicate using tight consumer-defined interfaces. Rather than depending on concrete clients, structures depend on `Requester` or `Doer` contracts, keeping the system fully mockable.
3. **Concurrency Safety**: Circular states, heartbeats, and packet routing are managed using `sync/atomic` and read-write mutexes. All blocking operations explicitly accept a `context.Context`.
4. **Defensive Web Scraping**: The `community` client proactively converts hidden HTML errors (e.g., rate limits disguised as standard web views) into strictly typed Go errors like `ErrRateLimited`.
