<div align="center">

# G-MAN

### The Ultimate Steam Bot Framework for Go

[![Go Reference](https://pkg.go.dev/badge/github.com/lemon4ksan/g-man.svg)](https://pkg.go.dev/github.com/lemon4ksan/g-man)
[![Go Report Card](https://goreportcard.com/badge/github.com/lemon4ksan/g-man)](https://goreportcard.com/report/github.com/lemon4ksan/g-man)
[![License](https://img.shields.io/github/license/lemon4ksan/g-man)](LICENSE)

> *"The right bot in the wrong place can make all the difference in the skins market."*

</div>

---

**G-man** is a high-performance Steam client library architected for high-frequency trading, industrial-scale automation, and resilient network operations. Unlike legacy wrappers, G-man treats the Steam Network as a unified entity, seamlessly blending **Socket (CM)** and **WebAPI** protocols into a single, thread-safe orchestrator.

## ⚡ Key Features

* **Self-Healing Sessions (Silent Re-auth)**: Eliminate the #1 cause of bot downtime. G-man monitors session health in real-time. If an Access Token or Web Cookie expires mid-request, the orchestrator automatically pauses, performs a background OAuth2 refresh, and retries the operation transparently. Your business logic never sees a "401 Unauthorized."
* **Dual-Stack Transport Engine**: Stop worrying about whether to use WebAPI or Sockets. G-man features a protocol-agnostic routing layer. It automatically selects the most efficient path - **TCP/WebSocket** for speed and real-time state, or **HTTPS** for stealth and reliability - switching between them seamlessly if one becomes unstable.
* **True Concurrency**: Escape the "Node.js Event-Loop bottleneck." Built on Go's CSP model, G-man is designed to manage hundreds of accounts and thousands of concurrent trade offers within a single process. High-frequency trade floods are handled via thread-safe worker pools, not single-threaded queues.
* **Deep Defensive Scraping**: Steam's "Soft Errors" are the silent killers of automation. G-man's `community` engine doesn't just check HTTP codes; it proactively scrapes response bodies for "Sorry!", Family View blocks, and Rate Limit warnings, converting ambiguous HTML into typed, actionable Go errors.
* **Type-Safe Data Sanitization**: Steam's JSON is a mess of mixed types (strings-as-ints, ints-as-bools). G-man centralizes this "dirty work" in the `rest` package. By the time data reaches your logic, it is strictly typed and validated. No more `strconv` boilerplate or runtime panics.
* **Modular "Auth-Aware" Architecture**: Build your bot like a puzzle. Modules for **Chat, Friends, Inventory, and GC** are decoupled from the core but "Auth-Aware." They automatically wake up and receive fresh security contexts the moment a login succeeds or a token is refreshed.
* **Game Coordinator (GC) Multiplexer**: First-class, multiplexed support for TF2, CS2, and Dota 2. Includes native job tracking, automatic GZIP decompression of multi-messages, and protection against "Zip Bomb" attacks.
* **The "Onion" Trading Engine**: A sophisticated middleware pipeline for trade offers. Process trades through a chain of modular processors: `Deduplicator` → `PriceValidator` → `SecurityEscrowCheck` → `AutoAccepter`. Highly extensible and easy to audit.

## 📂 Project Layout

```text
pkg/
├── steam/          
│   ├── auth/       # OAuth2 flow, Refresh/Access token management
│   ├── socket/     # Low-level CM connection, GZIP multi-messages, heartbeats
│   ├── protocol/   # Steam wire-format, headers, and Enum definitions
│   ├── transport/  # Unification layer for HTTP and Socket calls
│   ├── social/     # Chat, Friends list, Persona state tracking
│   ├── sys/        # Apps management, GC Coordinator, CM Directory
│   ├── community/  # Web-based interaction (Market, Inventory, API Keys)
├── trading/        # Trade offer middleware and "Onion" processing engine
├── protobuf/       # Pre-compiled .pb.go files for Steam and all major games
├── rest /          # Lightweight, generic wrapper around net/http
└── bus/            # High-performance internal event system
```

## 🚀 Quick Start

Initialize the orchestrator and let G-man handle the complexities of the Steam session lifecycle.

```go
func main() {
    // Configure the basics
    cfg := steam.DefaultConfig()
    cfg.Storage = memory.New()
    
    // Initialize the Orchestrator
    client := steam.NewClient(cfg,
        steam.WithLogger(log.New(log.LevelInfo)),
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
* [ ] **Database Drivers:** Official support for SQLite (bbolt/sql) and PostgreSQL.
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

* [ ] **BPTF Listing Manager:** High-level API for creating, updating, and mass-deleting listings.
* [ ] **BPTF Price Sync:** Automated background worker to fetch prices and update the internal price database.
* [ ] **Stock Control:** Implementation of buy/sell limits and automated stock balancing.
* [ ] **Pure Liquidator:** Automatic metal smelting/combining integrated with the trade flow.

### Industrial Scale & Ops

* [ ] **Prometheus Metrics:** Export trade statistics, profit, and latency data.
* [ ] **Advanced Proxy Rotation:** Ability to bind different bots to different local IPs/proxies within one process.
* [ ] **Web Dashboard:** A lightweight embedded UI to monitor bot health and manual offer review.
* [ ] **Account Manager (Orchestrator+)**: A high-level manager for running 100+ instances of steam.Client with shared rate-limiters and proxy rotation.

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
