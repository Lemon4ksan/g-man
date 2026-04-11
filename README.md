<div align="center">

# G-MAN

### The Ultimate Steam Bot Framework for Go

[![Go Reference](https://pkg.go.dev/badge/github.com/lemon4ksan/g-man.svg)](https://pkg.go.dev/github.com/lemon4ksan/g-man)
[![Go Report Card](https://goreportcard.com/badge/github.com/lemon4ksan/g-man)](https://goreportcard.com/report/github.com/lemon4ksan/g-man)
[![License](https://img.shields.io/github/license/lemon4ksan/g-man)](LICENSE)

> *"The right bot in the wrong place can make all the difference in the skins market."*

</div>

---

**G-man** is a high-performance Steam client library architected for high-frequency trading, complex inventory management, and industrial-scale automation. Written in pure **Go**, it bypasses the limitations of single-threaded environments, offering a thread-safe, modular, and type-safe foundation for modern Steam development.

> [!WARNING]
> This SDK is currently in **early development (Alpha)**. Breaking changes are expected. The API is evolving rapidly and is being tested. Production use is at your own risk.

## ⚡ Key Features

* **True Concurrency**: Unlike single-threaded Node.js libraries, G-man leverages Go's scheduler. Handle 1,000+ accounts or high-frequency trade floods without event-loop lag.
* **Universal Transport Engine:** A protocol-agnostic layer that unifies **TCP/WebSockets** and **HTTP WebAPI**. Call Unified Services through the most efficient route automatically.
* **Game Coordinator (GC) Native:** First-class support for TF2, CS2, and Dota 2. Includes job tracking, SOCache (Shared Objects) management, and item-schema parsing.
* **Stateful Orchestrator:** A centralized `steam.Client` that manages connection lifecycles, background heartbeats, and automatic WebSession/APIKey acquisition.
* **Trading Middleware:** An "Onion" style engine for processing trade offers. Pipeline your logic through custom processors: `Deduplicator` → `Pricer` → `SecurityCheck` → `Review`.
* **Deep Error Scraping:** The `community` client detects "soft errors" (Family View, Maintenance, Login Redirects) hidden inside HTML responses, returning typed Go errors.
* **Pluggable Persistence:** Native support for **Memory**, **JSON**, and **SQLite** storage for sessions, authentication tokens, and price databases.
* **Extensive Protobuf Support:** Pre-generated definitions for Steam, TF2, CS2, Dota 2, Deadlock, and WebUI.
* **Type Safety**: No more `undefined is not a function` in the middle of a $500 trade. Full Protobuf integration ensures your data is valid at compile time.
* **Binary Efficiency**: Zero-dependency, static binaries. Deploy your bot to a tiny Raspberry Pi with minimal footprint.

## 📂 Project Layout

```text
pkg/
├── steam/          # Core: socket, auth, community, transport, unified services
│   ├── sys/        # System: apps, game coordinator (gc), directory (CM list)
│   └── social/     # Communication: chat, friends list
├── trading/        # Business logic: trade engine, notifications, review system
├── tf2/            # Game-specific: inventory, schema, currency, bptf, sku
├── protobuf/       # Generated .pb.go files for all Steam games
├── storage/        # Persistence: SQLite, JSON, Memory providers
└── bus/            # Internal event system (Event Bus)
```

## 🚀 Quick Start

G-man uses a centralized orchestrator. You initialize the client with standard dependencies, and it handles the internal wiring.

```go
package main

import (
    "context"
    "github.com/lemon4ksan/g-man/pkg/log"
    "github.com/lemon4ksan/g-man/pkg/steam"
    "github.com/lemon4ksan/g-man/pkg/steam/auth"
    "github.com/lemon4ksan/g-man/pkg/storage/memory"
    trading "github.com/lemon4ksan/g-man/pkg/steam/trading/web"
)

func main() {
    logger := log.New(log.DefaultConfig(log.InfoLevel))

    // Setup Orchestrator
    cfg := steam.DefaultConfig()
    cfg.Storage = memory.New()
    
    client := steam.NewClient(cfg,
        steam.WithLogger(logger),
        trading.WithModule(trading.DefaultConfig()),
    )

    // Subscribe to events via the global Event Bus
    sub := client.Bus().Subscribe(&auth.LoggedOnEvent{}, &trading.NewOfferEvent{})
    go func() {
        for event := range sub.C() {
            switch ev := event.(type) {
            case *auth.LoggedOnEvent:
                logger.Info("Logged in!", log.Uint64("steam_id", ev.SteamID))
            case *trading.NewOfferEvent:
                logger.Info("New trade offer!", log.Uint64("offer_id", ev.Offer.ID))
            }
        }
    }()
    
    // Get optimal server
    dir := directory.NewDirectoryService(client.Service())
    server, err := dir.GetOptimalCMServer(ctx)
    if err != nil {
        logger.Error("Failed to get CM server list", log.Err(err))
        return
    }

    details := &auth.LogOnDetails{
        AccountName: "your_username",
        Password:    "your_password",
    }
    
    // ConnectAndLogin handles: Socket Connection -> CM Handshake -> 
    // Auth Sequence -> WebSession Exchange -> API Key Registration
    if err := client.ConnectAndLogin(context.Background(), server, details); err != nil {
        logger.Fatal("Login failed", log.Err(err))
    }

    client.Wait()
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

### Game Specifics (TF2)

* [x] **Inventory Manager:** Unified view of Web and GC inventories.
* [x] **Currency (Metal) Manager:** High-level smelting and metal stock balancing.
* [x] **SKU System:** Advanced parser for TF2 item identifiers.
* [x] **PriceDB:** Pluggable pricing providers (Backpack.tf / Prices.tf).

### Trading Engine

* [x] **Trade Middleware:** Chain-based offer processing.
* [x] **Live Trading:** Real-time trade window interaction via GC.
* [ ] **Inventory Manager:** High-level abstractions for item moving and multi-context sync.

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

## ☕ Support the Development

Developing a full-scale Steam SDK takes hundreds of hours and... a considerable amount of effort. If G-MAN has helped you build your trading empire or saved you from the Node.js event-loop nightmare, consider supporting the project.

<div align="center">

[![Trade Offer](https://img.shields.io/badge/Steam-Trade_Offer-blue?style=for-the-badge&logo=steam)](https://steamcommunity.com/tradeoffer/new/?partner=1141078357&token=HjsTJQFX)

> *"Donations... are not a requirement, but... they fulfill the terms of our... agreement."*

</div>

## 🤝 Contributing

G-man is an open-source project. We welcome contributions for new game coordinators (CS2/Dota 2) and storage providers. See [CONTRIBUTING.md](CONTRIBUTING.md) for details.

## License

Distributed under the **BSD-3-Clause** License. See `LICENSE` for more information.

---

<div align="center">
  <sub>Prepare for unforeseen consequences... or just prepare for the next Steam Sale.</sub>
</div>
