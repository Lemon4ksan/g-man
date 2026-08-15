// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inventory

import (
	"context"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
)

// @aoni:service
// @base_url "https://steamcommunity.com/"
// @header "Origin: https://steamcommunity.com"
type InventoryAPI interface {
	// @get "inventory/{steamID}/{appID}/{contextID}"
	// @header "Referer: https://steamcommunity.com/profiles/{steamID}/inventory"
	GetInventoryPage(ctx context.Context, steamID uint64, appID uint32, contextID int64, req GetInventoryPageRequest, mods ...aoni.RequestModifier) (*inventoryResponse, error)

	// @get "profiles/{userID}/inventory"
	GetInventoryHTML(ctx context.Context, userID uint64, mods ...aoni.RequestModifier) ([]byte, error)

	// @get "profiles/{steamID}/inventoryhistory"
	GetInventoryHistoryHTML(ctx context.Context, steamID id.ID, req InventoryHistoryParams, mods ...aoni.RequestModifier) ([]byte, error)
}

// @aoni:dto casing=snake_case
type GetInventoryPageRequest struct {
	Language     string `json:"l" url:"l"`
	Count        int    `json:"count" url:"count"`
	StartAssetID string `json:"start_assetid,omitempty" url:"start_assetid,omitempty"`
}

// @aoni:dto casing=snake_case
type InventoryHistoryParams struct {
	Language   string `json:"l" url:"l"`
	AfterTime  int64  `json:"after_time,omitempty" url:"after_time,omitempty"`
	AfterTrade uint64 `json:"after_trade,omitempty" url:"after_trade,omitempty"`
	Direction  int    `json:"prev" url:"prev"`
}
