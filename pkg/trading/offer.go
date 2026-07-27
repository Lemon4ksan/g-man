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

func (o *TradeOffer) CreatedAt() time.Time { return time.Unix(o.TimeCreated, 0) }
func (o *TradeOffer) UpdatedAt() time.Time { return time.Unix(o.TimeUpdated, 0) }
func (o *TradeOffer) ExpiresAt() time.Time { return time.Unix(o.ExpirationTime, 0) }

func (o *TradeOffer) IsActive() bool { return o.State == OfferStateActive }

func (o *TradeOffer) IsGlitched() bool {
	return o.OtherSteamID == 0 || (len(o.ItemsToGive) == 0 && len(o.ItemsToReceive) == 0)
}

type ActionType string

const (
	ActionAccept  ActionType = "accept"
	ActionDecline ActionType = "decline"
	ActionCounter ActionType = "counter"
	ActionSkip    ActionType = "skip"
	ActionReview  ActionType = "review"
	ActionIgnore  ActionType = "ignore"
)

type ActionDecision struct {
	Action        ActionType
	Reason        string
	CounterParams *CounterParams
}

type PartnerInventoryProvider interface {
	GetPartnerInventory(ctx context.Context, partnerID id.ID) ([]*Item, error)
}

type EscrowChecker interface {
	CheckEscrow(ctx context.Context, offer *TradeOffer) (bool, error)
}

type CounterParams struct {
	ItemsToGive    []*Item
	ItemsToReceive []*Item
	Message        string
	Token          string
}
