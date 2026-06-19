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

	"github.com/lemon4ksan/miyako/bus"
	"github.com/lemon4ksan/miyako/generic"

	"github.com/lemon4ksan/g-man/pkg/behavior"
	"github.com/lemon4ksan/g-man/pkg/behavior/guard"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/apps"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/directory"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/gc"
	"github.com/lemon4ksan/g-man/pkg/storage/jsonfile"
	"github.com/lemon4ksan/g-man/pkg/trading/engine"
	"github.com/lemon4ksan/g-man/pkg/trading/notifications"
	"github.com/lemon4ksan/g-man/pkg/trading/processor"
	"github.com/lemon4ksan/g-man/pkg/trading/reason"
	"github.com/lemon4ksan/g-man/pkg/trading/review"
	webtrading "github.com/lemon4ksan/g-man/pkg/trading/web"
)

// ---------------------------------------------------------
// Mock & Simple Providers for Notifications and Reviews
// ---------------------------------------------------------

// SimpleSchema implements review.SchemaProvider to translate SKUs to item names.
type SimpleSchema struct{}

func (s SimpleSchema) GetName(sku string, useDefindex bool) string {
	return "Generic Item [" + sku + "]"
}

// NotificationChat implements notifications.ChatProvider to send messages to users.
type NotificationChat struct {
	logger log.Logger
}

func (c NotificationChat) SendMessage(ctx context.Context, steamID id.ID, message string) error {
	c.logger.Info("Chat notification sent to partner",
		log.Uint64("partner_steam_id", uint64(steamID)),
		log.String("message", message),
	)

	return nil
}

// ReviewChat implements review.ChatProvider to send administrator alerts.
type ReviewChat struct {
	logger log.Logger
}

func (c ReviewChat) SendMessage(ctx context.Context, steamID uint64, message string) error {
	c.logger.Info("Review chat sent to partner",
		log.Uint64("partner_steam_id", steamID),
		log.String("message", message),
	)

	return nil
}

func (c ReviewChat) MessageAdmins(ctx context.Context, message string) error {
	c.logger.Warn("ADMIN ALERT: Trade Offer sent to Manual Review!",
		log.String("alert_details", message),
	)

	return nil
}

// NotificationConfig implements notifications.ConfigProvider for trade outcome templates.
type NotificationConfig struct{}

func (c NotificationConfig) GetTemplate(key string) string {
	switch key {
	case "accepted":
		return "Your trade offer #{{.OfferID}} was accepted successfully! Thank you!"
	case "declined":
		return "Your trade offer #{{.OfferID}} was declined. Reason: {{.ReasonType}}."
	default:
		return "Trade offer #{{.OfferID}} is now in status {{.OldState}}."
	}
}

func (c NotificationConfig) GetCommandPrefix() string {
	return "!"
}

// ---------------------------------------------------------
// Main Bot Entry Point
// ---------------------------------------------------------

