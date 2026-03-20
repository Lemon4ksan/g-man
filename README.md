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
* **BaseModule Pattern:** Standardized lifecycle for all modules (`Init`, `StartAuthed`, `Close`). Built-in support for scoped logging, event bus integration, and safe goroutine management.
* **Deep Error Scraping:** Specialized `community` client that detects "soft errors" (Family View, Login redirects, Steam Maintenance) hidden inside HTML responses.
* **Modern Authentication:** Full support for JWT-based login, automatic `WebSession` establishment, and OIDC cookie transfers.
* **Game Coordinator Native:** First-class support for GC interactions (TF2, CS2, DOTA2) with automatic job tracking and Protobuf unmarshaling.

## 📦 Installation

```bash
go get github.com/lemon4ksan/g-man@latest
```

## 🚀 Quick Start

G-man uses a declarative configuration. You define the modules you need in a single config struct, and the client handles the rest.

```go
package main

import (
    "context"
    "time"

    "github.com/lemon4ksan/g-man/pkg/log"
    "github.com/lemon4ksan/g-man/pkg/modules/apps"
    "github.com/lemon4ksan/g-man/pkg/modules/auth"
    "github.com/lemon4ksan/g-man/pkg/modules/guard"
    "github.com/lemon4ksan/g-man/pkg/modules/trading"
    "github.com/lemon4ksan/g-man/pkg/steam"
)

func main() {
    logger := log.New(log.DefaultConfig(log.InfoLevel))

    // 1. Configure the client and its modules
    cfg := steam.Config{
        Guard: &guard.Config{
            IdentitySecret: "base64_secret",
            DeviceID:       "android:uuid",
            AutoAccept:     true,
        },
            Trading: &trading.Config{
            PollInterval: 15 * time.Second,
        },
    }

    // 2. Initialize the Client
    client := steam.NewClient(cfg, steam.WithLogger(logger))

    // 3. Subscribe to events via the global Bus
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

    // 4. Connect and Login
    err := client.ConnectAndLogin(context.Background(), nil, steam.LogOnDetails{
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

* [ ] **Proxy Support:** Integrated SOCKS5/HTTP tunnel support for both Socket and WebAPI.
* [ ] **Persistence Layer:** Pluggable storage drivers (Redis, PostgreSQL, SQLite) for session and state persistence.
* [ ] **Steam CDN Support:** Logic for manifest parsing and downloading app metadata/item assets directly from Valve's content servers.
* [ ] **WebSession Auto-Refresh:** Background worker to keep cookies alive via periodic "heartbeat" visits to lightweight Steam pages.

### Economy & Trading

* [ ] **Trade Middleware Engine:** A "Chain of Responsibility" for incoming trades (e.g., `Blacklist` -> `Pricer` -> `EscrowCheck`).
* [ ] **Inventory Manager:** High-level abstractions for multi-context inventory synchronization and item moving.
* [ ] **PriceDB Integration:** Generic interface for price providers (Backpack.tf, PriceDB, custom APIs).

### Game Domains

* [ ] **CS2 Support:** Full Game Coordinator implementation, including inventory and match data.
* [ ] **Dota 2 Support:** GC implementation for item management and lobby control.
* [ ] **TF2 Crafting:** High-level API for mass-smelting and weapon crafting.

## 🤝 Contributing

G-man is an open-source project. We welcome contributions in the form of bug reports, feature requests, or pull requests. Please see our [Contributing Guide](CONTRIBUTING.md) for more details.

## License

Distributed under the **BSD-3-Clause** License. See `LICENSE` for more information.

---

<div align="center">
  <sub>Designed for performance. Engineered for profit.</sub>
</div>
