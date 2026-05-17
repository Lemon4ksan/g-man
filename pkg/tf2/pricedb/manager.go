// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pricedb

import (
	"context"
	"sync"
	"time"

	"github.com/lemon4ksan/g-man/pkg/behavior"
	"github.com/lemon4ksan/g-man/pkg/log"
)

// BehaviorName is the unique name of the behavior.
const BehaviorName = "pricedb_sync"

// WithPriceManager returns an option that registers the pricedb manager behavior with the orchestrator.
func WithPriceManager(client *Client) behavior.Option {
	return func(o *behavior.Orchestrator) {
		o.Register(NewManager(client, o.Logger()))
	}
}

// Manager handles caching and background updates for PriceDB prices.
// It acts as the primary price authority for the bot.
// It implements behavior.Behavior interface.
type Manager struct {
	client *Client
	logger log.Logger

	mu    sync.RWMutex
	cache map[string]*Price

	watchedSKUs  map[string]struct{}
	syncInterval time.Duration

	socket *SocketManager
}

// NewManager creates a new price manager for PriceDB.
// It implements behavior.Behavior interface.
func NewManager(client *Client, logger log.Logger) *Manager {
	m := &Manager{
		client:       client,
		logger:       logger.With(log.Module(BehaviorName)),
		cache:        make(map[string]*Price),
		watchedSKUs:  make(map[string]struct{}),
		syncInterval: 30 * time.Minute,
	}

	m.socket = NewSocketManager("", m.logger)
	m.socket.OnPrice(func(p *Price) {
		m.logger.Debug("Received real-time price update", log.String("sku", p.SKU))
		m.mu.Lock()
		defer m.mu.Unlock()

		if p.Validate() {
			m.cache[p.SKU] = p
		}
	})

	return m
}

// Name returns the unique name of the behavior.
func (m *Manager) Name() string {
	return BehaviorName
}

// Run starts the background price synchronization loop.
func (m *Manager) Run(ctx context.Context) error {
	m.logger.Info("PriceDB Sync behavior started", log.Duration("interval", m.syncInterval))

	ticker := time.NewTicker(m.syncInterval)
	defer ticker.Stop()

	// Initial update
	if err := m.Update(ctx); err != nil {
		m.logger.Error("Initial PriceDB update failed", log.Err(err))
	}

	// Start Socket.IO real-time listener
	go func() {
		if err := m.socket.Run(ctx); err != nil {
			m.logger.Error("PriceDB socket listener stopped", log.Err(err))
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := m.Update(ctx); err != nil {
				m.logger.Error("PriceDB background sync failed", log.Err(err))
			}
		}
	}
}

// GetPrice returns a cached price for the given SKU.
func (m *Manager) GetPrice(sku string) (*Price, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	p, ok := m.cache[sku]

	return p, ok
}

// Watch adds a SKU to the background update list.
func (m *Manager) Watch(sku string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.watchedSKUs[sku] = struct{}{}
}

// Unwatch removes a SKU from the background update list.
func (m *Manager) Unwatch(sku string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.watchedSKUs, sku)
}

// GetWatchedSKUs returns a slice of all currently watched SKUs.
func (m *Manager) GetWatchedSKUs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	skus := make([]string, 0, len(m.watchedSKUs))
	for sku := range m.watchedSKUs {
		skus = append(skus, sku)
	}

	return skus
}

// Update fetches the latest prices for all watched SKUs.
func (m *Manager) Update(ctx context.Context) error {
	m.mu.RLock()

	skus := make([]string, 0, len(m.watchedSKUs))
	for sku := range m.watchedSKUs {
		skus = append(skus, sku)
	}

	m.mu.RUnlock()

	if len(skus) == 0 {
		return nil
	}

	m.logger.Debug("Syncing prices from PriceDB...", log.Int("count", len(skus)))

	// PriceDB bulk API usually handles many SKUs at once.
	prices, err := m.client.GetItemsBulk(ctx, skus)
	if err != nil {
		return err
	}

	m.mu.Lock()
	for _, p := range prices {
		if p.Validate() {
			m.cache[p.SKU] = p
		}
	}

	m.mu.Unlock()

	return nil
}

// Fetch fetches the latest prices for the given SKUs and updates the cache.
func (m *Manager) Fetch(ctx context.Context, skus []string) (map[string]*Price, error) {
	if len(skus) == 0 {
		return make(map[string]*Price), nil
	}

	prices, err := m.client.GetItemsBulk(ctx, skus)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*Price)

	m.mu.Lock()
	for _, p := range prices {
		if p.Validate() {
			m.cache[p.SKU] = p
			result[p.SKU] = p
		}
	}

	m.mu.Unlock()

	return result, nil
}

// SeedFromBackpack populates the watched SKUs from the given inventory.
// This is useful for initial seeding at startup.
func (m *Manager) SeedFromBackpack(ctx context.Context, items []string) error {
	if len(items) == 0 {
		return nil
	}

	m.mu.Lock()
	for _, sku := range items {
		m.watchedSKUs[sku] = struct{}{}
	}

	m.mu.Unlock()

	m.logger.Info("Seeding prices from backpack...", log.Int("skus", len(items)))

	return m.Update(ctx)
}
