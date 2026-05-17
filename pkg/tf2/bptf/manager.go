// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bptf

import (
	"context"
	"fmt"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/tf2/schema"
	"github.com/lemon4ksan/g-man/pkg/tf2/sku"
)

// ListingManager manages high-level backpack.tf listings.
// It handles creating, updating, and mass-deleting listings while maintaining internal state.
type ListingManager struct {
	client *Client
	schema *schema.Manager
	logger log.Logger

	mu       sync.RWMutex
	listings map[string]*ListingResponse // id -> listing
}

// NewListingManager creates a new high-level listing manager.
func NewListingManager(client *Client, sm *schema.Manager, logger log.Logger) *ListingManager {
	return &ListingManager{
		client:   client,
		schema:   sm,
		logger:   logger.With(log.Module("bptf_listings")),
		listings: make(map[string]*ListingResponse),
	}
}

// Client returns the underlying backpack.tf client.
func (m *ListingManager) Client() *Client {
	return m.client
}

// Sync fetches all current listings from backpack.tf and updates the internal state.
func (m *ListingManager) Sync(ctx context.Context) error {
	m.logger.Info("syncing listings from backpack.tf")

	var allListings []ListingResponse

	skip := 0
	limit := 500 // Recommended limit for scrolling

	for {
		resp, err := m.client.GetListings(ctx, skip, limit)
		if err != nil {
			return fmt.Errorf("failed to fetch listings at skip %d: %w", skip, err)
		}

		allListings = append(allListings, resp.Results...)

		if len(allListings) >= resp.Cursor.Total || len(resp.Results) == 0 {
			break
		}

		skip += len(resp.Results)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Fully refresh internal state to match backpack.tf
	m.listings = make(map[string]*ListingResponse)
	for i := range allListings {
		m.listings[allListings[i].ID] = &allListings[i]
	}

	m.logger.Info("synced listings", log.Int("count", len(m.listings)))

	return nil
}

// Upsert creates or updates a listing.
func (m *ListingManager) Upsert(ctx context.Context, listing ListingResolvable) (*ListingResponse, error) {
	resp, err := m.client.CreateListing(ctx, listing)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.listings[resp.ID] = resp
	m.mu.Unlock()

	return resp, nil
}

// Delete removes a listing.
func (m *ListingManager) Delete(ctx context.Context, id string) error {
	if err := m.client.DeleteListing(ctx, id); err != nil {
		return err
	}

	m.mu.Lock()
	delete(m.listings, id)
	m.mu.Unlock()

	return nil
}

// DeleteAll removes all listings managed by this manager.
func (m *ListingManager) DeleteAll(ctx context.Context) error {
	m.mu.RLock()

	ids := make([]string, 0, len(m.listings))
	for id := range m.listings {
		ids = append(ids, id)
	}

	m.mu.RUnlock()

	if len(ids) == 0 {
		return nil
	}

	// Batch delete in chunks of 100
	for i := range ids {
		end := min(i+100, len(ids))

		if err := m.client.BatchDeleteListings(ctx, ids[i:end]); err != nil {
			return fmt.Errorf("batch delete failed at %d: %w", i, err)
		}
	}

	m.mu.Lock()
	m.listings = make(map[string]*ListingResponse)
	m.mu.Unlock()

	return nil
}

// FindListingBySKU looks for a listing matching the given SKU and intent.
// It searches for the SKU in the listing details or by matching the item name.
func (m *ListingManager) FindListingBySKU(sku, intent string) *ListingResponse {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, l := range m.listings {
		if l.Intent == intent && m.matchesSKU(l, sku) {
			return l
		}
	}

	return nil
}

// ItemToSKU converts a backpack.tf ItemDocument to a standard TF2 SKU.
func (m *ListingManager) ItemToSKU(doc ItemDocument) string {
	if m.schema == nil {
		return ""
	}

	// Resolve defindex by name since it's not provided in the bptf response
	itemSchema := m.schema.Get().ItemByName(doc.BaseName)
	if itemSchema == nil {
		return ""
	}

	defindex := itemSchema.Defindex

	item := &sku.Item{
		Defindex:  defindex,
		Quality:   doc.Quality.ID,
		Tradable:  doc.Tradable,
		Craftable: doc.Craftable,
	}

	if doc.Particle != nil {
		item.Effect = doc.Particle.ID
	}

	if doc.Paint != nil {
		item.Paint = doc.Paint.ID
	}

	if doc.ElevatedQuality != nil && doc.ElevatedQuality.ID == 11 {
		item.Quality2 = 11
	}

	return sku.FromObject(item)
}

func (m *ListingManager) matchesSKU(l *ListingResponse, sku string) bool {
	// 1. Try to find SKU tag in details: [sku:defindex;quality;...]
	// This is the most reliable way if we managed the listing.
	if l.Details != "" && (l.Details == sku || l.Details == "SKU: "+sku) {
		return true
	}

	// 2. Full conversion using schema
	if m.schema == nil {
		return false
	}

	return m.ItemToSKU(l.Item) == sku
}
