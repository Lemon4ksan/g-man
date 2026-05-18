// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package schema

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/g-man/pkg/tf2/sku"
	"github.com/lemon4ksan/g-man/pkg/trading"
)

// minimalRawSchema creates a minimal raw schema for testing.
func minimalRawSchema() *Raw {
	items := []*Item{
		{
			Defindex:      5021,
			Name:          "Scattergun",
			ItemName:      "Scattergun",
			ItemClass:     "weapon",
			ItemQuality:   QualityUnique,
			ProperName:    false,
			CraftClass:    "weapon",
			UsedByClasses: []string{"Scout"},
		},
		{
			Defindex:     378,
			Name:         "Team Captain",
			ItemName:     "Team Captain",
			ItemClass:    "tf_wearable",
			ItemQuality:  QualityUnique,
			Capabilities: &Capabilities{Paintable: true},
		},
		{Defindex: 15013, Name: "Pistol", ItemName: "Pistol", ItemClass: "weapon", ItemQuality: QualityDecorated},
		{
			Defindex:    16189,
			Name:        "Paintkit 102",
			ItemName:    "Woodsy Widowmaker War Paint",
			ItemClass:   "tool",
			ItemQuality: QualityDecorated,
		},
		{
			Defindex:    5022,
			Name:        "Crate 1",
			ItemName:    "Mann Co. Supply Crate",
			ItemClass:   "supply_crate",
			ItemQuality: QualityUnique,
		},
		{Defindex: 160, Name: "Lugermorph", ItemName: "Lugermorph", ItemClass: "weapon", ItemQuality: QualityVintage},
		{
			Defindex:    294,
			Name:        "Promo Lugermorph",
			ItemName:    "Promo Lugermorph",
			ItemClass:   "weapon",
			ItemQuality: QualityUnique,
		},
		{
			Defindex:      38,
			Name:          "Scout Weapon",
			ItemName:      "Scout Weapon",
			ItemClass:     "weapon",
			ItemQuality:   QualityUnique,
			CraftClass:    "weapon",
			UsedByClasses: []string{"Scout"},
		},
		{
			Defindex:     100,
			Name:         "Cosmetic",
			ItemName:     "Cosmetic",
			ItemClass:    "tf_wearable",
			ItemQuality:  QualityUnique,
			Capabilities: &Capabilities{Paintable: true},
		},
		{
			Defindex:    5739,
			Name:        "Seriesless Crate",
			ItemName:    "Seriesless Crate",
			ItemClass:   "supply_crate",
			ItemQuality: QualityUnique,
		},
		{
			Defindex:    6526,
			Name:        "Professional Killstreak Kit",
			ItemName:    "Professional Killstreak Kit",
			ItemClass:   "tool",
			ItemQuality: QualityUnique,
		},
		{Defindex: 6522, Name: "Strangifier", ItemName: "Strangifier", ItemClass: "tool", ItemQuality: QualityUnique},
		{
			Defindex:    20006,
			Name:        "Collector's Chemistry Set",
			ItemName:    "Collector's Chemistry Set",
			ItemClass:   "tool",
			ItemQuality: QualityUnique,
		},
		{
			Defindex:    20000,
			Name:        "Strangifier Chemistry Set",
			ItemName:    "Strangifier Chemistry Set",
			ItemClass:   "tool",
			ItemQuality: QualityUnique,
		},
		{
			Defindex:    20003,
			Name:        "Professional Killstreak Kit Fabricator",
			ItemName:    "Professional Killstreak Kit Fabricator",
			ItemClass:   "tool",
			ItemQuality: QualityUnique,
		},
		{
			Defindex:    20002,
			Name:        "Specialized Killstreak Kit Fabricator",
			ItemName:    "Specialized Killstreak Kit Fabricator",
			ItemClass:   "tool",
			ItemQuality: QualityUnique,
		},
		{Defindex: 9258, Name: "Unusualifier", ItemName: "Unusualifier", ItemClass: "tool", ItemQuality: QualityUnique},
	}

	qualities := map[string]int{
		"Normal": 0, "Genuine": 1, "Vintage": 3, "Unusual": 5, "Unique": 6, "Community": 7,
		"Valve": 8, "Self-Made": 9, "Customized": 10, "Strange": 11, "Completed": 12,
		"Haunted": 13, "Collector's": 14, "Decorated": 15,
	}

	qualityNames := map[string]string{
		"Normal":      "Normal",
		"Genuine":     "Genuine",
		"Vintage":     "Vintage",
		"Unusual":     "Unusual",
		"Unique":      "Unique",
		"Community":   "Community",
		"Valve":       "Valve",
		"Self-Made":   "Self-Made",
		"Customized":  "Customized",
		"Strange":     "Strange",
		"Completed":   "Completed",
		"Haunted":     "Haunted",
		"Collector's": "Collector's",
		"Decorated":   "Decorated",
	}

	particles := []*ParticleEffect{
		{ID: 13, Name: "Burning Flames"},
		{ID: 33, Name: "Orbiting Fire"},
		{ID: 103, Name: "Ether Trail"},
		{ID: 141, Name: "Fragmenting Reality"},
	}

	paintKits := map[string]string{
		"102":   "Woodsy Widowmaker",
		"15013": "Pistol Skin",
	}

	itemsGame := map[string]any{
		"items": map[string]any{
			"5022": map[string]any{
				"static_attrs": map[string]any{
					"set supply crate series": map[string]any{
						"value": float64(1),
					},
				},
			},
		},
	}

	killEater := []*KillEaterScoreType{
		{Type: 0, TypeName: "Kills"},
		{Type: 1, TypeName: "Kill Assists"},
		{Type: 97, TypeName: "Something Excluded"},
	}

	return &Raw{
		Schema: struct {
			Items                                []*Item               `json:"items"`
			Attributes                           []*AttributeSchema    `json:"attributes"`
			Qualities                            map[string]int        `json:"qualities"`
			QualityNames                         map[string]string     `json:"qualityNames"`
			OriginNames                          []*OriginName         `json:"originNames"`
			ItemSets                             []*ItemSet            `json:"item_sets"`
			AttributeControlledAttachedParticles []*ParticleEffect     `json:"attribute_controlled_attached_particles"`
			ItemLevels                           []*ItemLevel          `json:"item_levels"`
			KillEaterScoreTypes                  []*KillEaterScoreType `json:"kill_eater_score_types"`
			StringLookups                        []*StringLookup       `json:"string_lookups"`
			PaintKits                            map[string]string     `json:"paintkits"`
		}{
			Items:                                items,
			Qualities:                            qualities,
			QualityNames:                         qualityNames,
			AttributeControlledAttachedParticles: particles,
			PaintKits:                            paintKits,
			KillEaterScoreTypes:                  killEater,
		},
		ItemsGame: itemsGame,
	}
}

