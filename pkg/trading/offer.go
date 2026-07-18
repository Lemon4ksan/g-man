// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package trading defines models and structures for Steam trade offers.
package trading

import (
	"context"
	"time"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
)

// TradeOffer represents a snapshot of a trade offer at a specific time.
//
// Active offers are managed and polled via [web.Manager].
type TradeOffer struct {
	// ID is the unique transaction identifier assigned by Steam.
	ID uint64 `json:"tradeofferid,string"`
	// OtherSteamID is the 64-bit Steam identifier of the trade partner.
	OtherSteamID id.ID `json:"accountid_other"`
	// Message is the optional text message attached to the trade offer.
	Message string `json:"message"`
	// ExpirationTime is the Unix timestamp indicating when the offer expires.
	ExpirationTime int64 `json:"expiration_time"`
	// State represents the current lifecycle stage of the trade offer.
	State OfferState `json:"trade_offer_state"`
	// ItemsToGive is the list of items from our inventory requested by the partner.
	ItemsToGive []*Item `json:"items_to_give"`
	// ItemsToReceive is the list of items from the partner's inventory offered to us.
	ItemsToReceive []*Item `json:"items_to_receive"`
	// IsOurOffer is true if the offer was initiated and sent by our account.
	IsOurOffer bool `json:"is_our_offer"`
	// TimeCreated is the Unix timestamp indicating when the offer was created.
	TimeCreated int64 `json:"time_created"`
	// TimeUpdated is the Unix timestamp indicating when the offer was last modified.
	TimeUpdated int64 `json:"time_updated"`
	// FromRealTimeTrade is true if the offer originated from an active live trade session.
	FromRealTimeTrade bool `json:"from_real_time_trade"`
	// EscrowEndDate is the Unix timestamp indicating when the escrow hold period ends.
	EscrowEndDate int64 `json:"escrow_end_date"`
	// ConfirmationMethod is the mechanism required to finalize the trade (e.g. mobile app).
	ConfirmationMethod int `json:"confirmation_method"`
}

// CreatedAt returns the creation time of the offer as a [time.Time] value.
func (o *TradeOffer) CreatedAt() time.Time {
	return time.Unix(o.TimeCreated, 0)
}

// UpdatedAt returns the last modification time of the offer as a [time.Time] value.
func (o *TradeOffer) UpdatedAt() time.Time {
	return time.Unix(o.TimeUpdated, 0)
}

// ExpiresAt returns the expiration time of the offer as a [time.Time] value.
func (o *TradeOffer) ExpiresAt() time.Time {
	return time.Unix(o.ExpirationTime, 0)
}

// IsActive reports whether the offer is currently in an active, actionable state.
func (o *TradeOffer) IsActive() bool {
	return o.State == OfferStateActive
}

// IsGlitched reports whether the offer is malformed due to missing items or partner information.
func (o *TradeOffer) IsGlitched() bool {
	return o.OtherSteamID == 0 || (len(o.ItemsToGive) == 0 && len(o.ItemsToReceive) == 0)
}

// ActionType defines the decision made by the reasoning engine for a trade offer.
type ActionType string

const (
	// ActionAccept tells the processor to accept the offer.
	ActionAccept ActionType = "accept"
	// ActionDecline tells the processor to decline the offer.
	ActionDecline ActionType = "decline"
	// ActionCounter tells the processor to counter the offer with different items.
	ActionCounter ActionType = "counter"
	// ActionSkip tells the processor to skip the offer for now.
	ActionSkip ActionType = "skip"
	// ActionReview marks the trade for manual review.
	ActionReview ActionType = "review"
	// ActionIgnore means the offer should be ignored.
	ActionIgnore ActionType = "ignore"
)

// ActionDecision defines the final resolution payload dispatched by the automated processor.
type ActionDecision struct {
	// Action is the specific operations to perform on the offer (e.g., accept, decline).
	Action ActionType
	// Reason is the textual justification explaining why this decision was made.
	Reason string
	// CounterParams specifies the items for counter-offering when the action is [ActionCounter].
	CounterParams *CounterParams
}

// PartnerInventoryProvider defines the interface for fetching the inventory of a trade partner.
type PartnerInventoryProvider interface {
	// GetPartnerInventory retrieves a slice of items owned by the specified partner ID.
	GetPartnerInventory(ctx context.Context, partnerID id.ID) ([]*Item, error)
}

// EscrowChecker defines the interface for checking trade hold delays.
type EscrowChecker interface {
	// CheckEscrow reports whether completing this trade offer results in an escrow delay.
	CheckEscrow(ctx context.Context, offer *TradeOffer) (bool, error)
}

// CounterParams defines the payload parameters required to execute a counter-offer.
type CounterParams struct {
	// ItemsToGive is the modified list of items from our inventory we agree to trade.
	ItemsToGive []*Item
	// ItemsToReceive is the modified list of items from the partner's inventory we request.
	ItemsToReceive []*Item
	// Message is the text message attached to the counter-offer.
	Message string
	// Token is the optional trade token of the target partner.
	Token string
}
