// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package review handles detailed reporting and alerting for bot administrators.
It translates raw trade reasoning into human-readable summaries.

# Reporting and Alerts

When an offer is automatically declined or flagged for manual review, the
Reviewer generates a detailed report. These reports include:
  - The primary reason for the decision.
  - Granular lists of problematic items (overstocked, disabled, or duped).
  - Bot statistics (current stock levels, backpack slots).
  - Actionable commands (e.g., "!accept <id>") for manual intervention.

# Presentation Layer

The package uses a 'Formatter' strategy to support different output styles.
For example, the 'SteamFormatter' produces plain text optimized for Steam chat,
while the 'WebhookFormatter' generates Markdown-formatted blocks for Discord
or Slack integrations.

# Registry Pattern

Reasoning logic is decoupled from presentation via a centralized 'Registry'.
Adding support for new game-specific item checks (like CS2 wear levels or
Dota 2 gems) involves simply registering a new 'ReasonProcessor'.
*/
package review
