<div align="center">

<img src="assets/logo.png" alt="G-MAN Logo"/>

[![Go Reference](https://img.shields.io/badge/go-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/lemon4ksan/g-man)
[![Coverage](https://img.shields.io/badge/coverage-94.5%25-brightgreen?style=flat-square&logo=go&logoColor=white)](https://github.com/lemon4ksan/g-man)
[![License](https://img.shields.io/github/license/lemon4ksan/g-man?style=flat-square)](LICENSE)
[![Linter](https://img.shields.io/badge/linter-golangci--lint-brightgreen?style=flat-square&logo=go)](https://github.com/golangci/golangci-lint)

> _"The right bot in the wrong place can make all the difference in the skins market."_

#### 🇺🇸 [English](README.md) • 🇷🇺 [Русский](README_RU.md)

</div>

**G-man** is a high-performance, enterprise-grade Steam client SDK and multi-game automation framework architected in Go. Built for high-frequency trading, industrial-scale inventory management, and ultra-resilient network operations, G-man bridges the Steam Network and Game Coordinators into a single, thread-safe orchestrator. It seamlessly integrates **Socket (CM)**, **WebAPI**, and **Game Coordinator** protocols to keep your automation pipelines live 24/7.

```shell
go get github.com/lemon4ksan/g-man
```

## ⚡ Key Features

* **Self-Healing Sessions (Silent Re-auth):** Monitors Web sessions and access tokens in the background. If a web cookie expires, the orchestrator pauses active requests, performs an OAuth2 refresh, updates storage, and resumes operations transparently.
* **Dual-Stack Transport Engine:** Dynamically selects the optimal path: TCP/WebSocket CM channels for low-latency state synchronization, or HTTPS WebAPI for high-volume transactions, falling back to HTTP if a socket connection is interrupted.
* **"Onion" Trade Middleware Pipeline:** Encourages decoupled trading logic. Process incoming trade offers through an extensible chain: `Deduplicator` $\rightarrow$ `SecurityEscrowCheck` $\rightarrow$ `BlacklistFilter` $\rightarrow$ `PriceValidator` $\rightarrow$ `Verdict`.
* **Defensive Web Scraping:** Parses standard HTML pages returning `200 OK` status codes for hidden "Soft Errors" (e.g., rate-limit messages, Family View locks, login prompts) and converts them into strictly typed Go errors.
* **Robust Dependency Management:** Uses a Depth-First Search (DFS) algorithm during boot to topologically sort and initialize modules, failing fast on circular dependencies.

## ⚔️ Go vs. JavaScript: Why G-man Wins

Historically, Node.js (with libraries like `node-steam-user`) has been the default choice for Steam automation. However, when scaling to enterprise or production grade operations, JavaScript's single-threaded nature and runtime characteristics introduce heavy bottlenecks:

| Feature | 🤖 G-man (Go) | 📦 Node.js (JS/TS) | Why it matters |
| :--- | :--- | :--- | :--- |
| **Concurrency Model** | **CSP (Goroutines + Channels)** | Single-threaded Event Loop | Go handles thousands of concurrent sessions/accounts across all CPU cores without blocking. |
| **Idle Memory Footprint** | **~4.5 MB** per client | **~40 - 80 MB** per client | G-man allows running 10x-20x more bots on the same VPS, drastically reducing hosting costs. |
| **Garbage Collection** | **Low-latency (sub-millisecond)** | Generational (V8) | Eliminates random micro-stutters during heavy inventory parsing or high-frequency trading. |
| **Type Safety** | **Strict compile-time checking** | Dynamic / Runtime-only (TS compiles to JS) | Guarantees safety in critical economic/trading operations, eliminating `undefined` crashes. |
| **Deployment** | **Single static binary (~15MB)** | Large `node_modules` (+ Node runtime) | Simplifies deployment and reduces Docker image sizes from 300MB+ to 15MB. |

## 🔮 The Go Future: Why We Build on Go

Steam network protocols are evolving. Modern Game Coordinator handshakes (CS2, Dota 2) utilize Protobufs heavily, and Steam's API rate limits require robust connection multiplexing. G-man uses Go because it is built for the next decade of automation:

1. **Cloud-Native Architecture:** Go is the lingua franca of cloud engineering (Docker, Kubernetes, Prometheus). G-man exports native metrics effortlessly, allowing you to integrate bot fleets into modern Grafana dashboards.
2. **Zero-Dependency Resilience:** G-man compiles down to a single binary with zero external runtime dependencies. Say goodbye to `npm audit` warnings, breaking updates in nested NPM packages, and security vulnerabilities.
3. **Optimized Protobuf & Serialization:** Go's official protobuf implementation is highly optimized, allowing G-man to decode, process, and route binary network packets from Game Coordinators at blazing speeds.
4. **Resilient Network Primitives:** Implementing proxy-rotation, custom DNS resolution, and TCP/UDP dialers is simple and native in Go's standard `net` library, without relying on bloated wrapper packages.

## 🚀 Quick Start

### 1. Initialize the Core Client

```go
package main

import (
	"context"
	"os"

	"github.com/lemon4ksan/miyako/log"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/directory"
	"github.com/lemon4ksan/g-man/pkg/storage/jsonfile"
	webtrading "github.com/lemon4ksan/g-man/pkg/trading/web"
)

func main() {
	// 1. Initialize persistent JSON storage for sessions
	store, _ := jsonfile.New("storage.json")
	logger := log.New(log.DefaultConfig(log.LevelInfo))

	// 2. Instantiate the orchestrator with required modules
	client, _ := steam.NewClient(steam.DefaultConfig(),
		steam.WithLogger(logger),
		steam.WithStorage(store),
		webtrading.WithModule(webtrading.DefaultConfig()),
	)
	defer client.Close()

	if err := client.Run(); err != nil {
		panic(err)
	}

	// 3. Resolve CM server and login
	dir := directory.New(client)
	server, _ := dir.GetOptimalCMServer(context.Background())
	login := auth.NewLogOnDetails(os.Getenv("STEAM_USER"), os.Getenv("STEAM_PASS"))

	if err := client.ConnectAndLogin(context.Background(), server, login); err != nil {
		panic(err)
	}

	client.Wait()
}
```

### 2. Configure Custom Onion Middlewares

```go
package main

import (
	"github.com/lemon4ksan/g-man/pkg/trading/engine"
	"github.com/lemon4ksan/g-man/pkg/trading/reason"
)

func PriceValidationMiddleware(priceLimit int) engine.Middleware {
	return func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			totalGiveValue := 0
			for _, item := range ctx.Offer.ItemsToGive {
				if price, ok := ctx.Get("price_" + item.SKU); ok {
					totalGiveValue += price.(int)
				}
			}

			if totalGiveValue > priceLimit {
				ctx.Review(reason.ReviewEngineError)
				return nil // Halt chain safely
			}

			return next(ctx)
		}
	}
}
```

## 📦 Ecosystem & Extensions

G-man is designed to be highly modular. Game-specific modules and tooling are decoupled into dedicated repositories:

* **[g-man-tf2](https://github.com/lemon4ksan/g-man-tf2)**: Team Fortress 2 Trading & Economy Suite.
  - **Metal Arithmetic:** Calculations with keys, refined, reclaimed, and scrap metals.
  - **Auto-Smelter:** Automatically handles weapon combining and metal smelting to balance change.
  - **Backpack.tf Sync:** Listing publisher and competitor undercutting manager.
* **[g-man-cli](https://github.com/lemon4ksan/g-man-cli)**: Background system daemon (`g-mand`) and color-coded CLI client (`gmanctl`) for control, monitoring, and inventory management over secure gRPC.
* **[node-gman](https://github.com/lemon4ksan/node-gman)**: TypeScript/Node.js SDK for `g-mand` providing compatibility wrappers (`SteamUserAdapter`, `TradeOfferManagerAdapter`) to easily migrate or bridge existing NodeJS code to the Go backend.

## 🚀 Memory & Performance

* **Small Heap Footprint:** The core bot architecture requires approximately **~4.5 MB** of live heap memory in idle state (including the event bus, social tracking, and trade managers).
* **Buffer Pooling:** Network serialization and socket frames use buffer pools to minimize garbage collection cycles during high-throughput tasks.

## 🏗 Roadmap

- [x] **Smart Transport Routing:** Thread-safe dynamic requests via Sockets or HTTP.
- [x] **WebSession Keep-Alive:** Auto-refresh loops for web-cookies and API keys.
- [x] **Silent Re-Authentication:** Background recovery of expired JWTs.
- [x] **Topological Dependency Sorting:** Safe, cycle-free module boot ordering.
- [ ] **Steam CDN Downloader:** Dynamic downloading and parsing of app manifests/game assets.
- [ ] **CS2 Coordinator:** GC-handshakes, item skin parsing, and match history tracking.
- [ ] **Dota 2 Coordinator:** SOCache item parsing and custom lobby manager.

## 🤝 Contributing

We welcome contributions to G-man! Before you make a commit make sure to:

1. Review our design philosophy in [CONTRIBUTING.md](CONTRIBUTING.md).
2. Ensure network interactions route through the core `transport.Doer` interface to support mocking.
3. Verify concurrency safety using `go test -race ./...`.

## ☕ Support the Development

Building a industrial-scale Steam SDK takes hundreds of hours of protocol reverse-engineering. If G-man helped you automate your trading workflows or optimized your server resources, feel free to show some support:

<div align="center">

[![Trade Offer](https://img.shields.io/badge/Steam-Trade_Offer-blue?style=for-the-badge&logo=steam)](https://steamcommunity.com/tradeoffer/new/?partner=1141078357&token=HjsTJQFX)

> _"Donations... are not a requirement, but... they fulfill the terms of our... agreement."_

</div>

## ⚖️ Legal & License

**Disclaimer:** This software is **not** affiliated with, maintained by, or endorsed by **Valve Corporation** or any of its subsidiaries. Steam, Team Fortress 2, and all related Valve properties are registered trademarks of Valve Corporation. Use of this library is at your own risk.

This project is licensed under the **BSD 3-Clause License**. See [LICENSE](LICENSE) for full details.

---

<div align="center">
  <sub>Prepare for unforeseen consequences... or just prepare for the next Steam Sale.</sub>
</div>
