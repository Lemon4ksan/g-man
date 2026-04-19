// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package inventory allows retrieving user inventory using community requester.
package inventory

import (
	"context"
	"fmt"

	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
)

// GetUserInventoryContents recursively parses user inventory using 
// community requester and returns the list of items and currencies
// with their total count.
func GetUserInventoryContents(
	ctx context.Context,
	c community.Requester,
	userID uint64,
	appID int,
	contextID int,
	tradableOnly bool,
	language string,
) ([]CEconItem, []CEconItem, int, error) {
	if language == "" {
		language = "english"
	}

	var (
		inventory    []CEconItem
		currency     []CEconItem
		startAssetID string
		totalCount   int
	)

	pos := 1

	for {
		path := fmt.Sprintf("inventory/%d/%d/%d", userID, appID, contextID)

		req := struct {
			Language     string `url:"l"`
			Count        int    `url:"count"`
			StartAssetID string `url:"start_assetid,omitempty"`
		}{Language: language, Count: 1000, StartAssetID: startAssetID}

		resp, err := community.Get[inventoryResponse](ctx, c, path, req, api.WithHeader(
			"Referer", fmt.Sprintf("https://steamcommunity.com/profiles/%d/inventory", userID)),
		)
		if err != nil {
			return nil, nil, 0, err
		}

		if !resp.Success {
			return nil, nil, 0, fmt.Errorf("steam error: %s", resp.Error)
		}

		if resp.TotalCount == 0 || (len(resp.Assets) == 0) {
			return inventory, currency, resp.TotalCount, nil
		}

		descMap := make(map[string]*InventoryDescription)
		for _, d := range resp.Descriptions {
			key := fmt.Sprintf("%s_%s", d.ClassID, d.InstanceID)
			descMap[key] = &d
		}

		for _, asset := range resp.Assets {
			key := fmt.Sprintf("%s_%s", asset.ClassID, asset.InstanceID)
			description := descMap[key]

			if tradableOnly && (description == nil || description.Tradable == 0) {
				continue
			}

			asset.Pos = pos
			pos++

			item := CEconItem{
				Asset:       asset,
				Description: description,
			}

			if asset.CurrencyID != "" {
				currency = append(currency, item)
			} else {
				inventory = append(inventory, item)
			}
		}

		totalCount = resp.TotalCount

		if !resp.MoreItems {
			break
		}

		startAssetID = resp.LastAssetID
	}

	return inventory, currency, totalCount, nil
}
