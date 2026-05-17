// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package trading provides a generic, game-agnostic trade reasoning engine for Steam.

The package is designed to be highly extensible and reusable across different Steam games
(TF2, CS2, Dota 2, etc.) by following a middleware-based architecture and using
plug-and-play components for game-specific logic.

# Core Components

  - Engine: Orchestrates the execution of a middleware pipeline for trade offers.
  - Middleware: Encapsulates individual pieces of trading logic (e.g., ban checks, escrow, valuation).
  - TradeContext: Carries the state and metadata of a trade through the pipeline.
  - Reason: A set of common, game-agnostic reasons for trade decisions (Accept, Decline, Review).

# Separation of Responsibilities

To maintain game-agnosticism, this package provides only the foundation. Game-specific
logic should be implemented in separate packages (e.g., pkg/tf2/trading).

 1. Trade Reasons: Generic reasons are defined in pkg/trading/reason. Game-specific
    reasons should be defined in the game's own reason package (e.g., pkg/tf2/reason).
 2. Notification Templates: Common templates are registered in notifications/defaults.go.
    Game-specific templates should be registered via RegisterDefaultTemplate() during
    the game module's initialization.
 3. Review Formatting: The review registry can be extended using review.RegisterReason()
    to provide human-readable descriptions and links for game-specific reasons.

# Web Manager

The web.Manager handles the low-level Steam trade offer API interactions. It is configured
via web.Config, which must specify the AppID and ContextID for the target game, ensuring
correct inventory fetching and offer processing.
*/
package trading
