// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inventory

import (
	"errors"
)

var (
	ErrItemNotFound = errors.New("tf2inventory: could not find item in inventory")
	ErrSteamAPI     = errors.New("tf2inventory: steam api returned error status")
)

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

type TF2Attribute struct {
	Defindex   int         `json:"defindex"`
	Value      interface{} `json:"value"` // int or string
	FloatValue float64     `json:"float_value,omitempty"`
}
