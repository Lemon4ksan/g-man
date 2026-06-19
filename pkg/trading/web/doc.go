// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package web manages asynchronous trade offers via the Steam WebAPI.
//
// # Key Components
//
//   - [Manager]: The central coordinator that polls for sent and received trade offers, managing their states.
//   - [Config]: Configures parameters for polling intervals, language settings, and auto-cancellation limits.
//   - [NewOfferEvent]: Emitted via the event bus when a new, active trade offer is received.
//   - [OfferChangedEvent]: Emitted via the event bus when a tracked offer's transactional state changes.
//
// # Basic Usage Example
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"time"
//		"github.com/lemon4ksan/miyako/bus"
//		"github.com/lemon4ksan/g-man/pkg/log"
//		"github.com/lemon4ksan/g-man/pkg/trading/web"
//	)
//
//	func main() {
//		logger := log.New(log.DefaultConfig(log.LevelInfo))
//		eventBus := bus.New()
//
//		// Build the configuration
//		cfg := web.DefaultConfig()
//		cfg.PollInterval = 10 * time.Second
//
//		// Create a new trade manager
//		manager := web.New(cfg)
//
//		// Subscribe to new offers
//		sub := eventBus.Subscribe(&web.NewOfferEvent{})
//		defer sub.Unsubscribe()
//
//		fmt.Println("Trade manager initialized with poll interval:", manager.Count())
//	}
package web
