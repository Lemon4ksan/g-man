// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package account provides tools to manage, validate, and cache account-related data, limits, bans, wallet info, and guest passes.
// It automatically listens to Steam network packets, decodes account metadata, parses wallet state changes,
// tracks VAC bans, and caches the retrieved payloads inside thread-safe local buffers.
//
// The primary orchestrator is [Account], which can be registered on a [steam.Client] using [WithModule] or retrieved via [From].
// It publishes structured state updates such as [InfoEvent], [EmailInfoEvent], [LimitationsEvent], [VACBansEvent], and [WalletInfoEvent]
// to the internal client event bus.
//
// Basic usage example:
//
//	package main
//
//	import (
//		"fmt"
//
//		"github.com/lemon4ksan/g-man/pkg/steam"
//		"github.com/lemon4ksan/g-man/pkg/steam/sys/account"
//	)
//
//	func main() {
//		ctx := context.Background()
//		logger := log.New(log.DefaultConfig(log.LevelInfo))
//
//		// Build standard client config
//		clientCfg := steam.DefaultConfig()
//		client, err := steam.NewClient(clientCfg, steam.WithLogger(logger))
//		if err != nil {
//			fmt.Println("Failed to create client:", err)
//			return
//		}
//		defer client.Close()
//
//		// Initialize the account module
//		a := account.New()
//		client.RegisterModule(a)
//
//		// Run client systems
//		if err := client.Run(); err != nil {
//			fmt.Println("Failed to run client:", err)
//			return
//		}
//
//		// Retrieve the account module instance
//		acc := account.From(client)
//		info := acc.GetAccountInfo()
//		fmt.Println("Cached Persona Name:", info.PersonaName)
//	}
package account
