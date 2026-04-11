// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inventory

import (
	"context"
	"errors"

	"github.com/lemon4ksan/g-man/pkg/tf2/sku"
)

var (
	ErrItemNotFound = errors.New("tf2inventory: could not find item in inventory")
	ErrSteamAPI     = errors.New("tf2inventory: steam api returned error status")
)

// HistoryStatus represents the result of checking the item's history.
type HistoryStatus struct {
	// Recorded reports whether the service knows about this item.
	Recorded bool
	// IsDuped reports whether the item is considered a duplicate.
	IsDuped bool
}

// DupeChecker defines an interface for any service that can
// check the history of an item (e.g., backpack.tf, reps.tf).
type DupeChecker interface {
	CheckHistory(ctx context.Context, assetID uint64) (HistoryStatus, error)
}

type PlayerItemsResponse struct {
	Result struct {
		Status           int       `json:"status"`
		StatusDetail     string    `json:"statusDetail"`
		NumBackpackSlots int       `json:"num_backpack_slots"`
		Items            []TF2Item `json:"items"`
	} `json:"result"`
}

type TF2Item struct {
	ID              uint64         `json:"id"`
	OriginalID      uint64         `json:"original_id"`
	Defindex        int            `json:"defindex"`
	Level           int            `json:"level"`
	Quality         int            `json:"quality"`
	Inventory       uint32         `json:"inventory"`
	Quantity        int            `json:"quantity"`
	Origin          int            `json:"origin"`
	Style           int            `json:"style,omitempty"`
	FlagCannotTrade bool           `json:"flag_cannot_trade,omitempty"`
	FlagCannotCraft bool           `json:"flag_cannot_craft,omitempty"`
	CustomName      string         `json:"custom_name,omitempty"`
	CustomDesc      string         `json:"custom_desc,omitempty"`
	Attributes      []TF2Attribute `json:"attributes,omitempty"`
}

// ToSKU generates an SKU string for an item using inventory data.
// This allows you to compare items in someone else's inventory with our price list.
func (it *TF2Item) ToSKU() string {
	quality := it.Quality
	defindex := it.Defindex
	isCraftable := !it.FlagCannotCraft

	effect := 0
	wear := 0
	isAustralium := false
	paintkit := 0

	for _, attr := range it.Attributes {
		switch attr.Defindex {
		case 134: // Unusual Effect
			if val, ok := attr.Value.(float64); ok {
				effect = int(val)
			}

		case 725: // Wear
			if val, ok := attr.Value.(float64); ok {
				wear = int(val * 5)
			}

		case 2027: // Australium
			isAustralium = true
		case 834: // Paintkit
			if val, ok := attr.Value.(float64); ok {
				paintkit = int(val)
			}
		}
	}

	str, _ := sku.FromObject(&sku.Item{
		Defindex:   defindex,
		Quality:    quality,
		Craftable:  isCraftable,
		Australium: isAustralium,
		Effect:     effect,
		Wear:       wear,
		Paintkit:   paintkit,
	})

	return str
}

type TF2Attribute struct {
	Defindex   int     `json:"defindex"`
	Value      any     `json:"value"` // int or string
	FloatValue float64 `json:"float_value,omitempty"`
}
