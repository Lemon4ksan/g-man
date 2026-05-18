// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
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
	"github.com/lemon4ksan/g-man/pkg/tf2/schema"
	"github.com/lemon4ksan/g-man/pkg/trading/engine"
	"github.com/lemon4ksan/g-man/pkg/trading/reason"
	webtrading "github.com/lemon4ksan/g-man/pkg/trading/web"
)

func main() {
	// 1. Initialize JSON storage.
	jsonStorage, err := jsonfile.New("storage.json")
	if err != nil {
		panic(fmt.Errorf("failed to initialize storage: %w", err))
	}

	// 2. Setup the logger
	logger := log.New(log.DefaultConfig(log.LevelInfo))
	defer logger.Close()

	logger.Info("Starting G-man Trading Bot Example...")

	// 3. Configure the Steam Client
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
		logger.Info("Trading bot stopped safely.")
	}()

	// 4. Set up the Trading Engine
	tradeEngine := engine.New()

	tradeEngine.Use(func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			if len(ctx.Offer.ItemsToGive) == 0 && len(ctx.Offer.ItemsToReceive) > 0 {
				logger.Info("Received a donation offer! Accepting...", log.Uint64("trade_id", ctx.Offer.ID))
				ctx.Accept(reason.AcceptDonation)
				return nil
			}

			logger.Info("Declining non-donation offer", log.Uint64("trade_id", ctx.Offer.ID))
			ctx.Decline(reason.DeclineBegging)

			return next(ctx)
		}
	})

	// 5. Connect the Engine to the Trade Manager
	// We use the built-in engine.BotHandler to bridge our engine with the SDK's processor.
	bp := client.Module(backpack.ModuleName).(*backpack.Backpack)
	webTradeManager := client.Module("trading").(*webtrading.Manager)

	webTradeManager.SetOfferHandler(context.Background(), engine.NewBotHandler(tradeEngine, logger), bp)

	// 6. Subscribe to Core Events
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

	// 7. Connect and Login
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

	// 8. Wait for exit signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	<-stop
	logger.Info("Shutting down...")
}
