// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package web

import (
	"context"

	log "github.com/lemon4ksan/foundation/async/logkit"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"

	"github.com/lemon4ksan/g-man/pkg/steam/community/inventory"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/trading"
)

// PartnerInventoryOptions configures partner inventory projection behavior.
type PartnerInventoryOptions struct {
	FullMode bool
}

type sharedClassMeta struct {
	Name           string
	MarketName     string
	MarketHashName string
	Type           string
	IconURL        string
	Tradable       bool
	Marketable     bool
	Descriptions   []trading.Description
	Tags           []trading.Tag
	Actions        []trading.Action
}

// GetPartnerInventory fetches partner inventory items with standard projections.
func (m *Manager) GetPartnerInventory(ctx context.Context, partnerID id.ID) ([]*trading.Item, error) {
	return m.GetPartnerInventoryOpts(ctx, partnerID, PartnerInventoryOptions{FullMode: false})
}

// GetPartnerInventoryOpts fetches partner inventory with a contiguous item storage slice.
func (m *Manager) GetPartnerInventoryOpts(
	ctx context.Context,
	partnerID id.ID,
	opts PartnerInventoryOptions,
) ([]*trading.Item, error) {
	comm := m.Community()
	if comm == nil {
		return nil, ErrCommunityNotReady
	}

	inv, _, _, err := inventory.GetUserInventoryContents(
		ctx, comm, uint64(partnerID), m.config.AppID, m.config.ContextID, true, m.config.Language,
	)
	if err != nil {
		return nil, err
	}

	if len(inv) == 0 {
		return []*trading.Item{}, nil
	}

	itemStorage := make([]trading.Item, len(inv))
	result := make([]*trading.Item, 0, len(inv))
	classCache := make(map[uint64]*sharedClassMeta, 64)

	validIndex := 0
	for _, it := range inv {
		assetIDVal, ok := bytesconv.ParseUintFast(bytesconv.S2B(it.Asset.AssetID))
		if !ok {
			m.Logger.Warn("Invalid asset ID in partner inventory, skipping item",
				log.String("asset_id", it.Asset.AssetID),
			)

			continue
		}

		assetID := uint64(assetIDVal)
		classIDVal, _ := bytesconv.ParseUintFast(bytesconv.S2B(it.Asset.ClassID))
		classID := uint64(classIDVal)
		instanceIDVal, _ := bytesconv.ParseUintFast(bytesconv.S2B(it.Asset.InstanceID))
		instanceID := uint64(instanceIDVal)
		amountVal, _ := bytesconv.ParseUintFast(bytesconv.S2B(it.Asset.Amount))
		amount := int64(amountVal)

		packedKey := (classID << 32) | (instanceID & 0xFFFFFFFF)

		meta, exists := classCache[packedKey]
		if !exists {
			meta = &sharedClassMeta{
				Name:           it.Description.Name,
				MarketName:     it.Description.Name,
				MarketHashName: it.Description.MarketHashName,
				Type:           it.Description.Type,
				IconURL:        it.Description.IconURL,
				Tradable:       it.Description.Tradable == 1,
			}

			if len(it.Description.Descriptions) > 0 {
				meta.Descriptions = make([]trading.Description, len(it.Description.Descriptions))
				for idx, d := range it.Description.Descriptions {
					meta.Descriptions[idx] = trading.Description{
						Value: d.Value,
						Color: d.Color,
					}
				}
			}

			if len(it.Description.Tags) > 0 {
				meta.Tags = make([]trading.Tag, len(it.Description.Tags))
				for idx, t := range it.Description.Tags {
					meta.Tags[idx] = trading.Tag{
						Category:      t.Category,
						InternalName:  t.InternalName,
						Localized:     t.LocalizedCategoryName,
						LocalizedName: t.LocalizedTagName,
					}
				}
			}

			classCache[packedKey] = meta
		}

		itemPtr := &itemStorage[validIndex]
		validIndex++

		itemPtr.AppID = m.config.AppID
		itemPtr.ContextID = m.config.ContextID
		itemPtr.AssetID = assetID
		itemPtr.ClassID = classID
		itemPtr.InstanceID = instanceID
		itemPtr.Amount = amount
		itemPtr.Name = meta.Name
		itemPtr.MarketHashName = meta.MarketHashName
		itemPtr.Tradable = meta.Tradable
		itemPtr.Descriptions = meta.Descriptions
		itemPtr.Tags = meta.Tags

		if opts.FullMode {
			itemPtr.IconURL = meta.IconURL
			itemPtr.MarketName = meta.MarketName
			itemPtr.Type = meta.Type
		}

		result = append(result, itemPtr)
	}

	_ = m.enricher.EnrichItems(ctx, m.web, m.config.AppID, m.config.Language, result)

	return result, nil
}
