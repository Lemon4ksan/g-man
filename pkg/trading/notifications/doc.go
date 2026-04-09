// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package notifications provides a flexible system for generating user-facing
chat messages based on trade outcomes.

# Template-based Messaging

The package leverages Go's 'text/template' engine to allow highly customizable
and brandable notifications. This enables bot owners to change the wording,
formatting (e.g., using Steam's /pre tag), or localization of messages without
modifying the bot's core logic.

# Key Features

  - Dynamic Context: Data from the trade (e.g., missing value, item names,
    ban status) is automatically injected into templates.
  - Fallback Mechanism: The package provides a comprehensive set of default
    English templates for all common trade scenarios (Accept, Decline, Escrow, etc.).
  - Decoupled Delivery: The 'Manager' uses a 'ChatProvider' interface, making
    it compatible with any messaging service (Steam Chat, Discord, etc.).
*/
package notifications
