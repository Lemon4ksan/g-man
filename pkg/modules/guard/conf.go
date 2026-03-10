// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package guard

import (
	"fmt"
	"time"
)

type ConfirmationsList struct {
	Success       bool            `json:"success"`
	Confirmations []*Confirmation `json:"conf"`
	Message       string          `json:"message"`
	Detail        string          `json:"detail"`
	NeedAuth      bool            `json:"needauth"`
}

// ConfirmationType represents the type of Steam Guard confirmation.
// Different confirmation types can be handled differently (e.g., auto-accept trades).
type ConfirmationType int

const (
	// ConfTypeGeneric is a catch-all for unknown confirmation types.
	// Rarely used in practice.
	ConfTypeGeneric ConfirmationType = iota

	// ConfTypeTrade represents a trade offer confirmation.
	// Generated when someone sends you a trade offer or you send one.
	// These are the most common confirmations for trading bots.
	ConfTypeTrade

	// ConfTypeMarket represents a Steam Community Market listing confirmation.
	// Generated when listing or buying items on the market.
	ConfTypeMarket

	// ConfTypeLogin represents a login from a new device confirmation.
	// Generated when someone tries to log in from an unrecognized device.
	ConfTypeLogin

	// ConfTypeAccountChange represents account settings changes.
	// Generated for sensitive actions like password changes, email changes, etc.
	ConfTypeAccountChange
)

func (ct ConfirmationType) String() string {
	switch ct {
	case ConfTypeGeneric:
		return "generic"
	case ConfTypeTrade:
		return "trade"
	case ConfTypeMarket:
		return "market"
	case ConfTypeLogin:
		return "login"
	case ConfTypeAccountChange:
		return "account_change"
	default:
		return "unknown"
	}
}

// Confirmation represents a Steam Guard mobile confirmation.
// This struct mirrors the JSON response from Steam's mobileconf endpoints.
type Confirmation struct {
	// ID is the unique confirmation identifier
	// Used when accepting/rejecting the confirmation
	ID uint64 `json:"id,string"`

	// Nonce is a cryptographic nonce required for confirmation responses
	// Must be sent back when accepting/rejecting
	Nonce uint64 `json:"nonce,string"`

	// CreatorID is the SteamID of the user who created this confirmation
	// Usually your own SteamID, but can be different for shared accounts
	CreatorID uint64 `json:"creator_id,string"`

	// Type indicates what kind of confirmation this is (trade, market, etc.)
	Type ConfirmationType `json:"type"`

	// Title is a human-readable description (e.g., "Trade with John")
	Title string `json:"title"`

	// Receiving describes what items are being received (for trades)
	// Format: "+ Item1, + Item2" or "- Item1, - Item2"
	Receiving string `json:"receiving"`

	// Time is when the confirmation was created (format: "HH:MM")
	Time string `json:"time"`

	// Icon is a URL to an icon representing the confirmation type
	Icon string `json:"icon"`

	// Requester is the person/entity requesting the action (if applicable)
	Requester string `json:"requester,omitempty"`

	// expiresAt is calculated internally, not from JSON
	expiresAt time.Time
}

// IsExpired checks if the confirmation has passed its expiration time.
// Steam confirmations expire 2 minutes after they are created.
// Expired confirmations cannot be accepted/rejected.
func (c *Confirmation) IsExpired() bool {
	return !c.expiresAt.IsZero() && time.Now().After(c.expiresAt)
}

// TimeRemaining returns the duration until the confirmation expires.
// Returns a negative duration if already expired.
func (c *Confirmation) TimeRemaining() time.Duration {
	if c.expiresAt.IsZero() {
		return 2 * time.Minute // Default fallback
	}
	return time.Until(c.expiresAt)
}

// String returns a human-readable representation of the confirmation.
func (c *Confirmation) String() string {
	return fmt.Sprintf("Confirmation{ID=%d, Type=%s, Title=%q, ExpiresIn=%v}",
		c.ID, c.Type, truncate(c.Title, 20), c.TimeRemaining())
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
