// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tf2schema

import (
	"reflect"
	"testing"

	"github.com/lemon4ksan/g-man/pkg/tf2/sku"
)

func createMockSchema() *Schema {
	items := []*ItemSchema{
		// Checks for "Upgradeable"
		{Defindex: 13, Name: "TF_WEAPON_SCATTERGUN", ItemClass: "tf_weapon_scattergun"},
		{Defindex: 200, Name: "Upgradeable TF_WEAPON_SCATTERGUN", ItemClass: "tf_weapon_scattergun"},

		// Specific items
		{Defindex: 5020, ItemName: "Mann Co. Supply Crate Key"}, // Fake index -> 5021
		{Defindex: 212, ItemName: "Lugermorph"},                 // Fake index -> 160

		// Group items
		{Defindex: 5726, ItemName: "Killstreak Kit"}, // Should be 6527

		// Promo & Genuine
		{Defindex: 851, Name: "AWPer Hand", ItemName: "AWPer Hand", CraftClass: "weapon"},
		{Defindex: 801, Name: "Promo AWPer Hand", ItemName: "AWPer Hand", CraftClass: ""},

		// Checks for crateSeriesList
		{Defindex: 5022, ItemClass: "supply_crate"},

		// Effects check
		{Defindex: 100, ItemName: "Team Captain"},
	}

	raw := &RawSchema{}
	raw.Schema.Items = items

	s := &Schema{
		Raw:             raw,
		itemsByDef:      make(map[int]*ItemSchema),
		crateSeriesList: map[int]int{5022: 42},
	}

	for _, item := range items {
		s.itemsByDef[item.Defindex] = item
	}

	return s
}

func TestSchema_IsPromoItem(t *testing.T) {
	s := &Schema{}

	tests := []struct {
		name     string
		item     *ItemSchema
		expected bool
	}{
		{
			name:     "Valid Promo Item",
			item:     &ItemSchema{Name: "Promo AWPer Hand", CraftClass: ""},
			expected: true,
		},
		{
			name:     "Has Promo prefix but has CraftClass",
			item:     &ItemSchema{Name: "Promo Hat", CraftClass: "hat"},
			expected: false,
		},
		{
			name:     "Empty CraftClass but no Promo prefix",
			item:     &ItemSchema{Name: "AWPer Hand", CraftClass: ""},
			expected: false,
		},
		{
			name:     "Regular item",
			item:     &ItemSchema{Name: "Scattergun", CraftClass: "weapon"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.IsPromoItem(tt.item); got != tt.expected {
				t.Errorf("IsPromoItem() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSchema_NormalizeItem(t *testing.T) {
	s := createMockSchema()

	tests := []struct {
		name     string
		input    sku.Item
		expected sku.Item
	}{
		{
			name:     "Unknown item (should return early)",
			input:    sku.Item{Defindex: 99999},
			expected: sku.Item{Defindex: 99999},
		},
		{
			name:     "Upgradeable weapon fix",
			input:    sku.Item{Defindex: 13},  // TF_WEAPON_SCATTERGUN
			expected: sku.Item{Defindex: 200}, // Upgradeable TF_WEAPON_SCATTERGUN
		},
		{
			name:     "Key standardization",
			input:    sku.Item{Defindex: 5020}, // Some key
			expected: sku.Item{Defindex: 5021}, // Standard key
		},
		{
			name:     "Lugermorph standardization",
			input:    sku.Item{Defindex: 212},
			expected: sku.Item{Defindex: 160},
		},
		{
			name:     "Grouping Killstreak Kits",
			input:    sku.Item{Defindex: 5726},
			expected: sku.Item{Defindex: 6527},
		},
		{
			name:     "Promo to Non-Promo (Quality is NOT Genuine)",
			input:    sku.Item{Defindex: 801, Quality: QualityUnique}, // Promo AWPer Hand, Unique
			expected: sku.Item{Defindex: 851, Quality: QualityUnique}, // Unique AWPer Hand
		},
		{
			name:     "Non-Promo to Promo (Quality IS Genuine)",
			input:    sku.Item{Defindex: 851, Quality: QualityGenuine}, // AWPer Hand, Genuine
			expected: sku.Item{Defindex: 801, Quality: QualityGenuine}, // Promo AWPer Hand
		},
		{
			name:     "Crate series assignment",
			input:    sku.Item{Defindex: 5022},
			expected: sku.Item{Defindex: 5022, Crateseries: 42},
		},
		{
			name: "Strange Unusual Cosmetic",
			input: sku.Item{
				Defindex: 100, // Team Captain
				Effect:   13,  // Burning Flames
				Quality:  QualityStrange,
				Paintkit: 0,
			},
			expected: sku.Item{
				Defindex: 100,
				Effect:   13,
				Quality:  QualityUnusual, // Quality becomes Unusual
				Quality2: QualityStrange, // Quality2 becomes Strange
				Paintkit: 0,
			},
		},
		{
			name: "Unusual Weapon Skin (Decorated)",
			input: sku.Item{
				Defindex: 100,
				Effect:   701, // Some effect
				Quality:  QualityUnusual,
				Paintkit: 100, // Has skin
			},
			expected: sku.Item{
				Defindex: 100,
				Effect:   701,
				Quality:  QualityDecorated, // Skins are always Decorated
				Paintkit: 100,
			},
		},
		{
			name: "Strange Weapon Skin with Effect (Decorated)",
			input: sku.Item{
				Defindex: 100,
				Effect:   701,
				Quality:  QualityStrange, // Initial quality
				Quality2: QualityStrange,
				Paintkit: 100,
			},
			expected: sku.Item{
				Defindex: 100,
				Effect:   701,
				Quality:  QualityDecorated, // Скины всегда Decorated
				Quality2: QualityStrange,
				Paintkit: 100,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := tt.input
			s.NormalizeItem(&actual)

			if !reflect.DeepEqual(actual, tt.expected) {
				t.Errorf("\nGot:\n%+v\nWant:\n%+v", actual, tt.expected)
			}
		})
	}
}