func TestNewSchema(t *testing.T) {
	raw := minimalRawSchema()

	s := New(raw)
	if s == nil {
		t.Fatal("NewSchema returned nil")
	}

	// Verify indices are built
	if len(s.itemsByDef) != len(raw.Schema.Items) {
		t.Errorf("expected %d itemsByDef, got %d", len(raw.Schema.Items), len(s.itemsByDef))
	}

	if len(s.itemsByName) != len(raw.Schema.Items) {
		t.Errorf("expected %d itemsByName, got %d", len(raw.Schema.Items), len(s.itemsByName))
	}

	if len(s.attrsByDef) != len(raw.Schema.Attributes) {
		t.Errorf("expected %d attrsByDef, got %d", len(raw.Schema.Attributes), len(s.attrsByDef))
	}

	if len(s.qualByID) != len(raw.Schema.Qualities) {
		t.Errorf("expected %d qualByID, got %d", len(raw.Schema.Qualities), len(s.qualByID))
	}

	if len(s.qualByName) != len(raw.Schema.Qualities) {
		t.Errorf("expected %d qualByName, got %d", len(raw.Schema.Qualities), len(s.qualByName))
	}

	expectedEff := 0

	for _, p := range raw.Schema.AttributeControlledAttachedParticles {
		if p.Name != "" {
			expectedEff++
		}
	}

	if len(s.effByID) < expectedEff {
		t.Errorf("expected at least %d effByID, got %d", expectedEff, len(s.effByID))
	}
}

func TestGetItemByDef(t *testing.T) {
	s := New(minimalRawSchema())

	item := s.ItemByDef(5022)
	if item == nil {
		t.Fatal("item 5022 not found")
	}

	if item.Defindex != 5022 {
		t.Errorf("expected defindex 5022, got %d", item.Defindex)
	}
}

func TestGetItemByName(t *testing.T) {
	s := New(minimalRawSchema())

	item := s.ItemByName("Mann Co. Supply Crate")
	if item == nil {
		t.Fatal("item not found")
	}

	if item.Defindex != 5022 {
		t.Errorf("expected defindex 5022, got %d", item.Defindex)
	}

	// case insensitivity
	item = s.ItemByName("mann co. supply crate")
	if item == nil {
		t.Error("case insensitive lookup failed")
	}
}

