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

func (b BaseReason) ReasonType() reason.TradeReason { return b.Type }

// Specific reasons with their own fields.

type ReasonOverstocked struct {
	BaseReason
	AmountCanTrade int
	AmountOffered  int
}

type ReasonInvalidItems struct {
	BaseReason
	Price string
}

type ReasonDuped struct {
	BaseReason
	AssetID string
}

type ReasonUnderstocked struct {
	BaseReason
	AmountCanTrade int
	AmountTaking   int
}

type ReasonInvalidValue struct {
	BaseReason
	Diff    float64
	DiffRef float64
	DiffKey string
}

type ReasonDisabledItems struct {
	BaseReason
}

// Meta contains summary information about the reasons for the trade hold.
type Meta struct {
	UniqueReasons []reason.TradeReason
	Reasons       []interface{ ReasonType() reason.TradeReason }
}

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

type SchemaProvider interface {
	GetName(sku string, useDefindex bool) string
}

type ChatProvider interface {
	SendMessage(ctx context.Context, steamID uint64, message string) error
	MessageAdmins(ctx context.Context, message string) error
}

type PricelistProvider interface {
	GetKeyPrices() (buy, sell float64)
}

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

// Additional interfaces for collecting bot statistics.
type BotStatsProvider interface {
	GetTotalItems() int
	GetBackpackSlots() int
	GetPureStock() (keys, ref float64)
	GetVersion() string
}

type AutokeysProvider interface {
	IsEnabled() bool
	IsActive() bool
	GetStatus() string // "banking", "buying", "selling"
}
