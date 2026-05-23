// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package notifications

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/trading/reason"
)

// mockChatProvider implements ChatProvider.
type mockChatProvider struct {
	mu           sync.Mutex
	sentMessages map[id.ID][]string
	sendErr      error
}

func newMockChatProvider() *mockChatProvider {
	return &mockChatProvider{
		sentMessages: make(map[id.ID][]string),
	}
}

func (m *mockChatProvider) SendMessage(ctx context.Context, steamID id.ID, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.sendErr != nil {
		return m.sendErr
	}

	m.sentMessages[steamID] = append(m.sentMessages[steamID], message)

	return nil
}

// mockConfigProvider implements ConfigProvider.
type mockConfigProvider struct {
	mu            sync.Mutex
	templates     map[string]string
	commandPrefix string
}

func newMockConfigProvider() *mockConfigProvider {
	return &mockConfigProvider{
		templates:     make(map[string]string),
		commandPrefix: "!",
	}
}

func (m *mockConfigProvider) GetTemplate(key string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.templates[key]
}

func (m *mockConfigProvider) GetCommandPrefix() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.commandPrefix
}

func (m *mockConfigProvider) SetTemplate(key, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.templates[key] = content
}

func TestManager_SendNotification_Success(t *testing.T) {
	chat := newMockChatProvider()
	cfg := newMockConfigProvider()
	logger := log.New(log.DefaultConfig(log.LevelError))

	mgr := NewManager(chat, cfg, logger)

	partner := id.ID(76561198000000001)

	// Test StateAccepted
	info := &TradeInfo{
		OfferID:        1001,
		PartnerSteamID: partner,
		OldState:       StateAccepted,
	}

	err := mgr.SendNotification(context.Background(), info)
	require.NoError(t, err)

	assert.Contains(t, chat.sentMessages[partner][0], "Success! The trade offer has been accepted")
}

func TestManager_SendNotification_Escrow(t *testing.T) {
	chat := newMockChatProvider()
	cfg := newMockConfigProvider()
	logger := log.New(log.DefaultConfig(log.LevelError))

	mgr := NewManager(chat, cfg, logger)

	partner := id.ID(76561198000000002)

	info := &TradeInfo{
		OfferID:        1002,
		PartnerSteamID: partner,
		OldState:       StateInEscrow,
	}

	err := mgr.SendNotification(context.Background(), info)
	require.NoError(t, err)

	assert.Contains(t, chat.sentMessages[partner][0], "held in escrow by Steam")
}

func TestManager_SendNotification_Invalid(t *testing.T) {
	chat := newMockChatProvider()
	cfg := newMockConfigProvider()
	logger := log.New(log.DefaultConfig(log.LevelError))

	mgr := NewManager(chat, cfg, logger)

	partner := id.ID(76561198000000003)

	info := &TradeInfo{
		OfferID:        1003,
		PartnerSteamID: partner,
		OldState:       StateInvalid,
	}

	err := mgr.SendNotification(context.Background(), info)
	require.NoError(t, err)

	assert.Contains(t, chat.sentMessages[partner][0], "This trade is no longer valid")
}

func TestManager_SendNotification_Canceled(t *testing.T) {
	chat := newMockChatProvider()
	cfg := newMockConfigProvider()
	logger := log.New(log.DefaultConfig(log.LevelError))

	mgr := NewManager(chat, cfg, logger)
	partner := id.ID(76561198000000004)

	// Scenario: canceled by user
	infoUser := &TradeInfo{
		OfferID:          1004,
		PartnerSteamID:   partner,
		OldState:         StateCanceled,
		IsCanceledByUser: true,
	}
	err := mgr.SendNotification(context.Background(), infoUser)
	require.NoError(t, err)
	assert.Contains(t, chat.sentMessages[partner][0], "canceled by the user")

	// Scenario: canceled generically
	infoGeneric := &TradeInfo{
		OfferID:          1005,
		PartnerSteamID:   partner,
		OldState:         StateCanceled,
		IsCanceledByUser: false,
	}
	err = mgr.SendNotification(context.Background(), infoGeneric)
	require.NoError(t, err)
	assert.Contains(t, chat.sentMessages[partner][1], "due to Steam issues")
}

