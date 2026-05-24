// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"testing"
	"time"

	"github.com/lemon4ksan/g-man/pkg/behavior"
	"github.com/lemon4ksan/g-man/pkg/behavior/achievements"
	guardbeh "github.com/lemon4ksan/g-man/pkg/behavior/guard"
	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/guard"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/apps"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/gc"
	"github.com/lemon4ksan/g-man/pkg/storage/jsonfile"
	"github.com/lemon4ksan/g-man/pkg/tf2"
	"github.com/lemon4ksan/g-man/pkg/tf2/backpack"
	"github.com/lemon4ksan/g-man/pkg/tf2/behavior/stock"
	"github.com/lemon4ksan/g-man/pkg/tf2/bptf"
	"github.com/lemon4ksan/g-man/pkg/tf2/crafting"
	"github.com/lemon4ksan/g-man/pkg/tf2/pricedb"
	"github.com/lemon4ksan/g-man/pkg/tf2/rep"
	"github.com/lemon4ksan/g-man/pkg/tf2/schema"
	tf2trading "github.com/lemon4ksan/g-man/pkg/tf2/trading"
	"github.com/lemon4ksan/g-man/pkg/trading/engine"
	webtrading "github.com/lemon4ksan/g-man/pkg/trading/web"
)

type MemStats struct {
	Step        string
	HeapAllocMB float64
	SysMB       float64
	HeapObjects uint64
	TimeDiffMS  int64
}

type CachedSchema struct {
	Version string     `json:"Version"`
	Raw     schema.Raw `json:"Raw"`
}

func getMemStats(step string, start time.Time) MemStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return MemStats{
		Step:        step,
		HeapAllocMB: float64(m.HeapAlloc) / (1024 * 1024),
		SysMB:       float64(m.Sys) / (1024 * 1024),
		HeapObjects: m.HeapObjects,
		TimeDiffMS:  time.Since(start).Milliseconds(),
	}
}

func printStats(title string, stats []MemStats) {
	fmt.Println()
	fmt.Printf("================================ %s ================================\n", title)
	fmt.Printf("%-35s | %-12s | %-12s | %-15s | %-10s\n", "Step", "HeapAlloc", "Sys (OS)", "Heap Objects", "Duration")
	fmt.Printf(
		"%-35s | %-12s | %-12s | %-15s | %-10s\n",
		"-----------------------------------",
		"------------",
		"------------",
		"---------------",
		"----------",
	)

	for _, s := range stats {
		fmt.Printf("%-35s | %10.2f MB | %10.2f MB | %15d | %8d ms\n",
			s.Step, s.HeapAllocMB, s.SysMB, s.HeapObjects, s.TimeDiffMS)
	}

	fmt.Println("============================================================================================")
	fmt.Println()
}

func TestMemoryBenchmark(t *testing.T) {
	stats := make([]MemStats, 0, 5)
	startTotal := time.Now()

	// 1. Baseline (Start)
	runtime.GC()

	stats = append(stats, getMemStats("1. Baseline (Start)", startTotal))

	// 2. Locate and Read Cache File
	cachePath := filepath.Join("cache", "tf2", "json")
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		t.Skipf("Cache file not found at %s. Please run the bot once to generate it.", cachePath)
	}

	start := time.Now()

	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("failed to read cache: %v", err)
	}

	defer func() {
		data = nil

		runtime.GC()
	}()

	stats = append(stats, getMemStats("2. Read Cache File to RAM", start))

	// 3. Unmarshal JSON into Cached Schema Wrapper
	start = time.Now()

	var cached CachedSchema

	err = json.Unmarshal(data, &cached)
	if err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	data = nil // Dereference immediately to allow GC

	stats = append(stats, getMemStats("3. Unmarshaled schema.Raw", start))

	// 4. Build Schema & O(1) Lookup Indices
	start = time.Now()

	s := schema.New(&cached.Raw)
	if s == nil {
		t.Fatal("expected schema to be created, got nil")
	}

	stats = append(stats, getMemStats("4. Built schema.Schema indices", start))

	// 5. Run GC to free temporary JSON decoding buffers/objects
	start = time.Now()

	runtime.GC()

	stats = append(stats, getMemStats("5. After runtime.GC() (Active RAM)", start))

	printStats("SCHEMA ONLY MEMORY PROFILE", stats)

	// Keep reference to s so compiler doesn't optimize it away early
	if s.Version != "" {
		_ = s.Version
	}
}

