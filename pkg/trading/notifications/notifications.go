// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package notifications compiles template-driven chat notifications responding to trade offer resolution events.
package notifications

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"text/template"

	"github.com/lemon4ksan/miyako/log"
)

// Manager parses, caches, and renders notification message templates.
type Manager struct {
	chat     ChatProvider
	config   ConfigProvider
	logger   log.Logger
	tplMu    sync.RWMutex
	tplCache map[string]*template.Template
}

func NewManager(chat ChatProvider, config ConfigProvider, logger log.Logger) *Manager {
	return &Manager{
		chat:     chat,
		config:   config,
		logger:   logger,
		tplCache: make(map[string]*template.Template),
	}
}

// SendNotification resolves and renders the template for info, delivering it via ChatProvider.
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
		m.logger.Error("Failed to render notification template", log.String("key", key), log.Err(err))

		msg = "An internal error occurred while generating a response."
	}

	return m.chat.SendMessage(ctx, info.PartnerSteamID, msg)
}

func (m *Manager) resolveTemplate(info *TradeInfo) (key, defaultTpl string, err error) {
	switch info.OldState {
	case StateAccepted:
		return "success", GetDefaultTemplate("success"), nil
	case StateInEscrow:
		return "success_escrow", GetDefaultTemplate("success_escrow"), nil
	case StateInvalid:
		return "invalid_trade", GetDefaultTemplate("invalid_trade"), nil
	case StateDeclined:
		declineKey := "decline." + string(info.ReasonType)
		defaultDeclineTpl := GetDefaultTemplate(declineKey)

		if defaultDeclineTpl == "" {
			return "decline.general", GetDefaultTemplate("decline.general"), nil
		}

		return declineKey, defaultDeclineTpl, nil

	case StateCanceled:
		if info.IsCanceledByUser {
			return "cancel.by_user", GetDefaultTemplate("cancel.by_user"), nil
		}

		return "cancel.generic", GetDefaultTemplate("cancel.generic"), nil
	}

	return "", "", fmt.Errorf("no template found for trade state: %d", info.OldState)
}

func (m *Manager) renderTemplate(name, tplStr string, data any) (string, error) {
	m.tplMu.RLock()
	tpl, ok := m.tplCache[name]
	m.tplMu.RUnlock()

	if !ok {
		m.tplMu.Lock()

		var err error

		tpl, ok = m.tplCache[name]
		if !ok {
			tpl, err = template.New(name).Funcs(template.FuncMap{
				"prefix": m.config.GetCommandPrefix,
			}).Parse(tplStr)
			if err == nil {
				m.tplCache[name] = tpl
			}
		}

		m.tplMu.Unlock()

		if err != nil {
			return "", fmt.Errorf("template parse error: %w", err)
		}
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("template execute error: %w", err)
	}

	return buf.String(), nil
}
