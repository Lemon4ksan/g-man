// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/lemon4ksan/g-man/pkg/behavior"
	guardbehavior "github.com/lemon4ksan/g-man/pkg/behavior/guard"
	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/guard"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/directory"
	"github.com/lemon4ksan/g-man/pkg/storage"
	"github.com/lemon4ksan/g-man/pkg/storage/jsonfile"
)

// Config holds the configuration for the bot loaded from environment variables.
type Config struct {
	// Username is the Steam account username.
	Username string
	// Password is the Steam account password.
	Password string
	// SharedSecret is the TOTP secret key used to generate 2FA auth codes.
	SharedSecret string
	// IdentitySecret is the secret key used to confirm trade and market transactions.
	IdentitySecret string
	// DeviceID is the unique mobile device identifier.
	DeviceID string
	// StoragePath is the local file path to persist session and machine data.
	StoragePath string
}

// Bot encapsulates all core systems, storage, loggers, and coordinates the session lifecycle.
type Bot struct {
	cfg          Config
	store        storage.Provider
	logger       log.Logger
	client       *steam.Client
	sub          *bus.Subscription
	orchestrator *behavior.Orchestrator
	wg           sync.WaitGroup
}

// NewBot creates and initializes a new bot instance using the provided configuration
// and injected storage and logger dependencies.
func NewBot(cfg Config, store storage.Provider, logger log.Logger) (*Bot, error) {
	clientCfg := steam.DefaultConfig()
	clientCfg.Storage = store

	logger = logger.With(log.Module("bot"))

	opts := []steam.Option{
		steam.WithLogger(logger),
		guard.WithModule(guard.Config{
			SharedSecret:   cfg.SharedSecret,
			IdentitySecret: cfg.IdentitySecret,
			DeviceID:       cfg.DeviceID,
			RateLimit:      2 * time.Second,
		}),
	}

	client, err := steam.NewClient(clientCfg, opts...)
	if err != nil {
		return nil, fmt.Errorf("steam client initialization failed: %w", err)
	}

	bot := &Bot{
		cfg:    cfg,
		store:  store,
		logger: logger,
		client: client,
	}

	return bot, nil
}

// Run starts the bot's background systems, establishes connection and logs on to Steam.
// It blocks until the context is canceled or a termination signal is received.
func (b *Bot) Run(ctx context.Context) error {
	b.logger.Info("Starting core client services...")

	if err := b.client.Run(); err != nil {
		return fmt.Errorf("client run failed: %w", err)
	}

	server, err := b.discoverCMServer(ctx)
	if err != nil {
		return fmt.Errorf("cm discovery failed: %w", err)
	}

	b.logger.Info("Optimal CM server found",
		log.String("endpoint", server.Endpoint),
		log.Float64("load", server.Load),
	)

	b.setupOrchestrator()

	if err := b.orchestrator.Start(ctx); err != nil {
		return fmt.Errorf("orchestrator start failed: %w", err)
	}

	b.sub = b.client.Bus().Subscribe(&auth.LoggedOnEvent{}, &auth.LoggedOffEvent{})

	// Explicitly track background task execution using a WaitGroup
	b.wg.Go(func() {
		b.handleEvents(ctx)
	})

	b.logger.Info("Connecting and authenticating with Steam...",
		log.String("username", b.cfg.Username),
	)

	details := &auth.LogOnDetails{
		AccountName: b.cfg.Username,
		Password:    b.cfg.Password,
	}
	if err := b.client.ConnectAndLogin(ctx, server, details); err != nil {
		return fmt.Errorf("connect and login failed: %w", err)
	}

	b.logger.Info("Bot logged in and fully operational")

	b.waitForShutdown(ctx)

	return nil
}

// Close gracefully shuts down the bot, stopping the orchestrator and closing the client connection.
func (b *Bot) Close() {
	if b.orchestrator != nil {
		b.orchestrator.Stop()
		b.logger.Info("Behavior orchestrator stopped")
	}

	if b.sub != nil {
		b.sub.Unsubscribe()
	}

	// Wait for event handler loop to exit before closing the client
	b.wg.Wait()

	if err := b.client.Close(); err != nil {
		b.logger.Error("Error during client shutdown", log.Err(err))
	} else {
		b.logger.Info("Client session closed")
	}

	b.logger.Info("Bot shut down successfully")
}

func (b *Bot) discoverCMServer(ctx context.Context) (socket.CMServer, error) {
	dirCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	b.logger.Info("Discovering optimal Steam Connection Manager server...")
	dir := directory.New(b.client.Service())

	return dir.GetOptimalCMServer(dirCtx)
}

func (b *Bot) setupOrchestrator() {
	b.orchestrator = behavior.NewOrchestrator(b.logger, b.client.Bus())
	guardModule := guard.From(b.client)

	guardBehaviorCfg := guardbehavior.Config{
		AutoAcceptTypes: []guard.ConfirmationType{
			guard.ConfTypeTrade,
			guard.ConfTypeMarket,
			guard.ConfTypeLogin,
		},
		PollOnStart: true,
	}

	b.orchestrator.Install(guardbehavior.AutoAccept(guardModule, guardBehaviorCfg))
}

func (b *Bot) handleEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-b.sub.C():
			if !ok {
				return
			}

			switch ev := event.(type) {
			case *auth.LoggedOnEvent:
				b.logger.Info("Login successful", log.Uint64("steam_id", ev.SteamID))
			case *auth.LoggedOffEvent:
				b.logger.Info("Logged off")
			}
		}
	}
}

func (b *Bot) waitForShutdown(ctx context.Context) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-ctx.Done():
		b.logger.Info("Shutdown triggered by context cancellation")
	case sig := <-sigChan:
		b.logger.Info("System signal received, shutting down gracefully", log.String("signal", sig.String()))
	}
}

func loadEnvConfig() (Config, error) {
	username := os.Getenv("STEAM_USER")
	password := os.Getenv("STEAM_PASS")

	if username == "" || password == "" {
		return Config{}, errors.New("STEAM_USER and STEAM_PASS environment variables are required")
	}

	storagePath := os.Getenv("STEAM_STORAGE_PATH")
	if storagePath == "" {
		storagePath = "storage.json"
	}

	return Config{
		Username:       username,
		Password:       password,
		SharedSecret:   os.Getenv("STEAM_SHARED_SECRET"),
		IdentitySecret: os.Getenv("STEAM_IDENTITY_SECRET"),
		DeviceID:       os.Getenv("STEAM_DEVICE_ID"),
		StoragePath:    storagePath,
	}, nil
}

func main() {
	cfg, err := loadEnvConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	store, err := jsonfile.New(cfg.StoragePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize storage: %v\n", err)
		os.Exit(1)
	}

	defer func() {
		if err := store.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to close storage: %v\n", err)
		}
	}()

	logCfg := log.DefaultConfig(log.LevelDebug)
	logCfg.FullPath = true

	logger := log.New(logCfg)
	defer func() {
		if err := logger.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to close logger: %v\n", err)
		}
	}()

	bot, err := NewBot(cfg, store, logger)
	if err != nil {
		logger.Error("Failed to create bot", log.Err(err))
		return
	}
	defer bot.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := bot.Run(ctx); err != nil {
		logger.Error("Bot runtime error", log.Err(err))
		return
	}
}
