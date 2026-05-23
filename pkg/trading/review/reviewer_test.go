// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package review

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/trading/reason"
)

type mockSchemaProvider struct {
	names map[string]string
}

func (m *mockSchemaProvider) GetName(sku string, useDefindex bool) string {
	if name, ok := m.names[sku]; ok {
		return name
	}

	return "Unknown Item"
}

type mockChatProvider struct {
	sentMessage       string
	sentSteamID       uint64
	sentMessageAdmins string
}

func (m *mockChatProvider) SendMessage(ctx context.Context, steamID uint64, message string) error {
	m.sentSteamID = steamID
	m.sentMessage = message
	return nil
}

func (m *mockChatProvider) MessageAdmins(ctx context.Context, message string) error {
	m.sentMessageAdmins = message
	return nil
}

type mockBotStatsProvider struct {
	totalItems    int
	backpackSlots int
	keys          float64
	ref           float64
}

func (m *mockBotStatsProvider) GetTotalItems() int {
	return m.totalItems
}

func (m *mockBotStatsProvider) GetBackpackSlots() int {
	return m.backpackSlots
}

func (m *mockBotStatsProvider) GetPureStock() (keys, ref float64) {
	return m.keys, m.ref
}

func (m *mockBotStatsProvider) GetVersion() string {
	return "1.0.0"
}

func TestReviewer_BuildSummary(t *testing.T) {
	schema := &mockSchemaProvider{
		names: map[string]string{
			"5021;6": "Mann Co. Supply Crate Key",
			"5002;6": "Refined Metal",
		},
	}

	logger := log.New(log.DefaultConfig(log.LevelError))
	reviewer := New(schema, nil, logger)

	t.Run("Standard primary reason", func(t *testing.T) {
		meta := &TradeMetadata{
			PrimaryReason: reason.DeclineEscrow,
		}

		f := SteamFormatter{}
		report := reviewer.BuildSummary(meta, f)
		assert.Equal(t, "Partner has trade hold.", report.MainReason)
		assert.Len(t, report.Details, 0)
	})

	t.Run("Detailed reasons with processor", func(t *testing.T) {
		meta := &TradeMetadata{
			PrimaryReason: reason.ReviewOverstocked,
			Reasons: []interface{ ReasonType() reason.TradeReason }{
				&ReasonOverstocked{
					BaseReason:     BaseReason{Type: reason.ReviewOverstocked, SKU: "5021;6"},
					AmountCanTrade: 5,
					AmountOffered:  10,
				},
				&ReasonInvalidItems{
					BaseReason: BaseReason{Type: reason.ReviewInvalidItems, SKU: "5002;6"},
					Price:      "no price",
				},
			},
		}

		f := SteamFormatter{}
		report := reviewer.BuildSummary(meta, f)
		assert.Equal(t, "Offer contains items that'll make us overstocked.", report.MainReason)

		require.Len(t, report.Details, 2)
		assert.Contains(t, report.Details[0], "Mann Co. Supply Crate Key (can buy 5, offered 10)")
		assert.Contains(t, report.Details[1], "Refined Metal (no price)")
	})
}

func TestReviewer_SendDeclinedAlert(t *testing.T) {
	schema := &mockSchemaProvider{
		names: map[string]string{
			"5021;6": "Mann Co. Supply Crate Key",
		},
	}
	chat := &mockChatProvider{}
	logger := log.New(log.DefaultConfig(log.LevelError))
	reviewer := New(schema, chat, logger)

	meta := &TradeMetadata{
		PrimaryReason: reason.DeclineBanned,
		Reasons: []interface{ ReasonType() reason.TradeReason }{
			&ReasonOverstocked{
				BaseReason:     BaseReason{Type: reason.ReviewOverstocked, SKU: "5021;6"},
				AmountCanTrade: 1,
				AmountOffered:  2,
			},
		},
		ProcessTimeMS: 150,
	}

	stats := &mockBotStatsProvider{
		totalItems:    50,
		backpackSlots: 100,
		keys:          12.5,
		ref:           45.33,
	}

	partnerID := id.New(76561198033830321)
	err := reviewer.SendDeclinedAlert(context.Background(), 9999, partnerID, meta, stats)
	require.NoError(t, err)

	msg := chat.sentMessageAdmins
	assert.Contains(t, msg, "Trade #9999 with 76561198033830321 declined.")
	assert.Contains(t, msg, "Reason: Partner is banned in one or more communities.")
	assert.Contains(t, msg, "Mann Co. Supply Crate Key")
	assert.Contains(t, msg, "Stock: 12.50 keys, 45.33 ref")
	assert.Contains(t, msg, "Backpack: 50/100")
	assert.Contains(t, msg, "Processed in: 150ms")
}

