// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package notifications provides a client manager to handle and route incoming Steam notification messages.
// It decodes structured network packets, registers service callbacks, and publishes events such as comments,
// items, offline messages, and marketing announcements to the client event bus.
//
// The central module is [Notifications], which can be registered on a [steam.Client] using [WithModule] or retrieved via [From].
// It tracks notification counts and emits specialized events like [ReceivedEvent], [ItemAnnouncementsEvent],
// [CommentNotificationsEvent], and [UserNotificationsEvent] containing [NotificationType] and [MarketingMessage] metadata.
//
// Basic usage example:
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//
//		"github.com/lemon4ksan/g-man/pkg/steam"
//		"github.com/lemon4ksan/g-man/pkg/steam/sys/notifications"
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
//		// Initialize the notifications module
//		n := notifications.New()
//		client.RegisterModule(n)
//
//		// Run client systems
//		if err := client.Run(); err != nil {
//			fmt.Println("Failed to run client:", err)
//			return
//		}
//
//		// Retrieve the manager instance and request notification counts
//		n := notifications.From(client)
//		err := n.RequestNotifications(context.Background())
//		if err != nil {
//			panic(err)
//		}
//		fmt.Println("Notifications requested successfully")
//	}
package notifications
