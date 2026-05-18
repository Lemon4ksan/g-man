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
	"github.com/lemon4ksan/g-man/pkg/steam/social/friends"
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
	jsonStorage, err := jsonfile.New("storage.json")
	if err != nil {
		panic(err)
	}

	logger := log.New(log.DefaultConfig(log.LevelInfo))
	defer logger.Close()

	logger.Info("Starting G-man Custom Logic Bot Example...")

	// 1. Initialize Client with Friends module
	cfg := steam.DefaultConfig()
	cfg.Storage = jsonStorage

	client, err := steam.NewClient(cfg,
		steam.WithLogger(logger),
		apps.WithModule(),
		gc.WithModule(),
		tf2.WithModule(),
		schema.WithModule(schema.DefaultConfig()),
		backpack.WithModule(),
		friends.WithModule(), // We add friends module to check whitelist
		webtrading.WithModule(webtrading.Config{PollInterval: 30 * time.Second}),
	)
	if err != nil {
		panic(err)
	}

	defer func() {
		_ = client.Close()
		client.Wait()
	}()

	// 2. Setup the Trading Engine with Custom Middlewares
	tradeEngine := engine.New()

	// CUSTOM LOGIC 1: Whitelist for Friends
	// If the user is on our friends list, we might want to skip some checks or handle them differently.
	tradeEngine.Use(func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			friendsMod := client.Module("friends").(*friends.Manager)

			if friendsMod.IsFriend(ctx.Offer.OtherSteamID) {
				logger.Info("Trade partner is a friend! Applying whitelist logic.",
					log.Uint64("steam_id", uint64(ctx.Offer.OtherSteamID)))
				ctx.Set("is_whitelisted", true)
			}

			return next(ctx)
		}
	})

	// CUSTOM LOGIC 2: "Business Hours" Middleware
	// Decline or Skip all trades during "maintenance" hours (e.g., 3 AM to 4 AM).
	tradeEngine.Use(func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			now := time.Now()
			if now.Hour() == 3 {
				logger.Warn("Bot is currently in maintenance mode. Skipping trade.")
				ctx.Review(reason.ReviewHalted) // Mark for review instead of declining
				return nil
			}

			return next(ctx)
		}
	})

	// CUSTOM LOGIC 3: Automatic Donation Acceptance
	tradeEngine.Use(func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			// If we give 0 items, it's a gift.
			if len(ctx.Offer.ItemsToGive) == 0 {
				logger.Info("Accepting gift/donation", log.Uint64("offer_id", ctx.Offer.ID))
				ctx.Accept(reason.AcceptDonation)
				return nil
			}

			return next(ctx)
		}
	})

	// 3. Connect Engine to Trade Manager
	bp := client.Module(backpack.ModuleName).(*backpack.Backpack)
	webTradeManager := client.Module("trading").(*webtrading.Manager)
	webTradeManager.SetOfferHandler(context.Background(), engine.NewBotHandler(tradeEngine, logger), bp)

	// 4. Standard Login Boilerplate
	sub := client.Bus().Subscribe(&auth.LoggedOnEvent{}, &auth.SteamGuardRequiredEvent{})
	go func() {
		for event := range sub.C() {
			switch ev := event.(type) {
			case *auth.SteamGuardRequiredEvent:
				go func(cb func(string)) {
					var code string

					fmt.Print("Enter Steam Guard Code: ")

					_, _ = fmt.Scanln(&code)
					cb(code)
				}(ev.Callback)

			case *auth.LoggedOnEvent:
				logger.Info("Logged in!", log.Uint64("steam_id", ev.SteamID))
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	dir := directory.New(client.Service())
	server, _ := dir.GetOptimalCMServer(ctx)

	loginDetails := auth.NewLogOnDetails(os.Getenv("STEAM_USER"), os.Getenv("STEAM_PASS"))
	if loginDetails.AccountName == "" {
		logger.Error("STEAM_USER/STEAM_PASS not set!")
		return
	}

	_ = client.ConnectAndLogin(context.Background(), server, loginDetails)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
}
