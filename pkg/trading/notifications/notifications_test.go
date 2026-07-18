// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package notifications

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/lemon4ksan/miyako/log"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/trading/reason"
)

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

func (m *mockChatProvider) SetSendError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sendErr = err
}

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

func (m *mockConfigProvider) SetCommandPrefix(prefix string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.commandPrefix = prefix
}

func setupManager(t *testing.T) (*Manager, *mockChatProvider, *mockConfigProvider) {
	t.Helper()

	chat := newMockChatProvider()
	cfg := newMockConfigProvider()
	logger := log.New(log.DefaultConfig(log.LevelError))
	mgr := NewManager(chat, cfg, logger)

	return mgr, chat, cfg
}

func TestSendNotification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		state            TradeState
		reason           reason.TradeReason
		isCanceledByUser bool
		setup            func(cfg *mockConfigProvider, chat *mockChatProvider)
		expectContent    string
		expectErr        string
	}{
		{
			name:          "accepted",
			state:         StateAccepted,
			expectContent: "Success! The trade offer has been accepted",
		},
		{
			name:          "escrow",
			state:         StateInEscrow,
			expectContent: "held in escrow by Steam",
		},
		{
			name:          "invalid",
			state:         StateInvalid,
			expectContent: "This trade is no longer valid",
		},
		{
			name:             "canceled_by_user",
			state:            StateCanceled,
			isCanceledByUser: true,
			expectContent:    "canceled by the user",
		},
		{
			name:             "canceled_generic",
			state:            StateCanceled,
			isCanceledByUser: false,
			expectContent:    "due to Steam issues",
		},
		{
			name:          "decline_manual",
			state:         StateDeclined,
			reason:        reason.DeclineManual,
			expectContent: "declined by the owner",
		},
		{
			name:          "decline_escrow",
			state:         StateDeclined,
			reason:        reason.DeclineEscrow,
			expectContent: "result in a trade hold",
		},
		{
			name:          "decline_begging",
			state:         StateDeclined,
			reason:        reason.DeclineBegging,
			expectContent: "asking for items for free",
		},
		{
			name:          "decline_fallback",
			state:         StateDeclined,
			reason:        reason.TradeReason("UNKNOWN_REASON"),
			expectContent: "Your trade offer has been declined",
		},
		{
			name:  "config_overrides",
			state: StateAccepted,
			setup: func(cfg *mockConfigProvider, chat *mockChatProvider) {
				cfg.SetTemplate(
					"success",
					"Trade #{{.OfferID}} accepted successfully! Partner: {{.PartnerSteamID}}. Prefix is: {{prefix}}",
				)
				cfg.SetCommandPrefix("/")
			},
			expectContent: "Trade #1001 accepted successfully! Partner: 76561198000000001. Prefix is: /",
		},
		{
			name:  "template_parse_error",
			state: StateAccepted,
			setup: func(cfg *mockConfigProvider, chat *mockChatProvider) {
				cfg.SetTemplate("success", "Trade #{{.OfferID")
			},
			expectContent: "internal error occurred while generating a response",
		},
		{
			name:  "template_execution_error",
			state: StateAccepted,
			setup: func(cfg *mockConfigProvider, chat *mockChatProvider) {
				cfg.SetTemplate("success", "Trade #{{.NonExistentField}}")
			},
			expectContent: "internal error occurred while generating a response",
		},
		{
			name:      "unsupported_state",
			state:     TradeState(9999),
			expectErr: "no template found for trade state",
		},
		{
			name:  "chat_send_error",
			state: StateAccepted,
			setup: func(cfg *mockConfigProvider, chat *mockChatProvider) {
				chat.SetSendError(errors.New("network error"))
			},
			expectErr: "network error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mgr, chat, cfg := setupManager(t)
			partner := id.ID(76561198000000001)

			if tt.setup != nil {
				tt.setup(cfg, chat)
			}

			info := &TradeInfo{
				OfferID:          1001,
				PartnerSteamID:   partner,
				OldState:         tt.state,
				ReasonType:       tt.reason,
				IsCanceledByUser: tt.isCanceledByUser,
			}

			err := mgr.SendNotification(t.Context(), info)

			if tt.expectErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.expectErr)
				}

				if !strings.Contains(err.Error(), tt.expectErr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.expectErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			chat.mu.Lock()
			messages := chat.sentMessages[partner]
			chat.mu.Unlock()

			if len(messages) == 0 {
				t.Fatalf("expected at least one sent message to %v, got none", partner)
			}

			gotMsg := messages[0]
			if !strings.Contains(gotMsg, tt.expectContent) {
				t.Errorf("expected message %q to contain %q", gotMsg, tt.expectContent)
			}
		})
	}
}

func TestRegisterDefaultTemplate(t *testing.T) {
	key := "decline.custom_test_reason"
	content := "Your offer was declined due to custom test reason."

	orig := GetDefaultTemplate(key)
	t.Cleanup(func() {
		RegisterDefaultTemplate(key, orig)
	})

	RegisterDefaultTemplate(key, content)

	val := GetDefaultTemplate(key)
	if val != content {
		t.Errorf("expected %q, got %q", content, val)
	}
}
