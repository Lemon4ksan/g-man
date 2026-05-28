// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package session implements a modular behavior for maintaining and keeping a Steam session alive.
package session

import (
	"context"
	"time"

	"github.com/lemon4ksan/g-man/pkg/behavior"
	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/log"
)

// BehaviorName is the unique identifier for the session keep-alive behavior.
const BehaviorName = "session_keepalive"

// KeepAlive returns an option that registers the session keep-alive behavior with the orchestrator.
func KeepAlive(provider Provider, cfg Config) behavior.Option {
	return func(o *behavior.Orchestrator) {
		o.Register(New(provider, o.Logger(), o.Bus(), cfg))
	}
}

// Provider defines the contract needed to verify and refresh a Steam session.
type Provider interface {
	// IsAuthenticated returns whether the session is currently authenticated.
	IsAuthenticated() bool
	// Verify checks if the current web session is still valid.
	Verify(ctx context.Context) (bool, error)
	// Refresh executes an atomic token refresh.
	Refresh(ctx context.Context) error
}

// Config defines the scheduling policy for the session keepalive manager.
type Config struct {
	// Interval specifies how frequently to check session health (defaults to 5 minutes).
	Interval time.Duration
}

// Verifier orchestrates session verification and automatic refreshing.
type Verifier struct {
	provider Provider
	logger   log.Logger
	config   Config
	bus      *bus.Bus
}

// New creates a new session keep-alive manager behavior.
func New(provider Provider, logger log.Logger, bus *bus.Bus, cfg Config) *Verifier {
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Minute
	}

	return &Verifier{
		provider: provider,
		logger:   logger.With(log.Module(BehaviorName)),
		config:   cfg,
		bus:      bus,
	}
}

// Name returns the unique behavior identifier.
func (m *Verifier) Name() string {
	return BehaviorName
}

// Run executes the keep-alive loop, verifying the session at the specified interval
// and performing an automatic refresh if the session has expired.
func (m *Verifier) Run(ctx context.Context) error {
	m.logger.Info("Session Keep-Alive behavior started", log.Duration("interval", m.config.Interval))

	ticker := time.NewTicker(m.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if !m.provider.IsAuthenticated() {
				m.logger.Debug("Session is not authenticated, skipping verification")
				continue
			}

			m.logger.Debug("Verifying session status...")

			isAlive, err := m.provider.Verify(ctx)
			if err != nil {
				m.logger.Warn("Session verification encountered an error", log.Err(err))
			}

			if !isAlive && ctx.Err() == nil {
				m.logger.Info("Session has expired or is invalid. Performing automatic refresh...")

				if err := m.provider.Refresh(ctx); err != nil {
					m.logger.Error("Automatic session refresh failed", log.Err(err))
				} else {
					m.logger.Info("Session successfully refreshed!")
				}
			} else {
				m.logger.Debug("Session is alive and healthy")
			}
		}
	}
}
