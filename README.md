<div align="center">

# G-MAN

### The Ultimate Steam Bot Framework for Go

[![Go Reference](https://pkg.go.dev/badge/github.com/lemon4ksan/g-man.svg)](https://pkg.go.dev/github.com/lemon4ksan/g-man)
[![Go Report Card](https://goreportcard.com/badge/github.com/lemon4ksan/g-man)](https://goreportcard.com/report/github.com/lemon4ksan/g-man)
[![License](https://img.shields.io/github/license/lemon4ksan/g-man)](LICENSE)

> *"The right bot in the right place can make all the difference in the skins market."*

</div>

---

**G-man** is a next-generation Steam client library architected specifically for high-frequency trading, inventory management, and heavy automation. Built on **Go**, it leaves the limitations of single-threaded Node.js wrappers behind, offering unmatched performance for bot farms of any scale.

It prioritizes **Type Safety**, **Predictable Concurrency**, and **Modular Architecture**.

> [!WARNING]
> This SDK is currently in **early development (Alpha)**. The API is evolving rapidly. Breaking changes are expected. Production use is at your own risk.

## ⚡ Key Features

* **High-Concurrency Engine:** Native Go routines allow you to handle thousands of events, trade offers, and socket messages simultaneously without blocking.
* **Polymorphic Transport:** A unique layer that seamlessly switches between **TCP/WebSockets** and **HTTP WebAPI**. Bypass rate limits by routing API calls directly through the authenticated CM socket.
* **BaseModule Pattern:** Standardized lifecycle for all modules (`Init`, `StartAuthed`, `Close`). Includes built-in support for scoped logging, event bus integration, and safe goroutine management.
* **Persistence Layer:** Pluggable storage architecture. Automatically save and resume sessions (JWT Refresh Tokens) using Memory, SQLite, or PostgreSQL.
* **Trade Middleware Engine:** A powerful "Onion" style processing chain for incoming trades (e.g., `Deduplicator` -> `Blacklist` -> `Pricer` -> `EscrowCheck`).
* **Deep Error Scraping:** Specialized `community` client that detects "soft errors" (Family View, Login redirects, Steam Maintenance) hidden inside HTML responses.
* **Game Coordinator Native:** First-class support for GC interactions (TF2, CS2, DOTA2) with automatic job tracking, `SOCache` (Shared Objects) management, and Protobuf unmarshaling.

## 📦 Installation

```bash
go get github.com/lemon4ksan/g-man@latest
```

## 🚀 Quick Start

G-man uses a declarative configuration. You define the modules and storage you need in a single config struct, and the client handles the orchestration.

```go
package main

import (
    "context"
    "time"

    "github.com/lemon4ksan/g-man/pkg/log"
    "github.com/lemon4ksan/g-man/pkg/modules/auth"
    "github.com/lemon4ksan/g-man/pkg/modules/guard"
    "github.com/lemon4ksan/g-man/pkg/modules/trading"
    "github.com/lemon4ksan/g-man/pkg/steam"
    "github.com/lemon4ksan/g-man/pkg/storage/memory"
)

func main() {
    logger := log.New(log.DefaultConfig(log.InfoLevel))

    // Configure the client and its pluggable modules
    cfg := steam.DefaultConfig()
    cfg.Storage = memory.New() // Use SQLite or Postgres for production

    // Initialize the Client
    client := steam.NewClient(cfg,
        steam.WithLogger(logger),
        guard.WithModule(guard.Config{
            IdentitySecret: "base64_secret",
            DeviceID:       "android:uuid",
            AutoAccept:     true,
        }),
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

    // Connect and Login (will automatically use saved tokens if available)
    err := client.ConnectAndLogin(context.Background(), nil, &auth.LogOnDetails{
        AccountName: "username",
        Password:    "password",
    })
    if err != nil {
        panic(err)
    }

    client.Wait()
}
```

## 🏗 Roadmap

### Core & Protocol

* [x] **Persistence Layer:** Foundation interfaces and memory provider.
* [ ] **Database Drivers:** Official support for SQLite (bbolt/sql) and PostgreSQL.
* [ ] **Proxy Support:** Integrated SOCKS5/HTTP tunnel support for both Socket and WebAPI.
* [ ] **Steam CDN Support:** Logic for manifest parsing and downloading app metadata/item assets.
* [ ] **WebSession Auto-Refresh:** Background worker to keep cookies alive via periodic "heartbeat" visits.

### Economy & Trading

* [x] **Trade Middleware Engine:** "Chain of Responsibility" implementation.
* [x] **SOCache Manager:** Real-time inventory mirroring via Game Coordinator.
* [ ] **Inventory Manager:** High-level abstractions for item moving and multi-context sync.
* [ ] **PriceDB Integration:** Generic interface for price providers (Backpack.tf, Prices.tf).

### Game Domains

* [ ] **TF2 Crafting:** High-level API for mass-smelting and weapon crafting.
* [ ] **CS2 Support:** Game Coordinator implementation for inventory and match data.
* [ ] **Dota 2 Support:** GC implementation for item management and lobby control.

## 🤝 Contributing

G-man is an open-source project. We welcome contributions in the form of bug reports, feature requests, or pull requests. Please see our [Contributing Guide](CONTRIBUTING.md) for more details.

## License

Distributed under the **BSD-3-Clause** License. See `LICENSE` for more information.

---

<div align="center">
  <sub>Designed for performance. Engineered for profit.</sub>
</div>
