// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package commands provides a decoupled chat command manager for the Steam chat module.
// It automatically hooks into chat events, enforces administrator scopes using [SteamCaller],
// applies per-user rate limiting, and dispatches executed results back to the user via Steam chat.
//
// The primary orchestrator is [Manager], which implements the [module.Module] interface and wraps
// the core [command.Engine] helper. Commands are registered using the [Registry] interface, while
// the [ChatSender] interface abstracts outgoing chat communications.
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
//		"github.com/lemon4ksan/g-man/pkg/steam/social/commands"
//	)
//
//	func main() {
//		client := steam.NewClient()
//
//		// Register the manager as a module
//		commands.WithModule()(client)
//
//		// Retrieve the manager instance
//		mgr := commands.From(client)
//		mgr.Register("hello", func(ctx context.Context, senderID uint64, args []string) (string, error) {
//			return fmt.Sprintf("Hello, player %d!", senderID), nil
//		})
//	}
package commands
