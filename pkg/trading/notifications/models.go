// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package notifications

import (
	"context"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/trading/reason"
)

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

type TradeInfo struct {
	OfferID          uint64
	PartnerSteamID   id.ID
	ReasonType       reason.TradeReason
	OldState         TradeState
	IsCanceledByUser bool
	BannedStatus     map[string]string
	HighValueNames   []string
	MissingValue     string
}

type ConfigProvider interface {
	GetTemplate(key string) string
	GetCommandPrefix() string
}

type ChatProvider interface {
	SendMessage(ctx context.Context, steamID id.ID, message string) error
}
