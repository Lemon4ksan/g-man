// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package session implements automated keep-alive polling and verification loops for Steam web sessions.
package session

import (
	"context"
	"time"

	"github.com/lemon4ksan/miyako/bus"
	"github.com/lemon4ksan/miyako/generic"
	"github.com/lemon4ksan/miyako/log"

	"github.com/lemon4ksan/g-man/pkg/behavior"
	"github.com/lemon4ksan/g-man/pkg/steam"
)

// BehaviorName is the identifier for the keep-alive behavior.
const BehaviorName = "session_keepalive"

// KeepAlive registers the session keep-alive behavior with the client behavior runner.
func KeepAlive(client *steam.Client, cfg Config) {
	behavior.From(client).Register(New(client.Session(), client.Logger(), client.Bus(), cfg))
}

// Provider defines methods required to verify authentication status and trigger session renewals.
type Provider interface {
	IsAuthenticated() bool
	Verify(ctx context.Context) (bool, error)
	Refresh(ctx context.Context) error
}

// Config configures keep-alive health check intervals.
type Config struct {
	Interval time.Duration
}

// Verifier periodically checks session cookies and triggers automated token updates when invalid.
type Verifier struct {
	provider Provider
	logger   log.Logger
	config   Config
	bus      *bus.Bus
}

// New constructs a session keep-alive Verifier instance.
func New(provider Provider, logger log.Logger, bus *bus.Bus, cfg Config) *Verifier {
	cfg.Interval = generic.Coalesce(cfg.Interval, 5*time.Minute)

	return &Verifier{
		provider: provider,
		logger:   logger.With(log.Module(BehaviorName)),
		config:   cfg,
		bus:      bus,
	}
}

// Name returns behavior identifier "session_keepalive".
func (m *Verifier) Name() string {
	return BehaviorName
}

// Run starts periodic session health checks.
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
