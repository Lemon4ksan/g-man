// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package market provides a unified interface and models for automated
// listing synchronization across various Steam marketplace platforms.
package market

import (
	"context"
)

// Price represents a unified value model supporting keys, metal, and real cash.
type Price struct {
	Keys  int     `json:"keys"`
	Metal float64 `json:"metal"`
	Cash  int64   `json:"cash"` // Value represented in base monetary units (cents/kopecks)
}

// Listing represents an active, remote marketplace listing.
type Listing struct {
	ID      string `json:"id"`       // Unique remote listing identifier
	AssetID uint64 `json:"asset_id"` // Steam asset ID of the physical item
	SKU     string `json:"sku"`      // Unified SKU
	Price   Price  `json:"price"`    // Active listed price
}

// ListingRequest represents a payload to create or modify a remote listing.
type ListingRequest struct {
	AssetID uint64 `json:"asset_id"`
	SKU     string `json:"sku"`
	Price   Price  `json:"price"`
}

// Adapter defines the contract that any marketplace integration must satisfy.
// All specific market clients (Steam-Trader, Backpack.tf, Crit.tf) will implement this.
type Adapter interface {
	// Name returns the unique string identifier of the marketplace (e.g., "steam_trader", "bptf").
	Name() string
	// GameID returns the Steam AppID this adapter works with (e.g., 730, 440).
	GameID() int
	// GetListings fetches all active listings on the remote platform.
	GetListings(ctx context.Context) ([]Listing, error)
	// CreateListings registers new item listings on the remote platform.
	CreateListings(ctx context.Context, items []ListingRequest) error
	// UpdateListings modifies pricing or details of active remote listings.
	UpdateListings(ctx context.Context, items []ListingRequest) error
	// DeleteListings withdraws active listings from the remote platform.
	DeleteListings(ctx context.Context, ids []string) error
}
