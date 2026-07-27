// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package guard provides an event-driven behavior for automated handling of Steam Guard mobile confirmations.
package guard

import (
	"context"

	"github.com/lemon4ksan/miyako/bus"
	"github.com/lemon4ksan/miyako/generic"
	"github.com/lemon4ksan/miyako/log"

	"github.com/lemon4ksan/g-man/pkg/behavior"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/guard"
)

var (
	WithModule = guard.WithModule
	From       = guard.From
)

type ConfirmationType = guard.ConfirmationType

const (
	ConfTypeGeneric       = guard.ConfTypeGeneric
	ConfTypeTrade         = guard.ConfTypeTrade
	ConfTypeMarket        = guard.ConfTypeMarket
	ConfTypeLogin         = guard.ConfTypeLogin
	ConfTypeAccountChange = guard.ConfTypeAccountChange
)

// DefaultConfig builds guard Config using secrets and device identifiers.
func DefaultConfig(sharedSecret, identitySecret, deviceID string) guard.Config {
	guardCfg := guard.DefaultConfig()
	guardCfg.SharedSecret = sharedSecret
	guardCfg.IdentitySecret = identitySecret
	guardCfg.DeviceID = deviceID

	return guardCfg
}

// BehaviorName is the unique name of the guard behavior.
const BehaviorName = "guard_manager"

// AutoAccept registers a guard manager behavior with the client orchestrator.
func AutoAccept(client *steam.Client, cfg Config) {
	behavior.From(client).Register(New(guard.From(client), client.Logger(), client.Bus(), cfg))
}

// Provider defines methods required to query and accept pending mobile confirmations.
type Provider interface {
	FetchConfirmations(ctx context.Context) ([]*guard.Confirmation, error)
	AcceptMultiple(ctx context.Context, confs []*guard.Confirmation) error
}

// Config configures confirmation categories to automatically approve.
type Config struct {
	AutoAcceptTypes generic.Set[guard.ConfirmationType]
	PollOnStart     bool
}

// Manager listens for Steam Guard event notifications and executes automated confirmation approvals.
type Manager struct {
	guardian Provider
	logger   log.Logger
	config   Config
	bus      *bus.Bus
}

// New constructs a guard Manager instance.
func New(guardian Provider, logger log.Logger, bus *bus.Bus, cfg Config) *Manager {
	return &Manager{
		guardian: guardian,
		logger:   logger.With(log.Module(BehaviorName)),
		config:   cfg,
		bus:      bus,
	}
}

// Name returns behavior name "guard_manager".
func (m *Manager) Name() string {
	return BehaviorName
}

// Run starts event subscriptions and listens for confirmation triggers.
func (m *Manager) Run(ctx context.Context) error {
	m.logger.Info("Guard Manager behavior started", log.Any("auto_accept", m.config.AutoAcceptTypes))

	if m.config.PollOnStart {
		m.logger.Debug("Performing initial confirmation fetch...")
		m.resolveConfirmations(ctx)
	}

	sub := m.bus.Subscribe(
		&auth.SteamGuardRequiredEvent{},
		&guard.ConfirmationRequiredEvent{},
	)
	defer sub.Unsubscribe()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-sub.C():
			if !ok {
				return nil
			}

			trigger := false
			switch e := ev.(type) {
			case *auth.SteamGuardRequiredEvent:
				if e.IsAppConfirm {
					m.logger.Debug("Received login confirmation request signal")

					trigger = true
				}

			case *guard.ConfirmationRequiredEvent:
				if e.IsAppConfirm {
					m.logger.Debug("Received trade confirmation request signal", log.String("offer_id", e.TradeOfferID))

					trigger = true
				}
			}

			if trigger {
				go m.resolveConfirmations(ctx)
			}
		}
	}
}

func (m *Manager) resolveConfirmations(ctx context.Context) {
	confs, err := m.guardian.FetchConfirmations(ctx)
	if err != nil {
		m.logger.Error("Failed to fetch confirmations", log.Err(err))
		return
	}

	if len(confs) == 0 {
		return
	}

	var toAccept []*guard.Confirmation
	for _, conf := range confs {
		if m.config.AutoAcceptTypes.Has(conf.Type) {
			toAccept = append(toAccept, conf)
		} else {
			m.logger.Info("Confirmation requires manual review",
				log.String("type", conf.Type.String()),
				log.String("title", conf.Title),
			)
		}
	}

	if len(toAccept) == 0 {
		return
	}

	m.logger.Info("Automatically accepting confirmations", log.Int("count", len(toAccept)))

	if err := m.guardian.AcceptMultiple(ctx, toAccept); err != nil {
		m.logger.Error("Failed to accept confirmations", log.Err(err))
	}
}
