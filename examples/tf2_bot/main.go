// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/apps"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/directory"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/gc"
	"github.com/lemon4ksan/g-man/pkg/storage/jsonfile"
	"github.com/lemon4ksan/g-man/pkg/tf2"
	"github.com/lemon4ksan/g-man/pkg/tf2/backpack"
	"github.com/lemon4ksan/g-man/pkg/tf2/bptf"
	"github.com/lemon4ksan/g-man/pkg/tf2/crafting"
	"github.com/lemon4ksan/g-man/pkg/tf2/pricedb"
	"github.com/lemon4ksan/g-man/pkg/tf2/rep"
	"github.com/lemon4ksan/g-man/pkg/tf2/schema"
	tf2trading "github.com/lemon4ksan/g-man/pkg/tf2/trading"
	"github.com/lemon4ksan/g-man/pkg/trading/engine"
	webtrading "github.com/lemon4ksan/g-man/pkg/trading/web"
)

func main() {
	jsonStorage, err := jsonfile.New("storage.json")
	if err != nil {
		panic(fmt.Errorf("failed to initialize storage: %w", err))
	}

	logger := log.New(log.DefaultConfig(log.LevelInfo))
	defer logger.Close()

	logger.Info("Starting G-man TF2 Trading Bot Example...")

	// 1. Initialize TF2 Trade Configuration
	tradeCfgManager, err := tf2trading.NewConfigManager("trading_config.json")
	if err != nil {
		logger.Error("Failed to initialize trade config", log.Err(err))
		return
	}

	// 2. Setup standard HTTP clients and TF2 API services
	httpClient := &http.Client{Timeout: 30 * time.Second}
	bptfClient := bptf.New(httpClient, os.Getenv("BPTF_API_KEY"), os.Getenv("BPTF_USER_TOKEN"))
	pdbClient := pricedb.NewClient(httpClient)

	pdbManager := pricedb.NewManager(pdbClient, logger)
	bansManager := rep.NewBansManager(bptfClient, os.Getenv("MPTF_API_KEY"))
	bptfChecker := bptf.NewBackpackTFChecker(bptfClient)

	// 3. Configure the Steam Client with all necessary modules
	cfg := steam.DefaultConfig()
	cfg.Storage = jsonStorage

	client, err := steam.NewClient(cfg,
		steam.WithLogger(logger),
		apps.WithModule(),
		gc.WithModule(),
		tf2.WithModule(),
		schema.WithModule(schema.DefaultConfig()),
		backpack.WithModule(),
		webtrading.WithModule(webtrading.Config{PollInterval: 30 * time.Second}),
	)
	if err != nil {
		panic(fmt.Errorf("failed to create client: %w", err))
	}

	defer func() {
		_ = client.Close()
		client.Wait()
		logger.Info("TF2 bot stopped safely.")
	}()

	// 4. Set up Crafting / Metal Manager
	bp := client.Module(backpack.ModuleName).(*backpack.Backpack)
	tf2Mod := client.Module(tf2.ModuleName).(*tf2.TF2)
	webTradeManager := client.Module("trading").(*webtrading.Manager)

	craftingManager := crafting.NewManager(bp, tf2Mod)
	metalManager := crafting.NewMetalManager(bp, craftingManager, logger)

	// 5. Setup the TF2 Trading Engine Middlewares
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

	// 6. Connect the Engine to the Trade Manager
	// We use the built-in engine.BotHandler to bridge our engine with the SDK's processor.
	webTradeManager.SetOfferHandler(context.Background(), engine.NewBotHandler(tradeEngine, logger), bp)

	// 7. Subscribe to Core Events
	sub := client.Bus().Subscribe(
		&auth.LoggedOnEvent{},
		&auth.SteamGuardRequiredEvent{},
	)

	go func() {
		for event := range sub.C() {
			switch ev := event.(type) {
			case *auth.SteamGuardRequiredEvent:
				if ev.IsAppConfirm {
					logger.Info("Please confirm the login on your Steam Mobile Authenticator.")
					continue
				}

				go func(cb func(string)) {
					var code string

					fmt.Print("Enter Steam Guard Code: ")

					_, _ = fmt.Scanln(&code)
					cb(code)
				}(ev.Callback)

			case *auth.LoggedOnEvent:
				logger.Info("Login successful!", log.Uint64("steam_id", ev.SteamID))
			}
		}
	}()

	// 8. Connect and Login
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	dir := directory.New(client.Service())

	server, err := dir.GetOptimalCMServer(ctx)
	if err != nil {
		logger.Error("Failed to get CM server list", log.Err(err))
		return
	}

	user := os.Getenv("STEAM_USER")
	pass := os.Getenv("STEAM_PASS")

	if user == "" || pass == "" {
		logger.Error("Credentials not set! Use STEAM_USER and STEAM_PASS env variables.")
		return
	}

	loginDetails := auth.NewLogOnDetails(user, pass)
	logger.Info("Attempting login...", log.String("user", loginDetails.AccountName))

	err = client.ConnectAndLogin(context.Background(), server, loginDetails)
	if err != nil {
		logger.Error("Login process failed", log.Err(err))
		return
	}

	// 9. Start config hot-reloader
	tradeCfgManager.StartWatching(context.Background(), 10*time.Second, logger)

	// 10. Wait for exit signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	<-stop
	logger.Info("Shutting down TF2 bot...")
}
