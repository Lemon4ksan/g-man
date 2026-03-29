// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package schema

import (
	"strings"

	"github.com/lemon4ksan/g-man/pkg/tf2/sku"
)

// IsPromoItem checks if the item is a promo version.
func (s *Schema) IsPromoItem(it *ItemSchema) bool {
	return strings.HasPrefix(it.Name, "Promo ") && it.CraftClass == ""
}

// NormalizeItem "fixes" an item, bringing its Defindex and quality combinations
// into line with a single trade standard. The method modifies the passed [sku.Item] object using its pointer.
func (s *Schema) NormalizeItem(item *sku.Item) {
	schemaItem := s.GetItemByDef(item.Defindex)
	if schemaItem == nil {
		return
	}

	// Fix for "Upgradeable" weapons (Stock weapons that can be renamed)
	if strings.Contains(schemaItem.Name, strings.ToUpper(schemaItem.ItemClass)) {
		for _, it := range s.Raw.Schema.Items {
			if it.ItemClass == schemaItem.ItemClass && strings.HasPrefix(it.Name, "Upgradeable ") {
				item.Defindex = it.Defindex
				break
			}
		}
	}

	// Standardization of specific items (Keys, Luger)
	switch schemaItem.ItemName {
	case "Mann Co. Supply Crate Key":
		item.Defindex = 5021
	case "Lugermorph":
		item.Defindex = 160
	}

	// Grouping identical items under one Defindex (Strangifiers, Whales)
	switch item.Defindex {
	// Basic Killstreak Kits for various weapons come down to 6527
	case 5726, 5727, 5728, 5729, 5730, 5731, 5732, 5733, 5743, 5744,
		5745, 5746, 5747, 5748, 5749, 5750, 5751, 5793, 5794, 5795,
		5796, 5797, 5798, 5799, 5800, 5801:
		item.Defindex = 6527

	// Mann Co. Stockpile Crate of various versions
	case 5738:
		item.Defindex = 5737

	// Strangifiers for specific weapons are reduced to 6522
	case 5661, 5721, 5722, 5723, 5724, 5725, 5753, 5754, 5755, 5756,
		5757, 5758, 5759, 5783, 5784, 5804:
		item.Defindex = 6522

	// Strangifier Chemistry Sets are reduced to 20,000
	case 20001, 20005, 20008, 20009:
		item.Defindex = 20000
	}
	isPromo := s.IsPromoItem(schemaItem)

	// If this is a promotional item, but the quality is NOT Genuine (e.g., Unique),
	// you need to find the original (non-promotional) defindex of this item.
	if isPromo && item.Quality != QualityGenuine {
		for _, it := range s.Raw.Schema.Items {
			if !s.IsPromoItem(it) && it.ItemName == schemaItem.ItemName {
				item.Defindex = it.Defindex
				break
			}
		}
	} else if !isPromo && item.Quality == QualityGenuine {
		// If this is an original item, but the quality is Genuine,
		// you need to find the promo define.
		for _, it := range s.Raw.Schema.Items {
			if s.IsPromoItem(it) && it.ItemName == schemaItem.ItemName {
				item.Defindex = it.Defindex
				break
			}
		}
	}

	if schemaItem.ItemClass == "supply_crate" {
		if series, ok := s.crateSeriesList[item.Defindex]; ok {
			item.Crateseries = series
		}
	}

	// Fixed bugs with quality combinations (Effects & Qualities)
	if item.Effect != 0 {
		if item.Quality == QualityStrange && item.Paintkit == 0 {
			// Strange Unusual Cosmetic (Valve sometimes gives hats as Strange with the effect)
			// Change to: Quality = Unusual (5), Quality2 = Strange (11)
			item.Quality2 = QualityStrange
			item.Quality = QualityUnusual
		} else if item.Paintkit != 0 {
			// War Paint or Skins (Weapon Skins)
			// If the skin has an Unusual effect and is marked as Strange or Unusual
			if item.Quality2 == QualityStrange || item.Quality == QualityUnusual {
				item.Quality = QualityDecorated // Decorated (15)
			}
		}
	}
}
