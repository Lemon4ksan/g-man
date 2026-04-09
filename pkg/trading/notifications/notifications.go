// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package notifications

import (
	"bytes"
	"context"
	"fmt"
	"text/template"

	"github.com/lemon4ksan/g-man/pkg/log"
)

// Manager is responsible for generating and sending trade-related chat notifications.
type Manager struct {
	chat   ChatProvider
	config ConfigProvider
	logger log.Logger

	// Template cache to avoid re-parsing on every notification.
	templateCache *template.Template
}

// NewManager creates a new notification manager.
func NewManager(chat ChatProvider, config ConfigProvider, logger log.Logger) *Manager {
	return &Manager{
		chat:   chat,
		config: config,
		logger: logger,
		// We can pre-compile common templates or add custom functions here.
		templateCache: template.New("notifications").Funcs(template.FuncMap{
			"prefix": config.GetCommandPrefix, // {{.Prefix}}
		}),
	}
}

// SendNotification determines the correct template for a trade outcome and sends it.
func (m *Manager) SendNotification(ctx context.Context, info *TradeInfo) error {
	key, defaultTpl, err := m.resolveTemplate(info)
	if err != nil {
		return err
	}

	tplStr := m.config.GetTemplate(key)
	if tplStr == "" {
		tplStr = defaultTpl
	}

	msg, err := m.renderTemplate(key, tplStr, info)
	if err != nil {
		m.logger.Error("Failed to render notification template",
			log.String("key", key),
			log.Err(err),
		)
		// Send a generic error message to the user instead of nothing.
		msg = "An internal error occurred while generating a response."
	}

	return m.chat.SendMessage(ctx, info.PartnerSteamID, msg)
}

// resolveTemplate selects the correct template key and default content based on the trade outcome.
func (m *Manager) resolveTemplate(info *TradeInfo) (key, defaultTpl string, err error) {
	switch info.OldState {
	case StateAccepted:
		return "success", defaultTemplates["success"], nil
	case StateInEscrow:
		return "success_escrow", defaultTemplates["success_escrow"], nil
	case StateInvalid:
		return "invalid_trade", defaultTemplates["invalid_trade"], nil
	case StateDeclined:
		declineKey := "decline." + string(info.ReasonType)
		defaultDeclineTpl, ok := defaultTemplates[declineKey]
		if !ok {
			// Fallback for unknown decline reasons
			return "decline.general", defaultTemplates["decline.general"], nil
		}
		return declineKey, defaultDeclineTpl, nil
	case StateCanceled:
		if info.IsCanceledByUser {
			return "cancel.by_user", defaultTemplates["cancel.by_user"], nil
		}
		return "cancel.generic", defaultTemplates["cancel.generic"], nil
	}

	return "", "", fmt.Errorf("no template found for trade state: %d", info.OldState)
}

// renderTemplate executes a Go template with the provided trade data.
func (m *Manager) renderTemplate(name, tplStr string, data any) (string, error) {
	tpl, err := m.templateCache.New(name).Parse(tplStr)
	if err != nil {
		return "", fmt.Errorf("template parse error: %w", err)
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("template execute error: %w", err)
	}

	return buf.String(), nil
}
