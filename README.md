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

We prioritize **Type Safety**, **Concurrency**, and **Modularity**. Whether you are managing a single inventory or orchestrating a farm of thousands, G-man provides the rock-solid foundation you need.

> [!NOTE]  
> This SDK is currently in the early development. Any breaking changes can be made and the the stable work is not guaranteed.

## Key Features

* **Architected for Speed:** Native Go concurrency allows you to handle thousands of events and trade offers without blocking the event loop.
* **Transport Agnostic:** A unique `Transport` layer seamlessly switches between **TCP Sockets** and **HTTP WebAPI**. Bypass rate limits by sending API requests directly through the CM socket.
* **Truly Modular (SOLID):** Need a simple chat bot? Don't load the trading module. Need TF2 logic? Plug it in. Everything is decoupled via a high-performance **Event Bus**.
* **Modern Authentication:** Full support for the new Steam JWT-based authentication flow, session refreshment, and QR login.
* **Game Coordinator Native:** First-class support for GC interactions (TF2, CS2, Dota 2) with automatic protobuf handling and job management.
* **Smart Guard:** Integrated mobile confirmation handling with automatic rate-limiting and exponential backoff.

## 📦 Installation

```bash
go get github.com/lemon4ksan/g-man
```

## 🚀 Quick Start

G-man uses a composite architecture. You create a Client and plug in the modules you need.

```go
package main

import (
    "context"
    "github.com/lemon4ksan/g-man/log"
    "github.com/lemon4ksan/g-man/steam"
    "github.com/lemon4ksan/g-man/modules/apps"
    "github.com/lemon4ksan/g-man/modules/tf2"
)

func main() {
    // Initialize the Core
    client := steam.NewClient(steam.DefaultConfig(), steam.WithLogger(log.Default()))

    // Plug in Modules
    tf2Mod := tf2.New(client.Logger())
    appsMod := apps.New(client.Logger())

    client.AddModule(tf2Mod)
    client.AddModule(appsMod)

    // Login
    err := client.ConnectAndLogin(context.Background(), server, &auth.LogOnDetails{
        AccountName: "gman_trader",
        Password:    "password",
    })
    if err != nil {
        panic(err)
    }

    // Listen for Events
    sub := client.Bus().Subscribe(&tf2.CraftResponseEvent{})
    go func() {
        for ev := range sub.C() {
            event := ev.(*tf2.CraftResponseEvent)
            log.Info("Crafted items!", "count", len(event.ItemsCreated))
        }
    }()

    // Launch Game & Connect to GC
    appsMod.PlayGames(context.Background(), []uint32{440}, true)
    tf2Mod.PlayGame(context.Background())
    client.Wait()
}
```

## 🧩 Ecosystem

G-man is split into independent packages to keep your binary size small and your logic clean:

| Package | Description |
| :--- | :--- |
| **`steam`** | The core orchestrator and connection manager. |
| **`auth`** | Handles login, encryption, and WebSession management. |
| **`trading`** | Trade Offer polling, asset caching, and live trades. |
| **`guard`** | Mobile 2FA code generation and confirmation acceptance. |
| **`coordinator`** | Low-level packet routing for Game Coordinators. |
| **`games`** | Game-specific implementations (TF2, CS2). |

## License

Distributed under the **BSD-3-Clause** License. See `LICENSE` for more information.

---

<div align="center">
  <sub>Designed for performance. Engineered for profit.</sub>
</div>
