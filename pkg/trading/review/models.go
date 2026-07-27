// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package review

import (
	"context"
	"slices"

	"github.com/lemon4ksan/g-man/pkg/trading/reason"
)

type BaseReason struct {
	Type reason.TradeReason
	SKU  string
}

func (b BaseReason) ReasonType() reason.TradeReason { return b.Type }

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

type Meta struct {
	UniqueReasons []reason.TradeReason
	Reasons       []interface{ ReasonType() reason.TradeReason }
}

func (m *Meta) HasReason(reasonType reason.TradeReason) bool {
	return slices.Contains(m.UniqueReasons, reasonType)
}

type Content struct {
	Notes        []string
	ItemNamesOur map[string][]string
	Missing      string
}

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

type TradeMetadata struct {
	PrimaryReason       reason.TradeReason
	UniqueReasons       []string
	Reasons             []interface{ ReasonType() reason.TradeReason }
	BannedStatus        map[string]string
	HighValueNamesOur   []string
	HighValueNamesTheir []string
	ProcessTimeMS       int64
	IsOfferSent         bool
}

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

type BotStatsProvider interface {
	GetTotalItems() int
	GetBackpackSlots() int
	GetPureStock() (keys, ref float64)
	GetVersion() string
}

type AutokeysProvider interface {
	IsEnabled() bool
	IsActive() bool
	GetStatus() string
}
