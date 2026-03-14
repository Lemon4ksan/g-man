<div align="center">

# G-MAN

### The Ultimate Steam Bot Framework for Go

[![Go Reference](https://pkg.go.dev/badge/github.com/lemon4ksan/g-man.svg)](https://pkg.go.dev/github.com/lemon4ksan/g-man)
[![Go Report Card](https://goreportcard.com/badge/github.com/lemon4ksan/g-man)](https://goreportcard.com/report/github.com/lemon4ksan/g-man)
[![License](https://img.shields.io/github/license/lemon4ksan/g-man)](LICENSE)

> *"The right bot in the right place can make all the difference in the skins market."*

</div>

---

**G-man** is a next-generation Steam client library architected specifically for high-frequency trading and heavy automation. Built on **Go**, it leaves the limitations of single-threaded Node.js wrappers behind.

It prioritizes **Type Safety**, **Concurrency**, and **Modularity**. Whether you are managing a single inventory or orchestrating a farm of thousands, G-man provides the rock-solid foundation you need.

> [!NOTE]
> This SDK is currently in the early development. The API is not yet stable, and breaking changes may occur. Production use is at your own risk.

## Key Features

* **Architected for Speed:** Native Go concurrency allows you to handle thousands of events and trade offers simultaneously without blocking a central event loop.
* **Transport Agnostic:** A unique `Transport` layer seamlessly switches between **TCP Sockets** and **HTTP WebAPI**. Bypass rate limits by sending API requests directly through the authenticated CM socket.
* **Truly Modular (SOLID):** Need a simple chat bot? Don't load the trading module. Need TF2 logic? Plug it in. Everything is decoupled via a high-performance **Event Bus**.
* **Modern Authentication:** Full support for the new Steam JWT-based authentication flow, `WebSession` management, and session refreshment.
* **Game Coordinator Native:** First-class support for GC interactions (TF2, CS2, DOTA2) with automatic protobuf handling and job management.
* **Smart Guard:** Integrated mobile confirmation handling with automatic rate-limiting and exponential backoff to prevent API bans.

## 📦 Installation

```bash
go get github.com/lemon4ksan/g-man@latest
```

## 🚀 Quick Start

G-man uses a composite architecture. You create a `Client` and plug in the modules you need during initialization.

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/lemon4ksan/g-man/pkg/log"
    "github.com/lemon4ksan/g-man/pkg/modules/apps"
    "github.com/lemon4ksan/g-man/pkg/modules/auth"
    "github.com/lemon4ksan/g-man/pkg/modules/directory"
    "github.com/lemon4ksan/g-man/pkg/modules/tf2"
    "github.com/lemon4ksan/g-man/pkg/steam"
    "github.com/lemon4ksan/g-man/pkg/steam/socket"
)

func main() {
    // For this example, we'll use an environment variable for the password.
    password := os.Getenv("STEAM_PASSWORD")
    if password == "" {
        fmt.Println("STEAM_PASSWORD environment variable not set.")
        return
    }

    // Initialize a logger and desired modules.
    logger := log.New(log.DefaultConfig(log.InfoLevel))
    appsMod := apps.New()
    tf2Mod := tf2.New(logger)

    // Create the core client, providing modules as functional options.
    client := steam.NewClient(
        steam.DefaultConfig(),
        steam.WithLogger(logger),
        steam.WithModule(appsMod),
        steam.WithModule(tf2Mod),
    )

    // Listen for the "LoggedOnEvent" to know when we can perform actions.
    sub := client.Bus().Subscribe(&auth.LoggedOnEvent{}, &tf2.BackpackLoadedEvent{})
    go func() {
        for event := range sub.C() {
            switch ev := event.(type) {
            case *auth.LoggedOnEvent:
                logger.Info("Successfully logged on!", log.SteamID("steamID", ev.SteamID))
                // Now that we're logged in, we can launch TF2 to connect to its GC.
                appsMod.PlayGames(context.Background(), []uint32{tf2.AppID}, false)

            case *tf2.BackpackLoadedEvent:
                logger.Info("TF2 Backpack loaded!", log.Int("item_count", ev.Count))
            }
        }
    }()

    server, err := directory.GetOptimalCMServer(context.Background())
    if err != nil {
        logger.Error("Failed to get cm server", log.Err(err))
        return
    }
    
    err := client.ConnectAndLogin(context.Background(), server, &auth.LogOnDetails{
        AccountName: "your_steam_username",
        Password:    password,
    })
    if err != nil {
        logger.Error("Failed to login", log.Err(err))
        return
    }

    // Wait for the client to be closed (e.g., by CTRL+C).
    client.Wait()
    logger.Info("Client has been shut down.")
}
```

## 🧰 Packages

The pkg directory contains independent packages that can be used to bring any idea to life. From a community market parser and item auto smelter to full-fledged trading bots.

## 🚧 What's Next? (Roadmap)

While the foundation is strong, several key features are planned for future releases:

* **Bot Abstraction:** A universal bot client with plugin support for reducing low level code.
* **Trade Middleware Engine:** Possibility to attach chains of checks to incoming/outgoing trades.
* **WebSession Auto-Refresh:** A background worker that periodically visits steamcommunity.com/chat or other lightweight pages to keep the session active.
* **Generic Pricer Interface**: An abstract Pricer interface that can have implementations for backpack.tf, pricedb.io, or a custom API.
* **Idempotency Support for Trades:** The logic for checking the offer status before retrying to avoid accepting an already accepted or cancelled trade.
* **Persistence Layer:** An interface for database integration (e.g., SQLite, Redis) to persist session data and state across restarts.
* **Proxy Support:** Integrated SOCKS5/HTTP proxy support for both WebAPI and CM connections.
* **Detailed Documentation:** A more descriptive documentation for public methods.
* **CS2, DOTA2 support:** Currently only TF2 game coordinator is implemented.

## License

Distributed under the **BSD-3-Clause** License. See `LICENSE` for more information.

---

<div align="center">
  <sub>Designed for performance. Engineered for profit.</sub>
</div>
