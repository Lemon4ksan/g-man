// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package review provides advanced offer reviewer for bot integration.
package review

import (
	"context"
	"slices"

	"github.com/lemon4ksan/g-man/pkg/trading/reason"
)

// BaseReason contains basic information about the reason for the hold.
type BaseReason struct {
	Type reason.TradeReason
	SKU  string
}

// ReasonType returns the reason type.
func (b BaseReason) ReasonType() reason.TradeReason { return b.Type }

// Specific reasons with their own fields.

// ReasonOverstocked indicates that the offer would exceed our stock limits.
type ReasonOverstocked struct {
	BaseReason
	AmountCanTrade int
	AmountOffered  int
}

// ReasonInvalidItems indicates that some items in the offer are not in our pricelist.
type ReasonInvalidItems struct {
	BaseReason
	Price string
}

// ReasonDuped indicates that an item appears to be duplicated according to history.
type ReasonDuped struct {
	BaseReason
	AssetID string
}

// ReasonUnderstocked indicates that we don't have enough items to fulfill the trade.
type ReasonUnderstocked struct {
	BaseReason
	AmountCanTrade int
	AmountTaking   int
}

// ReasonInvalidValue indicates that the offer value is incorrect.
type ReasonInvalidValue struct {
	BaseReason
	Diff    float64
	DiffRef float64
	DiffKey string
}

// ReasonDisabledItems indicates that some items in the offer are currently disabled for trading.
type ReasonDisabledItems struct {
	BaseReason
}

// Meta contains summary information about the reasons for the trade hold.
type Meta struct {
	UniqueReasons []reason.TradeReason
	Reasons       []interface{ ReasonType() reason.TradeReason }
}

// HasReason returns true if the meta contains the specified reason type.
func (m *Meta) HasReason(reasonType reason.TradeReason) bool {
	return slices.Contains(m.UniqueReasons, reasonType)
}

// Content contains generated texts for logs and chat.
type Content struct {
	Notes        []string
	ItemNamesOur map[string][]string // key: reason type, value: list of strings
	Missing      string
}

// Bot dependency interfaces

// SchemaProvider provides item name resolution from the schema.
type SchemaProvider interface {
	GetName(sku string, useDefindex bool) string
}

// ChatProvider handles sending messages to users and admins.
type ChatProvider interface {
	SendMessage(ctx context.Context, steamID uint64, message string) error
	MessageAdmins(ctx context.Context, message string) error
}

// PricelistProvider provides current key prices.
type PricelistProvider interface {
	GetKeyPrices() (buy, sell float64)
}

// ConfigProvider provides access to review-related configuration.
type ConfigProvider interface {
	GetReviewTemplate(reasonType reason.TradeReason) string
	IsWebhookEnabled() bool
}

// TradeMetadata stores offer metadata.
type TradeMetadata struct {
	PrimaryReason       reason.TradeReason                             // Main reason for rejection
	UniqueReasons       []string                                       // All triggered reasons
	Reasons             []interface{ ReasonType() reason.TradeReason } // Detailed reasons by subject
	BannedStatus        map[string]string                              // Ban check results, for example: {"SteamREP": "clean"}
	HighValueNamesOur   []string
	HighValueNamesTheir []string
	ProcessTimeMS       int64 // Offer processing time in milliseconds
	IsOfferSent         bool  // True if we created the offer, False if it was sent to us
}

// DeclinedSummary stores formatted lists for output.
type DeclinedSummary struct {
	ReasonDescription   string
	InvalidItems        []string
	DisabledItems       []string
	Overstocked         []string
	Understocked        []string
	DupedItems          []string
	HighNotSellingItems []string
	HighValue           []string
}

// BotStatsProvider provides various bot statistics.
type BotStatsProvider interface {
	GetTotalItems() int
	GetBackpackSlots() int
	GetPureStock() (keys, ref float64)
	GetVersion() string
}

// AutokeysProvider provides status of the autokeys banking system.
type AutokeysProvider interface {
	IsEnabled() bool
	IsActive() bool
	GetStatus() string // "banking", "buying", "selling"
}