func TestGetQualityByIdAndName(t *testing.T) {
	s := New(minimalRawSchema())

	tests := []struct {
		id   int
		name string
	}{
		{6, "Unique"},
		{11, "Strange"},
		{5, "Unusual"},
		{1, "Genuine"},
	}

	for _, tt := range tests {
		if name := s.QualityByID(tt.id); name != tt.name {
			t.Errorf("GetQualityById(%d): expected %s, got %s", tt.id, tt.name, name)
		}

		if id := s.QualityIDByName(tt.name); id != tt.id {
			t.Errorf("GetQualityIdByName(%s): expected %d, got %d", tt.name, tt.id, id)
		}
	}

	if name := s.QualityByID(99); name != "" {
		t.Errorf("expected empty for unknown id, got %s", name)
	}

	if id := s.QualityIDByName("nonexistent"); id != 0 {
		t.Errorf("expected 0, got %d", id)
	}
}

func TestGetEffectByIdAndName(t *testing.T) {
	s := New(minimalRawSchema())

	tests := []struct {
		id   int
		name string
	}{
		{33, "Orbiting Fire"},
		{103, "Ether Trail"},
		{141, "Fragmenting Reality"},
	}

	for _, tt := range tests {
		if name := s.EffectByID(tt.id); name != tt.name {
			t.Errorf("GetEffectById(%d): expected %s, got %s", tt.id, tt.name, name)
		}

		if id := s.EffectIDByName(tt.name); id != tt.id {
			t.Errorf("GetEffectIdByName(%s): expected %d, got %d", tt.name, tt.id, id)
		}

		// Case insensitivity
		if id := s.EffectIDByName(strings.ToLower(tt.name)); id != tt.id {
			t.Errorf("Case insensitive GetEffectIdByName failed for %s", tt.name)
		}
	}

	if name := s.EffectByID(999); name != "" {
		t.Errorf("expected empty for unknown effect, got %s", name)
	}
}

func TestGetSpellIdByName(t *testing.T) {
	s := New(minimalRawSchema())

	tests := []struct {
		name       string
		expected   sku.Spell
		shouldFind bool
	}{
		{"Exorcism", sku.Spell{Attribute: 1009, Value: 1}, true},
		{"Voices from Below", sku.Spell{Attribute: 1006, Value: 1}, true},
		{"Spectral Spectrum", sku.Spell{Attribute: 1004, Value: 3}, true},
		{"voices from below", sku.Spell{Attribute: 1006, Value: 1}, true}, // Case insensitivity
		{"Nonexistent", sku.Spell{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spell, ok := s.SpellIDByName(tt.name)
			if ok != tt.shouldFind {
				t.Errorf("GetSpellIdByName(%s) ok = %v, want %v", tt.name, ok, tt.shouldFind)
			}

			if ok && spell != tt.expected {
				t.Errorf("GetSpellIdByName(%s) = %+v, want %+v", tt.name, spell, tt.expected)
			}
		})
	}
}

func TestGetSkinByIdAndName(t *testing.T) {
	s := New(minimalRawSchema())

	if name := s.SkinByID(15013); name != "Pistol Skin" {
		t.Errorf("expected Pistol Skin, got %s", name)
	}

	if name := s.SkinByID(999); name != "" {
		t.Errorf("expected empty, got %s", name)
	}

	if id := s.SkinIDByName("Pistol Skin"); id != 15013 {
		t.Errorf("expected 15013, got %d", id)
	}

	if id := s.SkinIDByName("pistol skin"); id != 15013 {
		t.Errorf("case insensitive failed, got %d", id)
	}
}