func TestFullBotMemoryProfile(t *testing.T) {
	stats := make([]MemStats, 0, 10)
	startTotal := time.Now()

	// 1. Baseline
	runtime.GC()

	stats = append(stats, getMemStats("1. Baseline (Start)", startTotal))

	// 2. Initialize JSON storage
	start := time.Now()

	jsonStorage, err := jsonfile.New("storage.json")
	if err != nil {
		t.Fatalf("failed to initialize storage: %v", err)
	}

	stats = append(stats, getMemStats("2. Initialized JSON Storage", start))

	// 3. Initialize Trade Config Manager
	start = time.Now()

	tradeCfgManager, err := tf2trading.NewConfigManager("trading_config.json")
	if err != nil {
		t.Fatalf("failed to initialize trade config: %v", err)
	}

	stats = append(stats, getMemStats("3. Loaded Trade Config Manager", start))

	// 4. Initialize HTTP Clients and Sync Managers
	start = time.Now()
	httpClient := &http.Client{Timeout: 5 * time.Second}

	logger := log.New(log.DefaultConfig(log.LevelWarn)) // Minimal logging in test
	defer logger.Close()

	bptfClient := bptf.New(httpClient, "mock-api-key", "mock-token")
	pdbClient := pricedb.NewClient(httpClient)

	pdbManager := pricedb.NewManager(pdbClient, logger)
	bansManager := rep.NewBansManager(bptfClient, "mock-api-key")
	bptfChecker := bptf.NewBackpackTFChecker(bptfClient)

	orchestrator := behavior.NewOrchestrator(logger, bus.New())
	orchestrator.Register(pdbManager)

	stats = append(stats, getMemStats("4. Initialized Sync & Auth Managers", start))

	// 5. Create Steam Client & All Modules
	start = time.Now()
	cfg := steam.DefaultConfig()
	cfg.Storage = jsonStorage
	cfg.Bus = orchestrator.Bus()

	client, err := steam.NewClient(cfg,
		steam.WithLogger(logger),
		apps.WithModule(),
		gc.WithModule(),
		tf2.WithModule(),
		schema.WithModule(schema.DefaultConfig()),
		backpack.WithModule(),
		guard.WithModule(guard.DefaultConfig()),
		webtrading.WithModule(webtrading.Config{PollInterval: 30 * time.Second}),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	stats = append(stats, getMemStats("5. Created Steam Client & Modules", start))

	// 6. Setup Behaviors, Crafting, Listings
	start = time.Now()
	bp := backpack.From(client)
	tf2Mod := tf2.From(client)
	schemaMgr := schema.From(client)
	webTradeManager := webtrading.From(client)
	guardian := guard.From(client)

	listingMgr := bptf.NewListingManager(bptfClient, schemaMgr, logger)
	craftingManager := crafting.NewManager(bp, tf2Mod)
	metalManager := crafting.NewMetalManager(bp, craftingManager, logger)

	orchestrator.Install(
		guardbeh.AutoAccept(guardian, guardbeh.Config{
			AutoAcceptTypes: []guard.ConfirmationType{guard.ConfTypeTrade},
		}),
		stock.Control(bp, listingMgr, pdbManager, tradeCfgManager),
		achievements.Simulate(tf2Mod, tf2.AchievementConfig()),
	)

	stats = append(stats, getMemStats("6. Installed Bot Behaviors", start))

	// 7. Load Schema Cache into Schema Manager (Simulating active bot state)
	start = time.Now()

	cachePath := filepath.Join("cache", "tf2", "json")
	if _, err := os.Stat(cachePath); err == nil {
		data, err := os.ReadFile(cachePath)
		if err == nil {
			var cached CachedSchema
			if json.Unmarshal(data, &cached) == nil {
				// Inject into schema module using buildSchema reflection/direct logic
				// For the memory test, we can directly build schema and set it on the module
				s := schema.New(&cached.Raw)
				s.Version = cached.Version
				// We don't need to link it fully, just holding the pointer in memory represents the loaded state
				_ = s
			}
		}
	}

	stats = append(stats, getMemStats("7. Loaded & Indexed Schema Cache", start))

	// 8. Setup Trading Engine & Middlewares
	start = time.Now()
	tradeEngine := engine.New()
	tradeCfg := tradeCfgManager.GetConfig()

	stockCfg := tf2trading.StockConfig{
		MaxTotal:   tradeCfg.GlobalMaxStock,
		DefaultMax: tradeCfg.DefaultMaxStock,
		MaxPerSKU:  make(map[string]int),
	}
	for sku, c := range tradeCfg.Items {
		stockCfg.MaxPerSKU[sku] = c.MaxStock
	}

	tradeEngine.Use(
		tf2trading.EscrowMiddleware(webTradeManager, logger),
		tf2trading.BanCheckMiddleware(bansManager, logger),
		tf2trading.PricerMiddleware(pdbManager, logger),
		tf2trading.DupeCheckMiddleware(bptfChecker, logger),
		tf2trading.StockLimitMiddleware(bp, stockCfg, logger),
		tf2trading.SmartCounterMiddleware(metalManager, bp, webTradeManager, logger),
	)

	stats = append(stats, getMemStats("8. Configured Trading Engine Chain", start))

	// 9. Run GC and analyze active bot memory size
	start = time.Now()

	runtime.GC()

	stats = append(stats, getMemStats("9. After runtime.GC() (Active Heap)", start))

	// 10. Call FreeOSMemory
	start = time.Now()

	debug.FreeOSMemory()

	stats = append(stats, getMemStats("10. After FreeOSMemory() (OS Reclaimed)", start))

	printStats("FULL BOT INITIALIZED MEMORY PROFILE", stats)

	// Write heap profile for the entire bot
	profFile, err := os.Create("full_bot_profile.pprof")
	if err == nil {
		defer profFile.Close()

		_ = pprof.WriteHeapProfile(profFile)
	}

	// Keep references alive
	if client != nil {
		_ = client.Service()
	}

	_ = tradeEngine
}
