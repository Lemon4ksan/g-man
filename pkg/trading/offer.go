// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package trading defines foundational data structures, offer states, item schemas, and decision models for Steam trading.
package trading

import (
	"context"
	"time"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
)

// TradeOffer represents a Steam trade offer exchanged between two Steam accounts.
type TradeOffer struct {
	ID                 uint64     `json:"tradeofferid,string"`
	OtherSteamID       id.ID      `json:"accountid_other"`
	Message            string     `json:"message"`
	ExpirationTime     int64      `json:"expiration_time"`
	State              OfferState `json:"trade_offer_state"`
	ItemsToGive        []*Item    `json:"items_to_give"`
	ItemsToReceive     []*Item    `json:"items_to_receive"`
	IsOurOffer         bool       `json:"is_our_offer"`
	TimeCreated        int64      `json:"time_created"`
	TimeUpdated        int64      `json:"time_updated"`
	FromRealTimeTrade  bool       `json:"from_real_time_trade"`
	EscrowEndDate      int64      `json:"escrow_end_date"`
	ConfirmationMethod int        `json:"confirmation_method"`
}

// CreatedAt returns the creation timestamp as a time.Time.
func (o *TradeOffer) CreatedAt() time.Time { return time.Unix(o.TimeCreated, 0) }

// UpdatedAt returns the last updated timestamp as a time.Time.
func (o *TradeOffer) UpdatedAt() time.Time { return time.Unix(o.TimeUpdated, 0) }

// ExpiresAt returns the offer expiration timestamp as a time.Time.
func (o *TradeOffer) ExpiresAt() time.Time { return time.Unix(o.ExpirationTime, 0) }

// IsActive reports whether the offer is currently in the active trade offer state.
func (o *TradeOffer) IsActive() bool { return o.State == OfferStateActive }

// IsGlitched reports whether the offer is corrupted or incompletely loaded by Steam.
//
// An offer is considered glitched by Steam if:
//  1. Partner SteamID is 0.
//  2. Both ItemsToGive and ItemsToReceive are empty (regardless of whether Message is non-empty).
//  3. Any item has either Name or MarketHashName empty (Steam failed to load asset descriptions).
//
// Parity: matches @tf2autobot/tradeoffer-manager (lib/classes/TradeOffer.js:78 !item.name || !item.market_hash_name).
func (o *TradeOffer) IsGlitched() bool {
	if o.OtherSteamID == 0 || (len(o.ItemsToGive) == 0 && len(o.ItemsToReceive) == 0) {
		return true
	}

	for _, item := range o.ItemsToGive {
		if item == nil || item.Name == "" || item.MarketHashName == "" {
			return true
		}
	}

	for _, item := range o.ItemsToReceive {
		if item == nil || item.Name == "" || item.MarketHashName == "" {
			return true
		}
	}

	return false
}

// ActionType defines the verdict action to take on a trade offer.
type ActionType string

const (
	// ActionAccept indicates the offer should be accepted.
	ActionAccept ActionType = "accept"
	// ActionDecline indicates the offer should be declined.
	ActionDecline ActionType = "decline"
	// ActionCounter indicates the offer should be countered with new items.
	ActionCounter ActionType = "counter"
	// ActionSkip indicates the offer should be skipped for now without changing state.
	ActionSkip ActionType = "skip"
	// ActionReview indicates the offer requires manual operator review.
	ActionReview ActionType = "review"
	// ActionIgnore indicates the offer should be ignored without processing.
	ActionIgnore ActionType = "ignore"
)

// ActionDecision encapsulates the evaluated verdict, human-readable reason, and optional counter parameters.
type ActionDecision struct {
	Action        ActionType
	Reason        string
	CounterParams *CounterParams
}

// PartnerInventoryProvider retrieves inventory items for a trade partner.
type PartnerInventoryProvider interface {
	GetPartnerInventory(ctx context.Context, partnerID id.ID) ([]*Item, error)
}

// EscrowChecker evaluates whether an offer has an active escrow hold period.
type EscrowChecker interface {
	CheckEscrow(ctx context.Context, offer *TradeOffer) (bool, error)
}

// CounterParams defines parameters for sending a counter-offer in response to an existing offer.
type CounterParams struct {
	ItemsToGive    []*Item
	ItemsToReceive []*Item
	Message        string
	Token          string
}