func TestCheckExistence(t *testing.T) {
	s := New(minimalRawSchema())

	tests := []struct {
		name     string
		item     *sku.Item
		expected bool
	}{
		{"Valid unique weapon", &sku.Item{Defindex: 5021, Quality: QualityUnique}, true},
		{"Invalid quality for weapon", &sku.Item{Defindex: 5021, Quality: 0}, false},
		{"Valid crate with series", &sku.Item{Defindex: 5022, Quality: QualityUnique, Crateseries: 1}, true},
		{
			"Invalid crate with extra attrs",
			&sku.Item{Defindex: 5022, Quality: QualityUnique, Crateseries: 1, Killstreak: 1},
			false,
		},
		{"Valid seriesless crate", &sku.Item{Defindex: 5739, Quality: QualityUnique}, true},
		{
			"Invalid seriesless crate with series",
			&sku.Item{Defindex: 5739, Quality: QualityUnique, Crateseries: 5},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := s.CheckExistence(tt.item)
			if result != tt.expected {
				t.Errorf("CheckExistence() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetName_EdgeCases(t *testing.T) {
	s := New(minimalRawSchema())

	tests := []struct {
		desc      string
		item      *sku.Item
		scmFormat bool
		expected  string
	}{
		{
			desc: "Basic Crate",
			item: &sku.Item{
				Defindex:    5022,
				Quality:     QualityUnique,
				Crateseries: 1,
				Craftable:   true,
				Tradable:    true,
			},
			expected: "Mann Co. Supply Crate #1",
		},
		{
			desc: "Specialized Killstreak",
			item: &sku.Item{
				Defindex:    5022,
				Quality:     QualityUnique,
				Crateseries: 1,
				Killstreak:  2,
				Craftable:   true,
				Tradable:    true,
			},
			expected: "Specialized Killstreak Mann Co. Supply Crate #1",
		},
		{
			desc:     "Strange Weapon",
			item:     &sku.Item{Defindex: 5021, Quality: QualityStrange, Craftable: true, Tradable: true},
			expected: "Strange Scattergun",
		},
		{
			desc:     "Unusual Weapon without SCM Format",
			item:     &sku.Item{Defindex: 5021, Quality: QualityUnusual, Effect: 33, Craftable: true, Tradable: true},
			expected: "Orbiting Fire Scattergun",
		},
		{
			desc:      "Unusual Weapon with SCM Format",
			item:      &sku.Item{Defindex: 5021, Quality: QualityUnusual, Effect: 33, Craftable: true, Tradable: true},
			scmFormat: true,
			expected:  "Unusual Scattergun",
		},
		{
			desc: "Australium",
			item: &sku.Item{
				Defindex:   5021,
				Quality:    QualityUnique,
				Australium: true,
				Craftable:  true,
				Tradable:   true,
			},
			expected: "Australium Scattergun",
		},
		{
			desc:     "Non-Craftable",
			item:     &sku.Item{Defindex: 5021, Quality: QualityUnique, Craftable: false, Tradable: true},
			expected: "Non-Craftable Scattergun",
		},
		{
			desc:     "Non-Tradable",
			item:     &sku.Item{Defindex: 5021, Quality: QualityUnique, Craftable: true, Tradable: false},
			expected: "Non-Tradable Scattergun",
		},
		{
			desc: "Festivized",
			item: &sku.Item{
				Defindex:   5021,
				Quality:    QualityUnique,
				Festivized: true,
				Craftable:  true,
				Tradable:   true,
			},
			expected: "Festivized Scattergun",
		},
		{
			desc: "Craft Number",
			item: &sku.Item{
				Defindex:    5021,
				Quality:     QualityUnique,
				Craftnumber: 42,
				Craftable:   true,
				Tradable:    true,
			},
			expected: "Scattergun #42",
		},
		{
			desc: "Elevated Quality (Strange Unusual)",
			item: &sku.Item{
				Defindex:  378,
				Quality:   QualityUnusual,
				Quality2:  11,
				Effect:    33,
				Craftable: true,
				Tradable:  true,
			},
			expected: "Strange Orbiting Fire Team Captain",
		},
		{
			desc:     "Kit Target",
			item:     &sku.Item{Defindex: 6526, Quality: QualityUnique, Target: 5021, Craftable: true, Tradable: true},
			expected: "Scattergun Professional Killstreak Kit",
		},
		{
			desc: "Wear (Factory New Skin)",
			item: &sku.Item{
				Defindex:  15013,
				Quality:   QualityDecorated,
				Paintkit:  102,
				Wear:      1,
				Craftable: true,
				Tradable:  true,
			},
			expected: "Woodsy Widowmaker Pistol (Factory New)",
		},
		{
			desc: "Spells",
			item: &sku.Item{
				Defindex:  5021,
				Quality:   QualityUnique,
				Craftable: true,
				Tradable:  true,
				Spells:    []sku.Spell{{Attribute: 1009, Value: 1}, {Attribute: 1004, Value: 3}},
			},
			expected: "Scattergun (Spell: Exorcism) (Spell: Spectral Spectrum)",
		},
		{
			desc: "Strange Parts",
			item: &sku.Item{
				Defindex:  5021,
				Quality:   QualityStrange,
				Craftable: true,
				Tradable:  true,
				Parts:     []int{0}, // 0 is "Kills" in minimalRawSchema
			},
			expected: "Strange Scattergun (Kills: 0)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			name := s.ItemName(tt.item, true, false, tt.scmFormat)
			if name != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, name)
			}
		})
	}
}

func TestGetItemObjectFromName_EdgeCases(t *testing.T) {
	s := New(minimalRawSchema())

	tests := []struct {
		name     string
		expected *sku.Item
	}{
		{
			"Scattergun",
			&sku.Item{Defindex: 5021, Quality: QualityUnique, Craftable: true, Tradable: true},
		},
		{
			"Strange Scattergun",
			&sku.Item{Defindex: 5021, Quality: QualityStrange, Craftable: true, Tradable: true},
		},
		{
			"Mann Co. Supply Crate #1",
			&sku.Item{Defindex: 5022, Quality: QualityUnique, Crateseries: 1, Craftable: true, Tradable: true},
		},
		{
			"Orbiting Fire Scattergun",
			&sku.Item{Defindex: 5021, Quality: QualityUnusual, Effect: 33, Craftable: true, Tradable: true},
		},
		{
			"Specialized Killstreak Scattergun",
			&sku.Item{Defindex: 5021, Quality: QualityUnique, Killstreak: 2, Craftable: true, Tradable: true},
		},
		{
			"Australium Scattergun",
			&sku.Item{Defindex: 5021, Quality: QualityUnique, Australium: true, Craftable: true, Tradable: true},
		},
		{
			"Non-Craftable Scattergun",
			&sku.Item{Defindex: 5021, Quality: QualityUnique, Craftable: false, Tradable: true},
		},
		{
			"Non-Tradable Scattergun",
			&sku.Item{Defindex: 5021, Quality: QualityUnique, Craftable: true, Tradable: false},
		},
		{
			"Festivized Scattergun",
			&sku.Item{Defindex: 5021, Quality: QualityUnique, Festivized: true, Craftable: true, Tradable: true},
		},
		{
			"Team Captain #1337",
			&sku.Item{Defindex: 378, Quality: QualityUnique, Craftnumber: 1337, Craftable: true, Tradable: true},
		},
		{
			"Professional Killstreak Kit Scattergun",
			&sku.Item{Defindex: 6526, Quality: QualityUnique, Target: 5021, Craftable: true, Tradable: true},
		},
		{
			"Woodsy Widowmaker Pistol (Field-Tested)",
			&sku.Item{
				Defindex:  15013,
				Quality:   QualityDecorated,
				Paintkit:  102,
				Wear:      3,
				Craftable: true,
				Tradable:  true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := s.ItemFromName(tt.name)

			// Compare essential fields
			if item.Defindex != tt.expected.Defindex ||
				item.Quality != tt.expected.Quality ||
				item.Killstreak != tt.expected.Killstreak ||
				item.Craftable != tt.expected.Craftable ||
				item.Tradable != tt.expected.Tradable ||
				item.Australium != tt.expected.Australium ||
				item.Festivized != tt.expected.Festivized ||
				item.Craftnumber != tt.expected.Craftnumber ||
				item.Target != tt.expected.Target ||
				item.Wear != tt.expected.Wear {
				t.Errorf("GetItemObjectFromName(%q) mismatch.\nExpected: %+v\nGot: %+v", tt.name, tt.expected, item)
			}
		})
	}
}

func TestGetSkuFromName(t *testing.T) {
	s := New(minimalRawSchema())

	tests := []struct {
		name     string
		expected string
	}{
		{"Scattergun", "5021;6"},
		{"Strange Scattergun", "5021;11"},
		{"Non-Craftable Scattergun", "5021;6;uncraftable"},
		{"Specialized Killstreak Scattergun", "5021;6;kt-2"},
		{"Orbiting Fire Team Captain", "378;5;u33"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			skuStr := s.SkuFromName(tt.name)
			if skuStr != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, skuStr)
			}
		})
	}
}

