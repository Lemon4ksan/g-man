// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package notifications

import (
	"sync"

	"github.com/lemon4ksan/g-man/pkg/trading/reason"
)

var (
	templatesMu      sync.RWMutex
	defaultTemplates = map[string]string{
		// Success
		"success":        "/pre ✅ Success! The trade offer has been accepted.",
		"success_escrow": "✅ The trade was accepted, but your items are held in escrow by Steam. Please enable the Steam Guard Mobile Authenticator to avoid this in the future.",

		// Canceled
		"cancel.by_user": "/pre ❌ The offer is no longer available because it was canceled by the user.",
		"cancel.generic": "/pre ❌ The offer is no longer available. This can happen due to Steam issues. Please try again.",

		// Invalid
		"invalid_trade": "/pre ❌ This trade is no longer valid. The items may have been traded away.",

		// Declines (General)
		"decline.general":                          "/pre ❌ Your trade offer has been declined.",
		"decline." + reason.DeclineManual.String(): "/pre ❌ Your trade offer has been declined by the owner.",
		"decline." + reason.DeclineEscrow.String(): "/pre ❌ Your offer was declined because it would result in a trade hold (escrow). Please enable the Steam Guard Mobile Authenticator.",

		// Declines (Value & Items)
		"decline." + reason.ReviewInvalidItems.String():   "/pre ❌ Your offer was declined because it contains items I am not currently trading for.",
		"decline." + reason.ReviewOverstocked.String():    "/pre ❌ Your offer was declined because I am overstocked on the items you are offering.",
		"decline." + reason.ReviewUnderstocked.String():   "/pre ❌ Your offer was declined because I am understocked on the items you are requesting.",
		"decline." + reason.DeclineBlacklisted.String():   "/pre ❌ You are blacklisted from using this bot.",
		"decline." + reason.DeclineBegging.String():       "/pre ❌ You are asking for items for free. Please provide equivalent value.",
		"decline." + reason.DeclineInternalError.String(): "/pre ❌ An internal error occurred while processing your trade. Please try again in a few minutes.",
	}
)

// RegisterDefaultTemplate adds or updates a fallback template.
// This allows game-specific packages (like tf2) to register their own reasons.
func RegisterDefaultTemplate(key, content string) {
	templatesMu.Lock()
	defer templatesMu.Unlock()

	defaultTemplates[key] = content
}

// GetDefaultTemplate retrieves a fallback template by key.
func GetDefaultTemplate(key string) string {
	templatesMu.RLock()
	defer templatesMu.RUnlock()
	return defaultTemplates[key]
}
