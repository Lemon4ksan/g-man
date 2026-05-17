<div align="center">

# G-MAN

### The Ultimate Steam & Multi-Game Trading Bot Framework for Go

[![Go Reference](https://pkg.go.dev/badge/github.com/lemon4ksan/g-man.svg)](https://pkg.go.dev/github.com/lemon4ksan/g-man)
[![Go Report Card](https://goreportcard.com/badge/github.com/lemon4ksan/g-man)](https://goreportcard.com/report/github.com/lemon4ksan/g-man)
[![License](https://img.shields.io/github/license/lemon4ksan/g-man)](LICENSE)

> *"The right bot in the wrong place can make all the difference in the skins market."*

</div>

---

**G-man** is a high-performance Steam client library and game automation framework architected for high-frequency trading, industrial-scale item management, and resilient network operations. Unlike legacy wrappers, G-man treats the Steam Network and associated Game Coordinators as a unified entity, seamlessly blending **Socket (CM)**, **WebAPI**, and **Game Coordinator** protocols into a single, thread-safe orchestrator.

## ⚡ Key Features

* **Self-Healing Sessions (Silent Re-auth)**: Eliminate the #1 cause of bot downtime. G-man monitors session health in real-time. If an Access Token or Web Cookie expires mid-request, the orchestrator automatically pauses, performs a background OAuth2 refresh, and retries the operation transparently. Your business logic never sees a "401 Unauthorized."
* **Dual-Stack Transport Engine**: Stop worrying about whether to use WebAPI or Sockets. G-man features a protocol-agnostic routing layer. It automatically selects the most efficient path - **TCP/WebSocket** for speed and real-time state, or **HTTPS** for stealth and reliability - switching between them seamlessly if one becomes unstable.
* **True Concurrency**: Escape the "Node.js Event-Loop bottleneck." Built on Go's CSP model, G-man is designed to manage hundreds of accounts and thousands of concurrent trade offers within a single process. High-frequency trade floods are handled via thread-safe worker pools, not single-threaded queues.
* **Deep Defensive Scraping**: Steam's "Soft Errors" are the silent killers of automation. G-man's `community` engine doesn't just check HTTP codes; it proactively scrapes response bodies for "Sorry!", Family View blocks, and Rate Limit warnings, converting ambiguous HTML into typed, actionable Go errors.
* **Type-Safe Data Sanitization**: Steam's JSON is a mess of mixed types (strings-as-ints, ints-as-bools). G-man centralizes this "dirty work" in the `rest` package. By the time data reaches your logic, it is strictly typed and validated. No more `strconv` boilerplate or runtime panics.
* **Modular "Auth-Aware" Architecture**: Build your bot like a puzzle. Decoupled modules for **Chat, Friends, Inventory, and GC** automatically wake up and receive fresh security contexts the moment a login succeeds or a token is refreshed.
* **Game Coordinator (GC) Multiplexer**: First-class, multiplexed support for TF2, CS2, and Dota 2. Includes native job tracking, automatic GZIP decompression of multi-messages, and protection against "Zip Bomb" attacks.
* **The "Onion" Trading Engine**: A sophisticated middleware pipeline for trade offers. Process trades through a chain of modular processors: `Deduplicator` → `PriceValidator` → `SecurityEscrowCheck` → `AutoAccepter`. Highly extensible and easy to audit.

## 🎮 Game Ecosystem & Coordinator Support

G-man is not just a Steam parser; it is a **multi-game bot framework** with dedicated support for Steam Game Coordinators (GC). It includes high-level domain modules, item schema normalization, and automated trading logic tailored for major games:

### 🎒 Team Fortress 2 (TF2) — Fully Production-Ready
G-man comes with a complete, production-grade TF2 trading suite built out of the box:
* **Autopricer & PriceDB:** Stateful price manager with real-time Socket.IO price updates, seeding inventory data, and local cache synchronization.
* **Competitor Undercutting:** Automated classifieds analysis querying backpack.tf's active snapshot APIs, automatically outpricing competitors while respecting user-configured price floors/ceilings.
* **Stock & Inventory Control:** Built-in limits to prevent inventory overflow, automated stock balancing, and smart pure-metal liquidators (smelting/combining).
* **Crafting & Achievements:** Autonomous TF2 item crafting (mass smelting weapons to scrap/refined) and a human-like achievement unlock simulator to mimic legitimate players.

## 📂 Project Layout

```text
pkg/
├── steam/            # Core Steam Protocol & Session Management
│   ├── auth/         # OAuth2 authentication, persistent session refreshes
│   ├── socket/       # Low-level Connection Manager (CM) client, GZIP heartbeats
│   ├── protocol/     # Steam wire-format, pre-compiled protobufs & language specs
│   ├── transport/    # Agnostic routing layer (Dual-Stack HTTP/Socket engine)
│   ├── social/       # Real-time chat, friends tracking, and relationship lists
│   ├── community/    # Defensive scraping: Market, Inventories, Steam API keys
│   └── sys/          # App management, Game Coordinator (GC) dispatcher
├── tf2/              # Production TF2 Trading & Item Domain Modules
│   ├── schema/       # Item schema parser, SKU parser, and attribute normalization
│   ├── currency/     # High-level TF2 metal math (Key, Refined, Scrap)
│   ├── backpack/     # Unified GC-Web inventory cache & automated item syncer
│   ├── pricedb/      # Pluggable TF2 pricing system & real-time Socket.IO updater
│   ├── bptf/         # Stateful backpack.tf Listing Manager (listings/snapshot APIs)
│   └── behavior/     # Autonomous stock balancing, competitive undercutting & crafting
├── behavior/         # General Bot Behaviors & Human Mimicry
│   └── achievements/ # Achievement unlocked simulator imitating legitimate gameplay
├── trading/          # Core Trading & Middleware Pipeline
│   └── processors/   # Onion middleware: deduplication, security escrows, auto-accept
├── protobuf/         # Compiled .pb.go protobuf messages for all Valve games
├── bus/              # Thread-safe event bus for fast inter-module message routing
└── rest/             # High-performance REST wrapper with unified type sanitization
```

## 🚀 Quick Start

Initialize the orchestrator and let G-man handle the complexities of the Steam session lifecycle.

```go
func main() {
    // Configure the basics
    cfg := steam.DefaultConfig()
    cfg.Storage = memory.New()
    logger := log.New(log.DefaultConfig(log.InfoLevel))
    
    // Initialize the Orchestrator
    client := steam.NewClient(cfg,
        steam.WithLogger(logger),
        chat.WithModule(),    // Plug in social features
        friends.WithModule(), // Sync friends list automatically
    )
    defer client.Close()

    // Listen for events globally
    go func() {
        sub := client.Bus().Subscribe(&chat.MessageEvent{})
        for event := range sub.C() {
            msg := event.(*chat.MessageEvent)
            fmt.Printf("Message from %d: %s\n", msg.SenderID, msg.Message)
        }
    }()

    // One-call connection and login
    // Handles: TCP Connect -> CM Handshake -> Auth -> WebSession -> API Key Sync
    err := client.ConnectAndLogin(context.Background(), server, &auth.LogOnDetails{
        AccountName:  "GordonF",
        RefreshToken: "your_encrypted_refresh_token",
    })
    
    if err != nil {
        logger.Error("Failed to connect and login", logger.Err(err))
        panic(err)
    }

    client.Wait() // Block until shutdown
}
```

## 🛠 Developer Tooling

G-man is built to stay up-to-date. We provide internal CLI generators for:

* **WebAPI**: Automatically syncs with Valve's latest GetSupportedAPIList.
* **Protobufs**: Sanitizes and compiles raw SteamRE definitions for Go.
* **SteamLanguage**: Generates type-safe Enums and Stringer implementations from .steamd files.

## 🏗 Roadmap

### Core Systems

* [x] **Smart Transport:** Automatic routing between Socket and WebAPI.
* [x] **WebSession Heartbeat:** Background worker to keep cookies and API keys alive.
* [x] **Persistent Auth:** Automatic re-login using encrypted Refresh Tokens.
* [x] **Proxy Support:** Integrated SOCKS5/HTTP tunneling for all outbound traffic.
* [ ] **Steam CDN Support:** Logic for manifest parsing and downloading app metadata/item assets.

### TF2 Specifics

* [x] **Inventory Manager:** Unified view of Web and GC inventories.
* [x] **Currency (Metal) Manager:** High-level smelting and metal stock balancing.
* [x] **SKU System:** Advanced parser for TF2 item identifiers.
* [x] **PriceDB:** Pluggable pricing providers (Backpack.tf).

### Trading Engine

* [x] **Trade Middleware:** Chain-based offer processing.
* [x] **Live Trading:** Real-time trade window interaction via GC.
* [x] **Inventory Manager:** High-level abstractions for item moving and multi-context sync.

### Game Domains

* [x] **TF2 Crafting:** High-level API for mass-smelting and weapon crafting.
* [ ] **CS2 Support:** Game Coordinator implementation for inventory and match data.
* [ ] **Dota 2 Support:** GC implementation for item management and lobby control.

### Trading Excellence (The "Autobot" Phase)

* [x] **BPTF Listing Manager:** High-level API for creating, updating, and mass-deleting listings.
* [x] **TF2 Price Sync:** Automated background worker to fetch prices and update the internal price database.
* [x] **Stock Control:** Implementation of buy/sell limits and automated stock balancing.
* [x] **Pure Liquidator:** Automatic metal smelting/combining integrated with the trade flow.

## ☕ Support the Development

Developing a full-scale Steam SDK takes hundreds of hours and... a considerable amount of effort. If G-MAN has helped you build your trading empire or saved you from the Node.js event-loop nightmare, consider supporting the project.

<div align="center">

[![Trade Offer](https://img.shields.io/badge/Steam-Trade_Offer-blue?style=for-the-badge&logo=steam)](https://steamcommunity.com/tradeoffer/new/?partner=1141078357&token=HjsTJQFX)

> *"Donations... are not a requirement, but... they fulfill the terms of our... agreement."*

</div>

## 🤝 Contributing

G-man is an open-source project. We welcome contributions for new game coordinators (CS2/Dota 2) and storage providers. See [CONTRIBUTING.md](CONTRIBUTING.md) for details.

## License

This project is **not** affiliated with, maintained by, or endorsed by **Valve Corporation** or any of its subsidiaries. Steam, the Steam logo, and all related Valve properties are trademarks of Valve Corporation.

Use of this SDK is at your own risk. G-MAN is not responsible for issues with your account, including, but not limited to, account suspensions, trade hold delays, or market fluctuations.

Distributed under the **BSD-3-Clause** License. See `LICENSE` for more information.

---

<div align="center">
  <sub>Prepare for unforeseen consequences... or just prepare for the next Steam Sale.</sub>
</div>
