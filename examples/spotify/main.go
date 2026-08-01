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

	"github.com/lemon4ksan/miyako/log"

	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/social/status"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/apps"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/directory"
	"github.com/lemon4ksan/g-man/pkg/storage/jsonfile"
)

func main() {
	jsonStorage, err := jsonfile.New("storage.json")
	if err != nil {
		panic(fmt.Errorf("failed to initialize storage: %w", err))
	}
	defer jsonStorage.Close()

	logger := log.New(log.DefaultConfig(log.LevelInfo))
	defer logger.Close()

	client, err := steam.NewClient(
		steam.DefaultConfig(),
		steam.WithStorage(jsonStorage),
		steam.WithLogger(logger),
		apps.WithModule(),
		status.WithModule(
			status.WithUpdateInterval(5*time.Second),
		),
	)
	if err != nil {
		panic(err)
	}

	clientID, clientSecret, refreshToken := os.Getenv(
		"SPOTIFY_CLIENT_ID",
	), os.Getenv(
		"SPOTIFY_CLIENT_SECRET",
	), os.Getenv(
		"SPOTIFY_REFRESH_TOKEN",
	)
	if clientID == "" || clientSecret == "" || refreshToken == "" {
		logger.Error("Spotify credentials not set!")
		return
	}

	spotify := NewProvider(Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RefreshToken: refreshToken,
	})

	statusMgr := status.From(client)

	statusMgr.SetDynamicProvider(func(ctx context.Context) string {
		track, err := spotify.FetchCurrentTrack(ctx)
		if err != nil {
			return ""
		}

		return track.StatusString()
	})

	if err := client.Run(); err != nil {
		panic(err)
	}

	user, pass := os.Getenv("STEAM_USER"), os.Getenv("STEAM_PASS")
	if user == "" || pass == "" {
		logger.Error("Credentials not set!")
		return
	}

	loginDetails := auth.NewLogOnDetails(user, pass)
	logger.Info("Attempting login...", log.String("user", loginDetails.AccountName))

	loginCtx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	server, err := directory.New(client).GetOptimalCMServer(loginCtx)
	if err != nil {
		logger.Error("Failed to fetch CM server list", log.Err(err))
		return
	}

	if err := client.ConnectAndLogin(loginCtx, server, loginDetails); err != nil {
		logger.Error("Login process failed", log.Err(err))
		return
	}

	logger.Info("Spotify music streaming started!")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	logger.Info("Shutting down G-man spotify bot...")

	if err := client.Close(); err != nil {
		logger.Error("Failed to close client", log.Err(err))
	}

	client.Wait()
}
