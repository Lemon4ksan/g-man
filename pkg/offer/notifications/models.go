// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package notifications

import (
	"context"

	"github.com/lemon4ksan/g-man/pkg/offer/reason"
)

// TradeState represents the state of the offer as passed from the offer manager.
type TradeState int

const (
	StateInvalid TradeState = iota
	StateActive
	StateAccepted
	StateCountered
	StateExpired
	StateCanceled
	StateDeclined
	StateInvalidItems
	StateCreatedNeedsConfirmation
	StateCanceledBySecondFactor
	StateInEscrow
)

// TradeInfo contains the information needed to generate a notification.
type TradeInfo struct {
	OfferID              uint64
	PartnerSteamID       uint64
	ReasonType           reason.TradeReason
	IsCanceledByUser     bool
	OldState             TradeState
	DiffValueRef         float64
	DiffValueKey         string
	SellKeyPriceRef      float64
	BannedStatus         map[string]string
	HighValueNames       []string
	ManualReviewDisabled bool
}

// ConfigProvider describes the configuration interface for notifications.
type ConfigProvider interface {
	GetCustomMessage(key string) string
	GetCommandPrefix() string
}

// ChatProvider for sending messages to a partner.
type ChatProvider interface {
	SendMessage(ctx context.Context, steamID uint64, message string) error
}