func main() {
	jsonStorage, err := jsonfile.New("storage.json")
	if err != nil {
		panic(fmt.Errorf("failed to initialize storage: %w", err))
	}

	logger := log.New(log.DefaultConfig(log.LevelInfo))
	defer logger.Close()

	logger.Info("Starting G-man Generic Raw Trading Bot Example...")

	// 1. Initialize the behavior orchestrator to manage background tasks
	orchBus := bus.New()
	orchestrator := behavior.NewOrchestrator(orchBus, logger)

	// 2. Configure the Steam Client with necessary modules
	cfg := steam.DefaultConfig()
	cfg.Storage = jsonStorage
	cfg.Bus = orchBus

	// Setup Steam Guard configuration for automatic mobile confirmations
	sharedSecret := os.Getenv("STEAM_SHARED_SECRET")
	identitySecret := os.Getenv("STEAM_IDENTITY_SECRET")
	deviceID := os.Getenv("STEAM_DEVICE_ID")

	client, err := steam.NewClient(cfg,
		steam.WithLogger(logger),
		apps.WithModule(),
		gc.WithModule(),
		guard.WithModule(guard.DefaultGuardConfig(sharedSecret, identitySecret, deviceID)),
		webtrading.WithModule(webtrading.DefaultConfig()),
	)
	if err != nil {
		panic(fmt.Errorf("failed to create client: %w", err))
	}

	defer func() {
		_ = client.Close()
		client.Wait()
		logger.Info("Generic bot stopped safely.")
	}()

	// 3. Set up Core Steam & Trading Services
	webTradeManager := webtrading.From(client)
	guardian := guard.From(client)

	guard.AutoAccept(orchestrator, guardian, guard.Config{
		AutoAcceptTypes: generic.NewSet(guard.ConfTypeTrade, guard.ConfTypeLogin),
		PollOnStart:     true,
	})

	// 4. Setup the Generic Trading Engine
	tradeEngine := engine.New()

	// Middleware 1: Simple Escrow Hold Protection Check
	tradeEngine.Use(func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			if ctx.Offer.EscrowEndDate > 0 {
				logger.Warn("Decline offer: trade involves escrow hold period", log.Uint64("offer_id", ctx.Offer.ID))
				ctx.Decline(reason.TradeReason("ESCROW_HOLD_DETECTION"))
				return nil
			}

			return next(ctx)
		}
	})

	// Middleware 2: Raw Exchange Value Check (Item Count comparison)
	tradeEngine.Use(func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			giveCount := len(ctx.Offer.ItemsToGive)
			receiveCount := len(ctx.Offer.ItemsToReceive)

			// Simple raw rule: we must receive at least as many items as we give
			if receiveCount < giveCount {
				logger.Info("Decline offer: unequal exchange ratio", log.Uint64("offer_id", ctx.Offer.ID))
				ctx.Decline(reason.TradeReason("INSUFFICIENT_ITEMS_VALUE"))
				return nil
			}

			// If the trade is highly profitable (we get twice as many items), accept immediately
			if receiveCount >= giveCount*2 && giveCount > 0 {
				logger.Info("Accept offer: highly profitable raw trade exchange", log.Uint64("offer_id", ctx.Offer.ID))
				ctx.Accept(reason.TradeReason("PROFITABLE_ITEM_RATIO"))
				return nil
			}

			return next(ctx)
		}
	})

	// Middleware 3: Security & High-Volume Review Filter
	tradeEngine.Use(func(next engine.Handler) engine.Handler {
		return func(ctx *engine.TradeContext) error {
			// Security rule: if we are giving away more than 10 items, send to manual review
			if len(ctx.Offer.ItemsToGive) > 10 {
				logger.Warn(
					"Review offer: high-volume outgoing items limit reached",
					log.Uint64("offer_id", ctx.Offer.ID),
				)
				ctx.Review(reason.TradeReason("HIGH_VOLUME_SAFETY_REVIEW"))

				return nil
			}

			// Otherwise, if all checks pass, accept the trade offer!
			ctx.Accept(reason.AcceptCorrectValue)

			return nil
		}
	})

	// 5. Initialize the Unified Cohesive Trade Processor
	// We instantiate mock chat/config providers to route notifications and reviews.
	notifChat := &NotificationChat{logger: logger}
	notifConfig := &NotificationConfig{}
	notifManager := notifications.NewManager(notifChat, notifConfig, logger)

	reviewChat := &ReviewChat{logger: logger}
	schemaProvider := &SimpleSchema{}
	reviewer := review.New(schemaProvider, reviewChat, logger)

	tradeProcessor := processor.New(
		webTradeManager, // web.Manager implements TradeExecutor (AcceptOffer / DeclineOffer)
		tradeEngine,     // Core decision-making engine
		notifManager,    // Notification manager
		reviewer,        // Manual reviewer
		logger,          // Custom slog logger
	)

	// Start the sequential queue worker in the background
	tradeProcessor.Start(context.Background())

	// 6. Subscribe to Core & Trade Events
	// We handle auth changes and route active WebAPI trade offers into our unified processor.
	sub := client.Bus().Subscribe(
		&auth.LoggedOnEvent{},
		&auth.LoggedOffEvent{},
		&webtrading.NewOfferEvent{},
	)
	go handleEvents(sub, tradeProcessor, logger)

	// 7. Run the Steam client
	if err := client.Run(); err != nil {
		panic(err)
	}

	// 8. Connect and Login
	loginCtx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	dir := directory.New(client.Service())

	server, err := dir.GetOptimalCMServer(loginCtx)
	if err != nil {
		logger.Error("Failed to fetch CM server list", log.Err(err))
		return
	}

	user, pass := os.Getenv("STEAM_USER"), os.Getenv("STEAM_PASS")

	if user == "" || pass == "" {
		logger.Error("Credentials not set! Specify STEAM_USER and STEAM_PASS env variables.")
		return
	}

	loginDetails := auth.NewLogOnDetails(user, pass)
	logger.Info("Attempting login...", log.String("user", loginDetails.AccountName))

	if err := client.ConnectAndLogin(context.Background(), server, loginDetails); err != nil {
		logger.Error("Login process failed", log.Err(err))
		return
	}

	// 9. Start background behavior orchestrator
	if err := orchestrator.Start(context.Background()); err != nil {
		logger.Error("Failed to start behavior orchestrator", log.Err(err))
	}

	defer orchestrator.Stop()

	// 10. Wait for OS exit signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	logger.Info("Shutting down G-man generic bot...")
}

func handleEvents(sub *bus.Subscription, proc *processor.Processor, logger log.Logger) {
	for event := range sub.C() {
		switch ev := event.(type) {
		case *auth.LoggedOnEvent:
			logger.Info("Login successful!", log.Uint64("steam_id", ev.SteamID))

		case *auth.LoggedOffEvent:
			logger.Info("Logged off from Steam")

		case *webtrading.NewOfferEvent:
			logger.Info("New active trade offer received from event bus",
				log.Uint64("offer_id", ev.Offer.ID),
				log.Uint64("partner_steam_id", uint64(ev.Offer.OtherSteamID)),
			)
			// Enqueue the incoming offer into the unified sequential processor
			proc.Enqueue(ev.Offer)
		}
	}
}
