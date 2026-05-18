<div align="center">

# 🎒 Team Fortress 2 Domain Engine

### Production-Grade Economy Automation, Pricing, and GC Integration

#### 🇺🇸 [English](README.md) • 🇷🇺 [Русский](README_RU.md)

</div>

The `tf2` package is a high-performance, specialized domain engine built on top of the G-man framework. It is designed to run completely automated trading operations, handle floating-point-safe currency arithmetic, deeply integrate with the Team Fortress 2 Game Coordinator (GC), and orchestrate real-time `backpack.tf` market synchronization.

## ⚡ Key Features Deep Dive

### 🪙 1. Floating-Point Safe Currency Math (`tf2/currency`)
Trading in TF2 requires extremely precise math involving **Keys** and **Refined Metal (Scrap, Reclaimed, Refined)**. Standard `float64` math introduces precision rounding errors (e.g., `0.11 + 0.22 = 0.33000000000000007`), which leads to disastrous failed trades or incorrect change.

The `currency` package solves this by converting all fractional metal into base-level integer units (`Scrap`).
* **Example**: Adding `1.55` ref and `0.22` ref guarantees exactly `1.77` ref output. It safely handles automatic conversions to and from TF2 Keys based on real-time PriceDB rates.

### 🎒 2. Game Coordinator `SOCache` Synchronization (`tf2/backpack`)
Most primitive bots scrape the HTTP WebAPI to get a user's inventory. This is highly rate-limited and often heavily delayed.
G-man's TF2 client maintains an active **SOCache (Shared Object Cache)** directly inside the Game Coordinator's memory space. 
* Whenever an item is crafted, traded, or deleted, Valve pushes a binary delta-update to the socket. The `tf2.Client` automatically patches the local memory cache and fires a `BackpackLoadedEvent`, meaning you always have 0-latency access to the bot's true inventory state.

### 📈 3. PriceDB and Backpack.tf Autopricing (`tf2/bptf` & `tf2/pricedb`)
Instead of constantly spamming HTTP requests for prices, G-man separates pricing into a local, thread-safe `PriceDB` memory store.
* The `bptf` package connects via WebSockets (`Socket.IO`) to receive real-time streaming price changes from `backpack.tf`.
* When a price changes, it patches the local `PriceDB` cache, immediately making the new rate available to the `trading/engine` validators.

## 🚀 Quickstart Example

Here is how you launch the TF2 engine, calculate some precise change, and listen for instant inventory updates:

```go
package main

import (
	"context"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/tf2"
	"github.com/lemon4ksan/g-man/pkg/tf2/backpack"
	"github.com/lemon4ksan/g-man/pkg/tf2/currency"
)

func main() {
	ctx := context.Background()

	// 1. Initialize Structured Logging
	logger := log.New(log.DefaultConfig(log.LevelInfo))
	defer logger.Close()

	// 2. Initialize the Steam Client with TF2 and Backpack Modules
	cfg := steam.DefaultConfig()
	steamClient, err := steam.NewClient(cfg,
		steam.WithLogger(logger),
		tf2.WithModule(),      // Registers tf2.ModuleName
		backpack.WithModule(), // Registers backpack.ModuleName
	)
	if err != nil {
		logger.Error("Failed to initialize Steam client", log.Err(err))
		return
	}
	defer func() {
		_ = steamClient.Close()
		steamClient.Wait()
	}()

	// Access registered modules
	tf2Mod := steamClient.Module(tf2.ModuleName).(*tf2.TF2)
	bpMod := steamClient.Module(backpack.ModuleName).(*backpack.Backpack)

	// 3. Safe Currency Arithmetic Demo
	// 1.55 ref + 0.55 ref = 2.10 ref (14 scrap + 5 scrap = 19 scrap = 2.11 ref)
	// G-man provides exact float64 addition via Scrap conversion:
	totalRef := currency.AddRefined(1.55, 0.55)
	logger.Info("Safe refined sum calculated", log.Float64("total_ref", totalRef)) // Output: 2.11

	// 4. Listen for Real-Time Inventory Updates via GC SOCache
	sub := steamClient.Bus().Subscribe(&tf2.BackpackLoadedEvent{})
	go func() {
		for event := range sub.C() {
			if bpEvent, ok := event.(*tf2.BackpackLoadedEvent); ok {
				logger.Info("TF2 Inventory synced instantly via SOCache", log.Int("items_count", bpEvent.Count))
				
				// Access the pure currency available in the backpack
				pure := bpMod.GetPureStock()
				logger.Info("Available pure stock",
					log.Int("keys", pure.Keys),
					log.Float64("refined", pure.TotalRefined()),
				)
			}
		}
	}()

	// Block main thread (or wait for exit signals)
	select {}
}
```

### 🔑 Key Implementation Highlights

When building a production-grade bot (like the [trading bot example](/examples/tf2_bot/main.go)), you should leverage the core TF2 package architectures shown below:

1. **Stateful PriceDB & Market Sync**: Rather than querying HTTP APIs on demand, retrieve prices in real-time using Socket.IO from `backpack.tf` and cache them in the high-performance `pricedb.Manager`:
   ```go
   pdbClient := pricedb.NewClient(httpClient)
   pdbManager := pricedb.NewManager(pdbClient, logger)
   ```

2. **Automated Crafting & Metal Management**: Smelt duplicate weapons and process change dynamically during trades when pure metal reserves are insufficient:
   ```go
   craftingManager := crafting.NewManager(bp, tf2Mod)
   metalManager := crafting.NewMetalManager(bp, craftingManager, logger)
   ```

3. **Onion-style Trade Middlewares**: Combine stock limits, pricing, escrow checks, and ban filters into an extensible, middleware-based trading pipeline:
   ```go
   tradeEngine.Use(
       tf2trading.EscrowMiddleware(webTradeManager, logger),
       tf2trading.PricerMiddleware(pdbManager, logger),
       tf2trading.StockLimitMiddleware(bp, stockCfg, logger),
       tf2trading.SmartCounterMiddleware(metalManager, bp, webTradeManager, logger),
   )
   ```

## 📚 Best Practices

* **Always use `tf2/sku` for Item Identification**: Never compare items by `defindex` alone, as properties like paint, killstreaks, and qualities drastically change an item's value. Always convert a `tf2.Item` to its string SKU (e.g., `5021;6` for a Mann Co. Supply Crate Key) for mapping and database storage.
* **Use SOCache over WebAPI**: When writing trading logic, rely on the `tf2.Client.Backpack()` rather than querying the `community` web scraper, to guarantee you are trading with the most accurate, real-time items.
