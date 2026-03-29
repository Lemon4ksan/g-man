// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package schema

import (
	"strings"

	"github.com/lemon4ksan/g-man/pkg/modules/econ"
	"github.com/lemon4ksan/g-man/pkg/tf2/sku"
)

// GetSKUFromEconItem converts a generic Steam WebAPI item into a strict TF2 SKU string.
func (s *Schema) GetSKUFromEconItem(item *econ.Item) string {
	nameToParse := item.MarketHashName
	if nameToParse == "" {
		nameToParse = item.MarketName
	}

	skuItem := s.GetItemObjectFromName(nameToParse)
	if skuItem == nil {
		return "unknown"
	}

	skuItem.Tradable = item.Tradable

	for _, desc := range item.Descriptions {
		if desc.Value == "( Not Usable in Crafting )" {
			skuItem.Craftable = false
			break
		}
	}

	if skuItem.Quality == QualityUnusual && skuItem.Effect == 0 {
		for _, desc := range item.Descriptions {
			if after, ok := strings.CutPrefix(desc.Value, "★ Unusual Effect: "); ok {
				if id := s.GetEffectIdByName(after); id != 0 {
					skuItem.Effect = id
					break
				}
			}
		}
	}

	for _, desc := range item.Descriptions {
		val := strings.TrimSpace(desc.Value)
		if strings.HasPrefix(val, "Halloween: ") {
			if spellID, ok := halloweenSpells[val]; ok {
				skuItem.Spells = append(skuItem.Spells, spellID)
			}
		}
	}

	s.NormalizeItem(skuItem)

	str, err := sku.FromObject(skuItem)
	if err != nil {
		return "invalid"
	}

	return str
}