func TestReviewer_SendReviewAlert(t *testing.T) {
	schema := &mockSchemaProvider{
		names: map[string]string{
			"5021;6": "Mann Co. Supply Crate Key",
		},
	}
	chat := &mockChatProvider{}
	logger := log.New(log.DefaultConfig(log.LevelError))
	reviewer := New(schema, chat, logger)

	meta := &TradeMetadata{
		PrimaryReason: reason.ReviewOverstocked,
		Reasons: []interface{ ReasonType() reason.TradeReason }{
			&ReasonOverstocked{
				BaseReason:     BaseReason{Type: reason.ReviewOverstocked, SKU: "5021;6"},
				AmountCanTrade: 0,
				AmountOffered:  5,
			},
		},
		ProcessTimeMS: 50,
	}

	partnerID := id.New(76561198033830321)
	err := reviewer.SendReviewAlert(context.Background(), 1234, partnerID, meta)
	require.NoError(t, err)

	msg := chat.sentMessageAdmins
	assert.Contains(t, msg, "Manual Review Required!")
	assert.Contains(t, msg, "Offer #1234 from user 76561198033830321 is pending your decision.")
	assert.Contains(t, msg, "Main Reason: Offer contains items that'll make us overstocked.")
	assert.Contains(t, msg, "Mann Co. Supply Crate Key")
	assert.Contains(t, msg, "Commands to respond:")
	assert.Contains(t, msg, "!accept 1234")
	assert.Contains(t, msg, "!decline 1234")
	assert.Contains(t, msg, "Engine processing time: 50ms")
}

func TestFormatters(t *testing.T) {
	t.Run("SteamFormatter", func(t *testing.T) {
		f := SteamFormatter{}
		assert.Equal(t, "item", f.Item("item"))
		assert.Equal(t, "bold", f.Bold("bold"))
		assert.Equal(t, "/me header", f.Header("header"))
		assert.Equal(t, "link (url)", f.Link("link", "url"))
	})

	t.Run("WebhookFormatter", func(t *testing.T) {
		f := WebhookFormatter{}
		assert.Equal(t, "_item_", f.Item("item"))
		assert.Equal(t, "**bold**", f.Bold("bold"))
		assert.Equal(t, "### header", f.Header("header"))
		assert.Equal(t, "[link](url)", f.Link("link", "url"))
	})
}

func TestRegisterReason(t *testing.T) {
	desc := "Custom test reason"
	proc := func(raw any, s SchemaProvider, f Formatter) string {
		return "Custom string"
	}

	RegisterReason(reason.TradeReason("CUSTOM_REASON"), desc, proc)

	reg, ok := reasonRegistry[reason.TradeReason("CUSTOM_REASON")]
	assert.True(t, ok)
	assert.Equal(t, desc, reg.Description)
	assert.Equal(t, "Custom string", reg.Processor(nil, nil, nil))
}

func TestMeta_HasReason(t *testing.T) {
	m := &Meta{
		UniqueReasons: []reason.TradeReason{reason.DeclineBanned, reason.DeclineEscrow},
	}

	assert.True(t, m.HasReason(reason.DeclineBanned))
	assert.True(t, m.HasReason(reason.DeclineEscrow))
	assert.False(t, m.HasReason(reason.ReviewOverstocked))
}