func TestCrateSeriesList(t *testing.T) {
	s := New(minimalRawSchema())

	series := s.CrateSeriesList()
	if val, ok := series[5022]; !ok || val != 1 {
		t.Errorf("expected series 1 for def 5022, got %v", val)
	}

	if _, ok := series[5739]; ok {
		t.Errorf("did not expect def 5739 (seriesless) to be in series list")
	}
}

func TestGetCraftableWeaponsSchema(t *testing.T) {
	s := New(minimalRawSchema())
	weapons := s.CraftableWeaponsSchema()

	if len(weapons) != 2 {
		t.Errorf("expected 2 weapons, got %d", len(weapons))
	}

	foundScattergun := false

	for _, w := range weapons {
		if w.Defindex == 5021 {
			foundScattergun = true
			break
		}
	}

	if !foundScattergun {
		t.Error("scattergun not found in craftable weapons")
	}
}

func TestGetWeaponsForCraftingByClass(t *testing.T) {
	s := New(minimalRawSchema())

	skus := s.WeaponsForCraftingByClass("Scout")
	if len(skus) != 2 || skus[0] != "5021;6" || skus[1] != "38;6" {
		t.Errorf("expected[5021;6 38;6], got %v", skus)
	}

	skusDemo := s.WeaponsForCraftingByClass("Demoman")
	if len(skusDemo) != 0 {
		t.Errorf("expected[], got %v", skusDemo)
	}
}

