// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trading

import (
	"context"
	"time"

	"github.com/lemon4ksan/g-man/pkg/trading"
)

// TradeOffer represents a snapshot of a trade offer at a specific time.
type TradeOffer struct {
	ID                 uint64             `json:"tradeofferid,string"`
	OtherSteamID       uint64             `json:"accountid_other"`
	Message            string             `json:"message"`
	ExpirationTime     int64              `json:"expiration_time"`
	State              trading.OfferState `json:"trade_offer_state"`
	ItemsToGive        []*trading.Item    `json:"items_to_give"`
	ItemsToReceive     []*trading.Item    `json:"items_to_receive"`
	IsOurOffer         bool               `json:"is_our_offer"`
	TimeCreated        int64              `json:"time_created"`
	TimeUpdated        int64              `json:"time_updated"`
	FromRealTimeTrade  bool               `json:"from_real_time_trade"`
	EscrowEndDate      int64              `json:"escrow_end_date"`
	ConfirmationMethod int                `json:"confirmation_method"`
}

// CreatedAt returns TimeCreated as a time.Time.
func (o *TradeOffer) CreatedAt() time.Time {
	return time.Unix(o.TimeCreated, 0)
}

// UpdatedAt returns TimeUpdated as a time.Time.
func (o *TradeOffer) UpdatedAt() time.Time {
	return time.Unix(o.TimeUpdated, 0)
}

// ExpiresAt returns ExpirationTime as a time.Time.
func (o *TradeOffer) ExpiresAt() time.Time {
	return time.Unix(o.ExpirationTime, 0)
}

// IsActive returns true if the offer is in a state that can be acted upon.
func (o *TradeOffer) IsActive() bool {
	return o.State == trading.OfferStateActive
}

// IsGlitched returns true if the offer seems malformed (missing items or partner).
func (o *TradeOffer) IsGlitched() bool {
	return o.OtherSteamID == 0 || (len(o.ItemsToGive) == 0 && len(o.ItemsToReceive) == 0)
}

// ActionType defines what to do with an offer.
type ActionType string

const (
	ActionAccept  ActionType = "accept"
	ActionDecline ActionType = "decline"
	ActionCounter ActionType = "counter"
	ActionSkip    ActionType = "skip"
)

// ActionDecision is returned by your bot's business logic to tell the Processor what to do.
type ActionDecision struct {
	Action ActionType
	Reason string
	Meta   any // Custom metadata (e.g., specific missing value)
}

// OfferHandler is implemented by your main bot logic.
// The Processor will call these methods sequentially.
type OfferHandler interface {
	// ProcessOffer analyzes the offer and decides what to do.
	ProcessOffer(ctx context.Context, offer *TradeOffer) (ActionDecision, error)
	// OnActionFailed is called if the SDK completely fails to execute the action after all retries.
	OnActionFailed(ctx context.Context, offer *TradeOffer, action ActionType, reason string, err error)
}