func TestManager_SendNotification_Declined(t *testing.T) {
	logger := log.New(log.DefaultConfig(log.LevelError))
	partner := id.ID(76561198000000005)

	tests := []struct {
		name          string
		reason        reason.TradeReason
		expectContent string
	}{
		{
			name:          "Decline Manual",
			reason:        reason.DeclineManual,
			expectContent: "declined by the owner",
		},
		{
			name:          "Decline Escrow",
			reason:        reason.DeclineEscrow,
			expectContent: "result in a trade hold",
		},
		{
			name:          "Decline Begging",
			reason:        reason.DeclineBegging,
			expectContent: "asking for items for free",
		},
		{
			name:          "Decline Unknown Reason - Fallback",
			reason:        reason.TradeReason("UNKNOWN_REASON"),
			expectContent: "Your trade offer has been declined",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chat := newMockChatProvider()
			cfg := newMockConfigProvider()
			mgr := NewManager(chat, cfg, logger)

			info := &TradeInfo{
				OfferID:        1006,
				PartnerSteamID: partner,
				OldState:       StateDeclined,
				ReasonType:     tt.reason,
			}

			err := mgr.SendNotification(context.Background(), info)
			require.NoError(t, err)

			assert.Contains(t, chat.sentMessages[partner][0], tt.expectContent)
		})
	}
}

func TestManager_SendNotification_ConfigOverrides(t *testing.T) {
	chat := newMockChatProvider()
	cfg := newMockConfigProvider()
	logger := log.New(log.DefaultConfig(log.LevelError))

	mgr := NewManager(chat, cfg, logger)

	partner := id.ID(76561198000000006)

	// Set custom template and command prefix override
	cfg.SetTemplate(
		"success",
		"Trade #{{.OfferID}} accepted successfully! Partner: {{.PartnerSteamID}}. Prefix is: {{prefix}}",
	)
	cfg.commandPrefix = "/"

	info := &TradeInfo{
		OfferID:        7777,
		PartnerSteamID: partner,
		OldState:       StateAccepted,
	}

	err := mgr.SendNotification(context.Background(), info)
	require.NoError(t, err)

	expected := "Trade #7777 accepted successfully! Partner: 76561198000000006. Prefix is: /"
	assert.Equal(t, expected, chat.sentMessages[partner][0])
}

func TestManager_SendNotification_TemplateParseError(t *testing.T) {
	chat := newMockChatProvider()
	cfg := newMockConfigProvider()
	logger := log.New(log.DefaultConfig(log.LevelError))

	mgr := NewManager(chat, cfg, logger)

	partner := id.ID(76561198000000007)

	// Set invalid template syntax (missing closing bracket)
	cfg.SetTemplate("success", "Trade #{{.OfferID")

	info := &TradeInfo{
		OfferID:        8888,
		PartnerSteamID: partner,
		OldState:       StateAccepted,
	}

	// Should handle the error gracefully by sending a fallback message to the user
	err := mgr.SendNotification(context.Background(), info)
	require.NoError(t, err)

	assert.Contains(t, chat.sentMessages[partner][0], "internal error occurred while generating a response")
}

func TestManager_SendNotification_TemplateExecutionError(t *testing.T) {
	chat := newMockChatProvider()
	cfg := newMockConfigProvider()
	logger := log.New(log.DefaultConfig(log.LevelError))

	mgr := NewManager(chat, cfg, logger)

	partner := id.ID(76561198000000008)

	// Template referencing a field that does not exist in TradeInfo
	cfg.SetTemplate("success", "Trade #{{.NonExistentField}}")

	info := &TradeInfo{
		OfferID:        9999,
		PartnerSteamID: partner,
		OldState:       StateAccepted,
	}

	err := mgr.SendNotification(context.Background(), info)
	require.NoError(t, err)

	assert.Contains(t, chat.sentMessages[partner][0], "internal error occurred while generating a response")
}

func TestManager_SendNotification_UnsupportedState(t *testing.T) {
	chat := newMockChatProvider()
	cfg := newMockConfigProvider()
	logger := log.New(log.DefaultConfig(log.LevelError))

	mgr := NewManager(chat, cfg, logger)

	partner := id.ID(76561198000000009)

	// Pass a completely unsupported dummy state
	info := &TradeInfo{
		OfferID:        9999,
		PartnerSteamID: partner,
		OldState:       TradeState(9999),
	}

	err := mgr.SendNotification(context.Background(), info)
	assert.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "no template found for trade state"))
}

func TestRegisterDefaultTemplate(t *testing.T) {
	key := "decline.custom_test_reason"
	content := "Your offer was declined due to custom test reason."
	RegisterDefaultTemplate(key, content)

	val := GetDefaultTemplate(key)
	assert.Equal(t, content, val)
}