func TestGetUnusualEffects(t *testing.T) {
	s := New(minimalRawSchema())
	effects := s.UnusualEffects()

	// Should include all non-empty effects
	if len(effects) < 4 {
		t.Errorf("expected at least 4 effects, got %d", len(effects))
	}

	found := false

	for _, e := range effects {
		if e.Name == "Orbiting Fire" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Orbiting Fire not found in effects list")
	}
}

func TestGetPaintableItemDefindexes(t *testing.T) {
	s := New(minimalRawSchema())
	paintable := s.PaintableItemDefindexes()

	if len(paintable) == 0 {
		t.Fatal("expected at least 1 paintable item")
	}

	if !slices.Contains(paintable, 378) {
		t.Error("Team Captain (378) not found in paintable item defindexes")
	}
}

func createMockSchema() *Schema {
	items := []*Item{
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

	raw := &Raw{}
	raw.Schema.Items = items

	s := &Schema{
		Raw:             raw,
		itemsByDef:      make(map[int]*Item),
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
		item     *Item
		expected bool
	}{
		{
			name:     "Valid Promo Item",
			item:     &Item{Name: "Promo AWPer Hand", CraftClass: ""},
			expected: true,
		},
		{
			name:     "Has Promo prefix but has CraftClass",
			item:     &Item{Name: "Promo Hat", CraftClass: "hat"},
			expected: false,
		},
		{
			name:     "Empty CraftClass but no Promo prefix",
			item:     &Item{Name: "AWPer Hand", CraftClass: ""},
			expected: false,
		},
		{
			name:     "Regular item",
			item:     &Item{Name: "Scattergun", CraftClass: "weapon"},
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
			input:    sku.Item{Defindex: 5049},
			expected: sku.Item{Defindex: 5021},
		},
		{
			name:     "Lugermorph standardization",
			input:    sku.Item{Defindex: 294},
			expected: sku.Item{Defindex: 160},
		},
		{
			name:     "Grouping Killstreak Kits",
			input:    sku.Item{Defindex: 6520},
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

	for i := range tests {
		tt := &tests[i]
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("Before: %d, Normalized: %d", tt.input.Defindex, s.NormalizeDefindex(tt.input.Defindex))
			s.NormalizeItem(&tt.input)

			if !reflect.DeepEqual(tt.input, tt.expected) {
				t.Errorf("\n        Got:\n        %+v\n        Want:\n        %+v", tt.input, tt.expected)
			}
		})
	}
}

func TestSchema_Getters(t *testing.T) {
	s := New(minimalRawSchema())

	t.Run("Qualities", func(t *testing.T) {
		assert.Equal(t, "Unique", s.QualityByID(6))
		assert.Equal(t, 6, s.QualityIDByName("Unique"))
		assert.NotEmpty(t, s.Qualities())
	})

	t.Run("Effects", func(t *testing.T) {
		assert.Equal(t, "Orbiting Fire", s.EffectByID(33))
		assert.Equal(t, 33, s.EffectIDByName("Orbiting Fire"))
		assert.NotEmpty(t, s.ParticleEffects())
	})

	t.Run("Skins", func(t *testing.T) {
		assert.Equal(t, "Woodsy Widowmaker", s.SkinByID(102))
		assert.Equal(t, 102, s.SkinIDByName("Woodsy Widowmaker"))
		assert.NotEmpty(t, s.PaintKits())
	})

	t.Run("Items", func(t *testing.T) {
		item := s.ItemByDef(5021)
		assert.NotNil(t, item)
		assert.Equal(t, "Scattergun", item.ItemName)

		itemByName := s.ItemByName("Scattergun")
		assert.Equal(t, item, itemByName)
	})
}

func TestGetSKUFromEconItem_Variations(t *testing.T) {
	s := New(minimalRawSchema())

	tests := []struct {
		name     string
		item     *trading.Item
		expected string
	}{
		{
			name: "Basic Unique Weapon",
			item: &trading.Item{
				MarketHashName: "Scattergun",
				Tradable:       true,
				Descriptions:   []trading.Description{},
			},
			expected: "5021;6",
		},
		{
			name: "Non-Craftable Unique Weapon",
			item: &trading.Item{
				MarketHashName: "Scattergun",
				Tradable:       true,
				Descriptions: []trading.Description{
					{Value: "( Not Usable in Crafting )"},
				},
			},
			expected: "5021;6;uncraftable",
		},
		{
			name: "Strange Unusual Hat with Effect",
			item: &trading.Item{
				MarketHashName: "Strange Unusual Team Captain",
				Tradable:       true,
				Descriptions: []trading.Description{
					{Value: "★ Unusual Effect: Orbiting Fire"},
				},
			},
			expected: "378;5;u33;strange",
		},
		{
			name: "Strange Unusual Team Captain",
			item: &trading.Item{
				MarketHashName: "Strange Unusual Team Captain",
				Tradable:       true,
				Descriptions: []trading.Description{
					{Value: "★ Unusual Effect: Burning Flames"},
				},
			},
			expected: "378;5;u13;strange",
		},
		{
			name: "Item with Halloween Spell",
			item: &trading.Item{
				MarketHashName: "Scattergun",
				Tradable:       true,
				Descriptions: []trading.Description{
					{Value: "Halloween: Exorcism", Color: "7ea9d1"},
				},
			},
			expected: "5021;6;s-1009-1",
		},
		{
			name: "Item with Multiple Halloween Spells",
			item: &trading.Item{
				MarketHashName: "Scattergun",
				Tradable:       true,
				Descriptions: []trading.Description{
					{Value: "Halloween: Exorcism", Color: "7ea9d1"},
					{Value: "Halloween: Spectral Spectrum (paint)", Color: "7ea9d1"},
				},
			},
			expected: "5021;6;s-1009-1;s-1004-3",
		},
		{
			name: "Decorated Weapon (Skin) with Wear",
			item: &trading.Item{
				MarketHashName: "Woodsy Widowmaker Pistol (Factory New)",
				Tradable:       true,
				Descriptions:   []trading.Description{},
			},
			expected: "15013;15;w1;pk102",
		},
		{
			name: "Unusual Decorated Weapon",
			item: &trading.Item{
				MarketHashName: "Unusual Woodsy Widowmaker Pistol (Minimal Wear)",
				Tradable:       true,
				Descriptions: []trading.Description{
					{Value: "★ Unusual Effect: Orbiting Fire"},
				},
			},
			expected: "15013;15;u33;w2;pk102",
		},
		{
			name: "War Paint",
			item: &trading.Item{
				MarketHashName: "Woodsy Widowmaker War Paint (Field-Tested)",
				Tradable:       true,
				Descriptions:   []trading.Description{},
			},
			expected: "16189;15;w3;pk102",
		},
		{
			name: "Woodsy Widowmaker Pistol (Factory New)",
			item: &trading.Item{
				MarketHashName: "Woodsy Widowmaker Pistol (Factory New)",
				Tradable:       true,
				Tags: []trading.Tag{
					{Category: "Quality", LocalizedName: "Decorated Weapon"},
					{Category: "Exterior", LocalizedName: "Factory New"},
				},
			},
			expected: "15013;15;w1;pk102",
		},
		{
			name: "Strange Unusual Team Captain",
			item: &trading.Item{
				MarketHashName: "Strange Unusual Team Captain",
				Tradable:       true,
				Descriptions: []trading.Description{
					{Value: "★ Unusual Effect: Burning Flames"},
				},
			},
			expected: "378;5;u13;strange",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.SKUFromEconItem(tt.item)
			t.Logf("Name: %s, Got SKU: %s, Descriptions: %d", tt.name, got, len(tt.item.Descriptions))

			if got != tt.expected {
				t.Errorf("GetSKUFromEconItem() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestGetItemObjectFromName_MoreVariations(t *testing.T) {
	s := New(minimalRawSchema())

	tests := []struct {
		input         string
		defindex      int
		quality       int
		quality2      int
		target        int
		output        int
		outputQuality int
		killstreak    int
	}{
		{
			input:    "The Scattergun",
			defindex: 5021,
			quality:  QualityUnique,
		},
		{
			input:    "Strange The Scattergun",
			defindex: 5021,
			quality:  QualityStrange,
		},
		{
			input:    "Unusual The Team Captain",
			defindex: 378,
			quality:  QualityUnusual,
		},
		{
			input:    "Non-Craftable The Scattergun",
			defindex: 5021,
			quality:  QualityUnique,
		},
		{
			input:    "Unusualifier Scattergun",
			defindex: 9258,
			quality:  QualityUnusual,
			target:   5021,
		},
		{
			input:    "Professional Killstreak Kit Fabricator Scattergun",
			defindex: 20003,
			quality:  QualityUnique,
			target:   5021,
			output:   6526,
		},
		{
			input:    "Specialized Killstreak Kit Fabricator Scattergun",
			defindex: 20002,
			quality:  QualityUnique,
			target:   5021,
			output:   6523,
		},
		{
			input:         "Collector's Chemistry Set Scattergun",
			defindex:      20006,
			quality:       QualityUnique,
			output:        5021,
			outputQuality: 14,
		},
		{
			input:         "Strangifier Chemistry Set Scattergun",
			defindex:      20000,
			quality:       QualityUnique,
			target:        5021,
			output:        6522,
			outputQuality: QualityUnique,
		},
		{
			input:    "Strangifier Scattergun",
			defindex: 6522,
			quality:  QualityUnique,
			target:   5021,
		},
		{
			input:      "Professional Killstreak Kit Scattergun",
			defindex:   6526,
			quality:    QualityUnique,
			target:     5021,
			killstreak: 0, // В китах killstreak сбрасывается в 0, а defindex меняется на 6526
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := s.ItemFromName(tt.input)
			if got == nil {
				t.Fatalf("failed to parse %q", tt.input)
			}

			assert.Equal(t, tt.defindex, got.Defindex, "Defindex mismatch for %s", tt.input)
			assert.Equal(t, tt.quality, got.Quality, "Quality mismatch for %s", tt.input)

			if tt.target != 0 {
				assert.Equal(t, tt.target, got.Target, "Target mismatch for %s", tt.input)
			}

			if tt.output != 0 {
				assert.Equal(t, tt.output, got.Output, "Output mismatch for %s", tt.input)
			}

			if tt.outputQuality != 0 {
				assert.Equal(t, tt.outputQuality, got.OutputQuality, "OutputQuality mismatch for %s", tt.input)
			}
		})
	}
}

func TestNormalizeItem_Advanced(t *testing.T) {
	s := createMockSchema()

	t.Run("Strange Unusual Decoration", func(t *testing.T) {
		item := &sku.Item{
			Defindex: 100,
			Quality:  QualityStrange,
			Effect:   13,
			Paintkit: 102,
		}
		s.NormalizeItem(item)

		assert.Equal(t, QualityDecorated, item.Quality, "Should be Decorated (15)")
		assert.Equal(t, QualityStrange, item.Quality2, "Should have Elevated Strange (11)")
	})

	t.Run("Australium Normalization", func(t *testing.T) {
		item := &sku.Item{Defindex: 45, Quality: QualityStrange, Australium: true}
		assert.True(t, s.IsAustraliumDefindex(item.Defindex))
	})
}

func TestGetStrangeParts_Mapping(t *testing.T) {
	s := New(minimalRawSchema())

	s.Raw.Schema.KillEaterScoreTypes = append(s.Raw.Schema.KillEaterScoreTypes, &KillEaterScoreType{
		Type: 10, TypeName: "Airborne Enemies Killed",
	})

	parts := s.StrangeParts()
	assert.Equal(t, "sp10", parts["Airborne Enemies Killed"])
}

func TestGetItemByNameWithThe_SpecialCases(t *testing.T) {
	s := New(minimalRawSchema())

	item := s.ItemByNameWithThe("The Scattergun")
	assert.NotNil(t, item)
	assert.Equal(t, 5021, item.Defindex)

	s.Raw.Schema.Items = append(s.Raw.Schema.Items, &Item{
		Defindex: 0, ItemName: "Scattergun", ItemQuality: 0,
	})
	s.buildIndices()

	itemStock := s.ItemByName("Scattergun")
	assert.Equal(t, 5021, itemStock.Defindex)
}
