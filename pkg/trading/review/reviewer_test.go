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

func setupReviewer(t *testing.T) (*Reviewer, *mockSchemaProvider, *mockChatProvider) {
	t.Helper()

	schema := &mockSchemaProvider{
		names: map[string]string{
			"5021;6": "Mann Co. Supply Crate Key",
			"5002;6": "Refined Metal",
		},
	}
	chat := &mockChatProvider{}
	logger := log.New(log.DefaultConfig(log.LevelError))
	reviewer := New(schema, chat, logger)

	return reviewer, schema, chat
}

func TestBuildSummary_VariousMetadata_GeneratesExpectedReport(t *testing.T) {
	t.Parallel()

	t.Run("standard_primary_reason", func(t *testing.T) {
		t.Parallel()

		reviewer, _, _ := setupReviewer(t)
		meta := &TradeMetadata{
			PrimaryReason: reason.DeclineEscrow,
		}

		f := SteamFormatter{}
		report := reviewer.BuildSummary(meta, f)
		assert.Equal(t, "Partner has trade hold.", report.MainReason)
		assert.Empty(t, report.Details)
	})

	t.Run("detailed_reasons_with_processor", func(t *testing.T) {
		t.Parallel()

		reviewer, _, _ := setupReviewer(t)
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

func TestSendDeclinedAlert_ValidMetadata_SendsAdminsReport(t *testing.T) {
	t.Parallel()

	reviewer, _, chat := setupReviewer(t)

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
	err := reviewer.SendDeclinedAlert(t.Context(), 9999, partnerID, meta, stats)
	require.NoError(t, err)

	msg := chat.sentMessageAdmins
	assert.Contains(t, msg, "Trade #9999 with 76561198033830321 declined.")
	assert.Contains(t, msg, "Reason: Partner is banned in one or more communities.")
	assert.Contains(t, msg, "Mann Co. Supply Crate Key")
	assert.Contains(t, msg, "Stock: 12.50 keys, 45.33 ref")
	assert.Contains(t, msg, "Backpack: 50/100")
	assert.Contains(t, msg, "Processed in: 150ms")
}

func TestSendReviewAlert_ValidMetadata_SendsAdminsReport(t *testing.T) {
	t.Parallel()

	reviewer, _, chat := setupReviewer(t)

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
	err := reviewer.SendReviewAlert(t.Context(), 1234, partnerID, meta)
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

func TestFormatters_DifferentPlatforms_FormatsCorrectly(t *testing.T) {
	t.Parallel()

	t.Run("steam_formatter", func(t *testing.T) {
		t.Parallel()

		f := SteamFormatter{}
		assert.Equal(t, "item", f.Item("item"))
		assert.Equal(t, "bold", f.Bold("bold"))
		assert.Equal(t, "/me header", f.Header("header"))
		assert.Equal(t, "link (url)", f.Link("link", "url"))
	})

	t.Run("webhook_formatter", func(t *testing.T) {
		t.Parallel()

		f := WebhookFormatter{}
		assert.Equal(t, "_item_", f.Item("item"))
		assert.Equal(t, "**bold**", f.Bold("bold"))
		assert.Equal(t, "### header", f.Header("header"))
		assert.Equal(t, "[link](url)", f.Link("link", "url"))
	})
}

func TestRegisterReason_CustomReasonAndProcessor_RegistersSuccessfully(t *testing.T) {
	// Note: We deliberately do NOT call t.Parallel() here.
	// This test modifies a global map (reasonRegistry) and could cause races
	// if executed concurrently with other tests reading from it.
	customReason := reason.TradeReason("CUSTOM_REASON")
	desc := "Custom test reason"
	proc := func(raw any, s SchemaProvider, f Formatter) string {
		return "Custom string"
	}

	RegisterReason(customReason, desc, proc)

	t.Cleanup(func() {
		delete(reasonRegistry, customReason)
	})

	reg, ok := reasonRegistry[customReason]
	assert.True(t, ok)
	assert.Equal(t, desc, reg.Description)
	assert.Equal(t, "Custom string", reg.Processor(nil, nil, nil))
}

func TestMeta_HasReason_VariousScenarios_ReturnsExpectedResult(t *testing.T) {
	t.Parallel()

	m := &Meta{
		UniqueReasons: []reason.TradeReason{reason.DeclineBanned, reason.DeclineEscrow},
	}

	assert.True(t, m.HasReason(reason.DeclineBanned))
	assert.True(t, m.HasReason(reason.DeclineEscrow))
	assert.False(t, m.HasReason(reason.ReviewOverstocked))
}
