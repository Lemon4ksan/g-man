// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package guard handles the decision-making policy for Steam Guard confirmations.
// It listens for events from various modules and uses the guard module to resolve them.
package guard

import (
	"context"
	"slices"

	"github.com/lemon4ksan/g-man/pkg/behavior"
	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/guard"
)

// BehaviorName is the unique name of the guard behavior.
const BehaviorName = "guard_manager"

// AutoAccept returns an option that registers the guard manager behavior with the orchestrator.
func AutoAccept(provider Provider, cfg Config) behavior.Option {
	return func(o *behavior.Orchestrator) {
		o.Register(New(provider, o.Logger(), o.Bus(), cfg))
	}
}

// Provider defines the interface for fetching and accepting Steam Guard confirmations.
type Provider interface {
	// FetchConfirmations retrieves a list of pending confirmations from the Steam Guard module.
	FetchConfirmations(ctx context.Context) ([]*guard.Confirmation, error)
	// AcceptMultiple accepts multiple confirmations in a single batch.
	AcceptMultiple(ctx context.Context, confs []*guard.Confirmation) error
}

// Config defines which confirmations should be automatically accepted.
type Config struct {
	// AutoAcceptTypes specifies which confirmation types to auto-accept (e.g. Trade, Login).
	AutoAcceptTypes []guard.ConfirmationType
	// PollOnStart enables a one-time fetch when the behavior starts to catch missed confirmations.
	PollOnStart bool
}

// Manager handles the decision-making policy for Steam Guard confirmations.
// It listens for events from various modules and uses the guard module to resolve them.
type Manager struct {
	guardian Provider
	logger   log.Logger
	config   Config
	bus      *bus.Bus
}

// New creates a new guard manager behavior.
func New(guardian Provider, logger log.Logger, bus *bus.Bus, cfg Config) *Manager {
	return &Manager{
		guardian: guardian,
		logger:   logger.With(log.Module(BehaviorName)),
		config:   cfg,
		bus:      bus,
	}
}

// Name returns the unique name of the behavior.
func (m *Manager) Name() string {
	return BehaviorName
}

// Run starts the guard manager, listening for confirmation-related events.
func (m *Manager) Run(ctx context.Context) error {
	m.logger.Info("Guard Manager behavior started", log.Any("auto_accept", m.config.AutoAcceptTypes))

	if m.config.PollOnStart {
		m.logger.Debug("Performing initial confirmation fetch...")
		m.resolveConfirmations(ctx)
	}

	// Subscribe to all events that might indicate a new mobile confirmation is available
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
				// Proactively resolve confirmations
				m.resolveConfirmations(ctx)
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
		if slices.Contains(m.config.AutoAcceptTypes, conf.Type) {
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
