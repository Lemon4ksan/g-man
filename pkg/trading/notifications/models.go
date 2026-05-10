// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package notifications

import (
	"context"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/trading/reason"
)

// TradeState represents the state of the offer as passed from the offer manager.
type TradeState int

const (
	// StateInvalid means the offer is invalid.
	StateInvalid TradeState = iota
	// StateActive means the offer is active.
	StateActive
	// StateAccepted means the offer is accepted.
	StateAccepted
	// StateCountered means the offer is countered.
	StateCountered
	// StateExpired means the offer is expired.
	StateExpired
	// StateCanceled means the offer is canceled.
	StateCanceled
	// StateDeclined means the offer is declined.
	StateDeclined
	// StateInvalidItems means the offer has invalid items.
	StateInvalidItems
	// StateCreatedNeedsConfirmation means the offer was created and needs confirmation.
	StateCreatedNeedsConfirmation
	// StateCanceledBySecondFactor means the offer was canceled by second factor.
	StateCanceledBySecondFactor
	// StateInEscrow means the offer is in escrow.
	StateInEscrow
)

// TradeInfo contains the information needed to generate a notification.
type TradeInfo struct {
	OfferID        uint64
	PartnerSteamID id.ID
	ReasonType     reason.TradeReason
	OldState       TradeState

	// Data for templates
	IsCanceledByUser bool
	BannedStatus     map[string]string
	HighValueNames   []string
	MissingValue     string // e.g., "1.33 ref" or "1 key, 2 ref"
}

// ConfigProvider provides notification templates and global settings.
type ConfigProvider interface {
	// GetTemplate returns the template string for a given key.
	// Example keys: "success", "success_escrow", "decline.escrow".
	GetTemplate(key string) string

	// GetCommandPrefix returns the chat command prefix (e.g., "!").
	GetCommandPrefix() string
}

// ChatProvider defines an interface for sending messages to a Steam user.
type ChatProvider interface {
	SendMessage(ctx context.Context, steamID id.ID, message string) error
}
