// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package schema

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lemon4ksan/g-man/pkg/tf2/sku"
	"github.com/lemon4ksan/g-man/pkg/trading"
)

var debugLog = func(v ...any) {
	if os.Getenv("DEBUG_SCHEMA") == "true" {
		log.Println(v...)
	}
}

// Raw is the raw schema data from the API.
type Raw struct {
	Schema struct {
		Items                                []*Item               `json:"items"`
		Attributes                           []*AttributeSchema    `json:"attributes"`
		Qualities                            map[string]int        `json:"qualities"`
		QualityNames                         map[string]string     `json:"qualityNames"` // Note: Some API responses omit this
		OriginNames                          []*OriginName         `json:"originNames"`
		ItemSets                             []*ItemSet            `json:"item_sets"`
		AttributeControlledAttachedParticles []*ParticleEffect     `json:"attribute_controlled_attached_particles"`
		ItemLevels                           []*ItemLevel          `json:"item_levels"`
		KillEaterScoreTypes                  []*KillEaterScoreType `json:"kill_eater_score_types"`
		StringLookups                        []*StringLookup       `json:"string_lookups"`
		PaintKits                            map[string]string     `json:"paintkits"` // Injected from protodefs
	} `json:"schema"`

	ItemsGame map[string]any `json:"items_game"` // Parsed items_game.txt (should be nilled after init)
}

// Item represents a single item definition.
type Item struct {
	Defindex      int             `json:"defindex"`
	Name          string          `json:"name"`
	ItemName      string          `json:"item_name"`
	ItemClass     string          `json:"item_class"`
	ItemQuality   int             `json:"item_quality"`
	ProperName    bool            `json:"proper_name"`
	CraftClass    string          `json:"craft_class"`
	Capabilities  *Capabilities   `json:"capabilities"`
	UsedByClasses []string        `json:"used_by_classes"`
	Attributes    []ItemAttribute `json:"attributes"`
}

// Capabilities defines what actions can be performed on the item.
type Capabilities struct {
	Paintable bool `json:"paintable"`
	Nameable  bool `json:"nameable"`
	CanCraft  bool `json:"can_craft_if_purchased"`
}

// ItemAttribute represents an attribute attached to an item.
// Memory Optimized: Removed `Value any` to avoid heap allocations.
type ItemAttribute struct {
	Name  string `json:"name"`
	Class string `json:"class"`

	// Steam uses float/int for 99% of attribute values.
	// We use float64 to safely decode both from JSON.
	Value float64 `json:"value"`

	// ValueString is used if the JSON value is a string (e.g., "#ItemDesc").
	ValueString string `json:"value_string,omitempty"`
}

// UnmarshalJSON custom unmarshaler to handle dynamic "value" types without allocations.
func (a *ItemAttribute) UnmarshalJSON(data []byte) error {
	// A temporary struct to capture everything except the dynamic "value"
	type Alias ItemAttribute

	aux := &struct {
		*Alias
		DynamicValue any `json:"value"`
	}{
		Alias: (*Alias)(a),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	switch v := aux.DynamicValue.(type) {
	case float64:
		a.Value = v
	case int:
		a.Value = float64(v)
	case string:
		a.ValueString = v
	}

	return nil
}

// AttributeSchema defines what a specific attribute ID means.
type AttributeSchema struct {
	Defindex        int    `json:"defindex"`
	Name            string `json:"name"`
	AttributeClass  string `json:"attribute_class"`
	Description     string `json:"description_string"`
	DescriptionFmt  string `json:"description_format"`
	EffectType      string `json:"effect_type"`
	Hidden          bool   `json:"hidden"`
	StoredAsInteger bool   `json:"stored_as_integer"`
}

// ParticleEffect represents Unusual and Killstreak eye effects.
type ParticleEffect struct {
	ID               int    `json:"id"`
	System           string `json:"system"`
	AttachToRootbone bool   `json:"attach_to_rootbone"`
	Name             string `json:"name"`
}

// KillEaterScoreType represents strange parts and counters (e.g., Kills, Headshots).
type KillEaterScoreType struct {
	Type      int    `json:"type"`
	TypeName  string `json:"type_name"`
	LevelData string `json:"level_data"`
}

// ItemSet defines a collection of items that form a set (e.g., The Saharan Spy).
type ItemSet struct {
	ItemSet    string          `json:"item_set"`
	Name       string          `json:"name"`
	Items      []string        `json:"items"`
	Attributes []ItemAttribute `json:"attributes"`
}

// OriginName maps an origin ID to its display name (e.g., 0 = Timed Drop, 4 = Crafted).
type OriginName struct {
	Origin int    `json:"origin"`
	Name   string `json:"name"`
}

// ItemLevel represents strange rank thresholds (e.g., Hale's Own).
type ItemLevel struct {
	Name   string `json:"name"`
	Levels []struct {
		Level         int    `json:"level"`
		RequiredScore int    `json:"required_score"`
		Name          string `json:"name"`
	} `json:"levels"`
}

// StringLookup contains lookup tables for string-based attributes (like Spells!).
type StringLookup struct {
	TableName string `json:"table_name"`
	Strings   []struct {
		Index  int    `json:"index"`
		String string `json:"string"`
	} `json:"strings"`
}

// Schema is the main type.
type Schema struct {
	Version string
	Raw     *Raw
	Time    time.Time

	// Primary indices - O(1) lookups
	itemsByDef  map[int]*Item
	itemsByName map[string]*Item

	// Attribute indices - O(1) lookups
	attrsByDef map[int]*AttributeSchema

	// Quality indices
	qualByID   map[int]string
	qualByName map[string]int

	// Effect indices
	effByID   map[int]string
	effByName map[string]int

	// Paint kit indices
	paintKitByID   map[int]string
	paintKitByName map[string]int

	// Paint indices
	paintByDecimal map[int]string
	paintByName    map[string]int

	// Crate series
	crateSeriesList map[int]int

	// Name indices without "The "
	itemsByNameStripped map[string]*Item

	// Reverse spell mapping
	spellsByName map[string]sku.Spell
	spellsByID   map[string]string
}

// New creates a Schema from the given raw data and builds all indices.
func New(raw *Raw) *Schema {
	s := &Schema{
		Raw:            raw,
		itemsByDef:     make(map[int]*Item),
		itemsByName:    make(map[string]*Item),
		attrsByDef:     make(map[int]*AttributeSchema),
		qualByID:       make(map[int]string),
		qualByName:     make(map[string]int),
		effByID:        make(map[int]string),
		effByName:      make(map[string]int),
		paintKitByID:   make(map[int]string),
		paintKitByName: make(map[string]int),
		paintByDecimal: make(map[int]string),
		paintByName:    make(map[string]int),
		spellsByName:   make(map[string]sku.Spell),
		spellsByID:     make(map[string]string),
	}
	s.buildIndices()
	s.buildSpellIndices()

	return s
}

// buildIndices creates all O(1) lookup maps from the raw data.
func (s *Schema) buildIndices() {
	// Item indices
	s.itemsByNameStripped = make(map[string]*Item)

	for _, item := range s.Raw.Schema.Items {
		lowName := strings.ToLower(item.ItemName)
		s.itemsByDef[item.Defindex] = item

		if item.ItemQuality == 0 || (item.ItemName == "Name Tag" && item.Defindex == 2093) {
			continue
		}

		if _, exists := s.itemsByName[lowName]; !exists {
			s.itemsByName[lowName] = item
		}

		stripped := strings.TrimPrefix(lowName, "the ")
		if _, exists := s.itemsByNameStripped[stripped]; !exists {
			s.itemsByNameStripped[stripped] = item
		}
	}

	// Attribute indices
	for _, attr := range s.Raw.Schema.Attributes {
		s.attrsByDef[attr.Defindex] = attr
	}

	// Quality indices (bidirectional)
	for qType, id := range s.Raw.Schema.Qualities {
		if name, ok := s.Raw.Schema.QualityNames[qType]; ok {
			s.qualByID[id] = name
			s.qualByName[strings.ToLower(name)] = id
		}
	}

	// Effect indices (bidirectional) with special cases
	seenEffects := make(map[string]bool)

	for _, eff := range s.Raw.Schema.AttributeControlledAttachedParticles {
		if eff.Name == "" {
			continue
		}

		if !seenEffects[eff.Name] {
			s.effByID[eff.ID] = eff.Name
			s.effByName[strings.ToLower(eff.Name)] = eff.ID
			seenEffects[eff.Name] = true

			// Special case mappings from original JS
			switch eff.Name {
			case "Eerie Orbiting Fire":
				s.effByName["orbiting fire"] = 33
				s.effByID[33] = "Orbiting Fire"
			case "Nether Trail":
				s.effByName["ether trail"] = 103
				s.effByID[103] = "Ether Trail"
			case "Refragmenting Reality":
				s.effByName["fragmenting reality"] = 141
				s.effByID[141] = "Fragmenting Reality"
			}
		}
	}

	// Paint kit indices (bidirectional)
	for idStr, name := range s.Raw.Schema.PaintKits {
		if id, err := strconv.Atoi(idStr); err == nil {
			s.paintKitByID[id] = name
			s.paintKitByName[strings.ToLower(name)] = id
		}
	}

	// Paint indices (bidirectional)
	for _, it := range s.Raw.Schema.Items {
		if strings.Contains(it.Name, "Paint Can") && it.Name != "Paint Can" && it.Attributes != nil {
			if len(it.Attributes) > 0 {
				decimal := int(it.Attributes[0].Value)

				s.paintByDecimal[decimal] = it.ItemName
				s.paintByName[strings.ToLower(it.ItemName)] = decimal
			}
		}
	}

	s.paintByDecimal[5801378] = "Legacy Paint"
	s.paintByName["legacy paint"] = 5801378

	s.crateSeriesList = s.buildCrateSeriesList()
	s.buildSpellIndices()

	s.Raw.ItemsGame = nil
}

// buildSpellIndices creates a reverse lookup for spells.
func (s *Schema) buildSpellIndices() {
	s.spellsByName = make(map[string]sku.Spell)
	s.spellsByID = make(map[string]string)

	for name, spell := range SpellDefinitions {
		lowerName := strings.ToLower(name)
		s.spellsByName[lowerName] = spell

		// Index by ID
		idKey := fmt.Sprintf("%d-%d", spell.Attribute, spell.Value)
		s.spellsByID[idKey] = name

		// Also index without "Halloween: " prefix and common suffixes for convenience
		if strings.HasPrefix(lowerName, "halloween: ") {
			shortName := strings.TrimPrefix(lowerName, "halloween: ")
			s.spellsByName[shortName] = spell

			// Strip common suffixes like "(paint)", "(footprints)", etc.
			if idx := strings.Index(shortName, " ("); idx != -1 {
				veryShortName := shortName[:idx]
				s.spellsByName[veryShortName] = spell
			}
		}
	}
}

// buildCrateSeriesList builds the crate series map efficiently.
func (s *Schema) buildCrateSeriesList() map[int]int {
	series := make(map[int]int)

	// From schema items
	for _, it := range s.Raw.Schema.Items {
		if it.Attributes != nil {
			for _, attr := range it.Attributes {
				if attr.Name == "set supply crate series" {
					series[it.Defindex] = int(it.Attributes[0].Value)
					break
				}
			}
		}
	}

	// From items_game
	if s.Raw.ItemsGame != nil {
		if items, ok := s.Raw.ItemsGame["items"].(map[string]any); ok {
			for defindexStr, item := range items {
				defindex, err := strconv.Atoi(defindexStr)
				if err != nil {
					continue
				}

				if _, ok := series[defindex]; ok {
					continue
				}

				itemMap, ok := item.(map[string]any)
				if !ok {
					continue
				}

				if staticAttrs, ok := itemMap["static_attrs"].(map[string]any); ok {
					if val, ok := staticAttrs["set supply crate series"]; ok {
						switch v := val.(type) {
						case float64:
							series[defindex] = int(v)
						case int:
							series[defindex] = v
						case map[string]any:
							if vv, ok := v["value"]; ok {
								if f, ok := vv.(float64); ok {
									series[defindex] = int(f)
								}
							}
						}
					}
				}
			}
		}
	}

	return series
}

// GetItemByDef returns the item with the given defindex.
func (s *Schema) GetItemByDef(def int) *Item {
	return s.itemsByDef[def]
}

// GetItemByName returns the item with the given name.
func (s *Schema) GetItemByName(name string) *Item {
	return s.itemsByName[strings.ToLower(name)]
}

// GetAttributeByDef returns the attribute with the given defindex.
func (s *Schema) GetAttributeByDef(def int) *AttributeSchema {
	return s.attrsByDef[def]
}

// GetQualityById returns the quality name with the given id.
func (s *Schema) GetQualityById(id int) string {
	return s.qualByID[id]
}

// GetQualityIdByName returns the quality id with the given name.
func (s *Schema) GetQualityIdByName(name string) int {
	return s.qualByName[strings.ToLower(name)]
}

// GetEffectById returns the effect name with the given id.
func (s *Schema) GetEffectById(id int) string {
	return s.effByID[id]
}

// GetEffectIdByName returns the ID for a particle effect name.
func (s *Schema) GetEffectIdByName(name string) int {
	return s.effByName[strings.ToLower(name)]
}

// GetSkinById returns the skin name with the given id.
func (s *Schema) GetSkinById(id int) string {
	return s.paintKitByID[id]
}

// GetSkinIdByName returns the skin id with the given name.
func (s *Schema) GetSkinIdByName(name string) int {
	return s.paintKitByName[strings.ToLower(name)]
}

// GetPaintNameByDecimal returns the paint name with the given decimal value.
func (s *Schema) GetPaintNameByDecimal(decimal int) string {
	if name, ok := s.paintByDecimal[decimal]; ok {
		return name
	}

	if name, ok := StandardPaints[uint32(decimal)]; ok {
		return name
	}

	if decimal == 0 {
		return ""
	}

	return fmt.Sprintf("#%06X", decimal)
}

// GetPaintDecimalByName returns the paint decimal value with the given name.
func (s *Schema) GetPaintDecimalByName(name string) int {
	return s.paintByName[strings.ToLower(name)]
}

// GetItemByNameWithThe tries to find an item after stripping "The " from the name.
func (s *Schema) GetItemByNameWithThe(name string) *Item {
	name = strings.ToLower(name)
	name = strings.TrimPrefix(name, "the ")
	name = strings.TrimSpace(name)

	return s.itemsByNameStripped[name]
}

// GetItemBySKU returns the item for a given SKU string.
func (s *Schema) GetItemBySKU(itemSku string) *Item {
	item, err := sku.FromString(itemSku)
	if err != nil {
		return nil
	}

	return s.GetItemByDef(item.Defindex)
}

// GetUnusualEffects returns all unusual effects as name-id pairs.
func (s *Schema) GetUnusualEffects() []struct {
	Name string
	ID   int
} {
	out := make([]struct {
		Name string
		ID   int
	}, 0, len(s.effByID))

	for id, name := range s.effByID {
		out = append(out, struct {
			Name string
			ID   int
		}{name, id})
	}

	return out
}

// GetPaints returns a map of paint name to decimal value.
func (s *Schema) GetPaints() map[string]int {
	return s.paintByName
}

// GetPaintableItemDefindexes returns defindexes of items that can be painted.
func (s *Schema) GetPaintableItemDefindexes() []int {
	var out []int

	for _, it := range s.Raw.Schema.Items {
		if it.Capabilities != nil && it.Capabilities.Paintable {
			out = append(out, it.Defindex)
		}
	}

	return out
}

// GetStrangeParts returns a map of strange part names to their SKU suffix.
func (s *Schema) GetStrangeParts() map[string]string {
	partsToExclude := map[string]bool{
		"Ubers": true, "Kill Assists": true, "Sentry Kills": true,
		"Sodden Victims": true, "Spies Shocked": true, "Heads Taken": true,
		"Humiliations": true, "Gifts Given": true, "Deaths Feigned": true,
		"Buildings Sapped": true, "Tickle Fights Won": true, "Opponents Flattened": true,
		"Food Items Eaten": true, "Banners Deployed": true, "Seconds Cloaked": true,
		"Health Dispensed to Teammates": true, "Teammates Teleported": true,
		"KillEaterEvent_UniquePlayerKills": true, "Points Scored": true,
		"Double Donks": true, "Teammates Whipped": true, "Wrangled Sentry Kills": true,
		"Carnival Kills": true, "Carnival Underworld Kills": true, "Carnival Games Won": true,
		"Contracts Completed": true, "Contract Points": true, "Contract Bonus Points": true,
		"Times Performed": true, "Kills and Assists during Invasion Event": true,
		"Kills and Assists on 2Fort Invasion": true, "Kills and Assists on Probed": true,
		"Kills and Assists on Byre": true, "Kills and Assists on Watergate": true,
		"Souls Collected": true, "Merasmissions Completed": true,
		"Halloween Transmutes Performed": true, "Power Up Canteens Used": true,
		"Contract Points Earned": true, "Contract Points Contributed To Friends": true,
	}
	m := make(map[string]string)

	for _, p := range s.Raw.Schema.KillEaterScoreTypes {
		if partsToExclude[p.TypeName] || p.Type == 0 || p.Type == 97 {
			continue
		}

		m[p.TypeName] = fmt.Sprintf("sp%d", p.Type)
	}

	return m
}

// GetSpellName returns the human-readable name of a spell.
func (s *Schema) GetSpellName(spell sku.Spell) string {
	idKey := fmt.Sprintf("%d-%d", spell.Attribute, spell.Value)

	name, ok := s.spellsByID[idKey]
	if !ok {
		return fmt.Sprintf("Unknown Spell (%d-%d)", spell.Attribute, spell.Value)
	}

	// Clean up name for display
	name = strings.TrimPrefix(name, "Halloween: ")
	if idx := strings.Index(name, " ("); idx != -1 {
		name = name[:idx]
	}

	return name
}

// GetSpellIdByName returns the attribute and value IDs for a spell name.
func (s *Schema) GetSpellIdByName(name string) (sku.Spell, bool) {
	spell, ok := s.spellsByName[strings.ToLower(name)]
	return spell, ok
}

var weaponsToExclude = map[int]bool{
	266: true, 452: true, 466: true, 474: true,
	572: true, 574: true, 587: true, 638: true,
	735: true, 736: true, 737: true, 851: true,
	880: true, 933: true, 939: true, 947: true,
	1013: true, 1152: true, 30474: true,
}

// GetCraftableWeaponsSchema returns all craftable weapon items.
func (s *Schema) GetCraftableWeaponsSchema() []*Item {
	var out []*Item

	for _, it := range s.Raw.Schema.Items {
		if weaponsToExclude[it.Defindex] {
			continue
		}

		if it.ItemQuality == QualityUnique && it.CraftClass == "weapon" {
			out = append(out, it)
		}
	}

	return out
}

// GetWeaponsForCraftingByClass returns SKUs of craftable weapons usable by the given class.
func (s *Schema) GetWeaponsForCraftingByClass(class string) []string {
	validClasses := map[string]bool{
		"Scout": true, "Soldier": true, "Pyro": true, "Demoman": true,
		"Heavy": true, "Engineer": true, "Medic": true, "Sniper": true, "Spy": true,
	}
	if !validClasses[class] {
		panic(fmt.Sprintf("invalid class %q", class))
	}

	var out []string

	for _, it := range s.GetCraftableWeaponsSchema() {
		for _, uc := range it.UsedByClasses {
			if uc == class {
				out = append(out, fmt.Sprintf("%d;6", it.Defindex))
				break
			}
		}
	}

	return out
}

// GetCraftableWeaponsForTrading returns SKUs of all craftable weapons.
func (s *Schema) GetCraftableWeaponsForTrading() []string {
	weapons := s.GetCraftableWeaponsSchema()

	out := make([]string, 0, len(weapons))
	for _, it := range weapons {
		out = append(out, fmt.Sprintf("%d;6", it.Defindex))
	}

	return out
}

// GetUncraftableWeaponsForTrading returns SKUs of non‑craftable weapons.
func (s *Schema) GetUncraftableWeaponsForTrading() []string {
	exclude := map[int]bool{348: true, 349: true, 1178: true, 1179: true, 1180: true, 1181: true, 1190: true}

	var out []string

	for _, it := range s.GetCraftableWeaponsSchema() {
		if exclude[it.Defindex] {
			continue
		}

		out = append(out, fmt.Sprintf("%d;6;uncraftable", it.Defindex))
	}

	return out
}

// GetCrateSeriesList returns the crate series map.
func (s *Schema) GetCrateSeriesList() map[int]int {
	return s.crateSeriesList
}

// NormalizeDefindex returns the "canonical" defindex for an item.
func (s *Schema) NormalizeDefindex(defindex int) int {
	return NormalizeDefindex(defindex)
}

// IsAustraliumDefindex returns true if the defindex can be an Australium weapon.
func (s *Schema) IsAustraliumDefindex(defindex int) bool {
	return IsAustraliumDefindex(defindex)
}

// IsNativeFestive returns true if the defindex belongs to a "native" Festive item.
func (s *Schema) IsNativeFestive(defindex int) bool {
	return IsNativeFestive(defindex)
}

// GetQualities returns the quality name to ID map.
func (s *Schema) GetQualities() map[string]int {
	return s.qualByName
}

// GetWearByName returns the wear ID for a given string (e.g. "Factory New").
func (s *Schema) GetWearByName(name string) int {
	name = strings.TrimSpace(name)
	if !strings.HasPrefix(name, "(") {
		name = "(" + name + ")"
	}

	return wears[name]
}

// GetParticleEffects returns the effect name to ID map.
func (s *Schema) GetParticleEffects() map[string]int {
	return s.effByName
}

// GetPaintKitsByName returns the paintkit name to ID map.
func (s *Schema) GetPaintKitsByName() map[string]int {
	return s.paintKitByName
}

// GetPaintKits returns the paint kit name to ID map.
func (s *Schema) GetPaintKits() map[string]int {
	return s.paintKitByName
}

// CheckExistence verifies that the given item exists in the schema.
func (s *Schema) CheckExistence(item *sku.Item) bool {
	schemaItem := s.GetItemByDef(item.Defindex)
	if schemaItem == nil {
		return false
	}

	// Items with default quality
	if schemaItem.ItemQuality == 0 || schemaItem.ItemQuality == QualityVintage ||
		schemaItem.ItemQuality == QualityUnusual || schemaItem.ItemQuality == QualityStrange {
		if item.Quality != schemaItem.ItemQuality {
			return false
		}
	}

	allowedQualities := []int{schemaItem.ItemQuality}

	switch schemaItem.ItemQuality {
	case QualityUnusual:
		allowedQualities = append(allowedQualities, 11)
	case QualityUnique:
		allowedQualities = append(allowedQualities, 1, 3, 11)
	case QualityStrange:
		allowedQualities = append(allowedQualities, 5)
	}

	if !slices.Contains(allowedQualities, item.Quality) {
		return false
	}

	if item.Quality2 != 0 {
		isElevatedCapable := item.Quality == QualityUnusual ||
			item.Quality == QualityVintage ||
			item.Quality == QualityGenuine ||
			item.Quality == QualityHaunted ||
			item.Quality == QualityCollectors ||
			item.Quality == QualityDecorated

		if isElevatedCapable {
			return false
		}
	}

	// Exclusive genuine items
	if item.Quality != QualityGenuine {
		if _, ok := exclusiveGenuineReversed[item.Defindex]; ok {
			return false
		}
	} else {
		if _, ok := exclusiveGenuine[item.Defindex]; ok {
			return false
		}
	}

	// Retired keys
	if _, ok := retiredKeys[item.Defindex]; ok {
		switch item.Defindex {
		case 5713, 5716, 5717, 5762:
			if item.Craftable {
				return false
			}
		default:
			if !item.Craftable && item.Defindex != 5791 && item.Defindex != 5792 {
				return false
			}
		}
	}

	// Helper for crates
	hasExtraAttr := func() bool {
		return item.Quality != QualityUnique ||
			item.Killstreak != 0 ||
			item.Australium ||
			item.Effect != 0 ||
			item.Festivized ||
			item.Paintkit != 0 ||
			item.Wear != 0 ||
			item.Quality2 != 0 ||
			item.Craftnumber != 0 ||
			item.Target != 0 ||
			item.Output != 0 ||
			item.OutputQuality != 0 ||
			item.Paint != 0
	}

	if schemaItem.ItemClass == "supply_crate" && item.Crateseries == 0 {
		if item.Defindex != 5739 && item.Defindex != 5760 &&
			item.Defindex != 5737 && item.Defindex != 5738 {
			return false
		}

		if hasExtraAttr() {
			return false
		}
	}

	if item.Crateseries != 0 {
		if hasExtraAttr() {
			return false
		}

		if schemaItem.ItemClass != "supply_crate" {
			return false
		}

		validSingleSeries := map[int][]int{
			5022: {1, 3, 7, 12, 13, 18, 19, 23, 26, 31, 34, 39, 43, 47, 54, 57, 75},
			5041: {2, 4, 8, 11, 14, 17, 20, 24, 27, 32, 37, 42, 44, 49, 56, 71, 76},
			5045: {5, 9, 10, 15, 16, 21, 25, 28, 29, 33, 38, 41, 45, 55, 59, 77},
			5068: {30, 40, 50},
		}
		if list, ok := validSingleSeries[item.Defindex]; ok {
			if !slices.Contains(list, item.Crateseries) {
				return false
			}
		} else if munition, ok := munitionCrate[item.Crateseries]; ok {
			if item.Defindex != munition {
				return false
			}
		} else {
			if val, ok := s.crateSeriesList[item.Defindex]; !ok || val != item.Crateseries {
				return false
			}
		}
	}

	return true
}

// GetName builds the full item name from an item object.
func (s *Schema) GetName(item *sku.Item, proper, usePipeForSkin, scmFormat bool) string {
	schemaItem := s.GetItemByDef(item.Defindex)
	if schemaItem == nil {
		return ""
	}

	var parts []string

	if !scmFormat && !item.Tradable {
		parts = append(parts, "Non-Tradable")
	}

	if !scmFormat && !item.Craftable {
		parts = append(parts, "Non-Craftable")
	}

	if item.Quality2 != 0 {
		qName := s.GetQualityById(item.Quality2)
		if qName != "" {
			if !scmFormat && (item.Wear != 0 || item.Paintkit != 0) {
				qName += "(e)"
			}

			parts = append(parts, qName)
		}
	}

	addPrimaryQuality := false
	switch {
	case item.Quality == QualityUnique && item.Quality2 != Quality2None,
		item.Quality != QualityUnique && item.Quality != QualityDecorated && item.Quality != QualityUnusual,
		item.Quality == QualityUnusual && item.Effect == 0,
		item.Quality == QualityUnusual && scmFormat,
		schemaItem.ItemQuality == QualityUnusual:
		addPrimaryQuality = true
	}

	if addPrimaryQuality {
		qName := s.GetQualityById(item.Quality)
		if qName != "" {
			parts = append(parts, qName)
		}
	}

	if !scmFormat && item.Effect != 0 {
		effName := s.GetEffectById(item.Effect)
		if effName != "" {
			parts = append(parts, effName)
		}
	}

	if item.Festivized {
		parts = append(parts, "Festivized")
	}

	if item.Killstreak > 0 {
		switch item.Killstreak {
		case 1:
			parts = append(parts, "Killstreak")
		case 2:
			parts = append(parts, "Specialized Killstreak")
		case 3:
			parts = append(parts, "Professional Killstreak")
		}
	}

	if item.Target != 0 {
		targetItem := s.GetItemByDef(item.Target)
		if targetItem != nil {
			parts = append(parts, targetItem.ItemName)
		}
	}

	if item.OutputQuality != 0 && item.OutputQuality != 6 {
		oqName := s.GetQualityById(item.OutputQuality)
		if oqName != "" {
			parts = append([]string{oqName}, parts...)
		}
	}

	if item.Output != 0 {
		outItem := s.GetItemByDef(item.Output)
		if outItem != nil {
			parts = append(parts, outItem.ItemName)
		}
	}

	if item.Australium {
		parts = append(parts, "Australium")
	}

	if item.Paintkit != 0 {
		skinName := s.GetSkinById(item.Paintkit)
		if skinName != "" {
			if usePipeForSkin {
				parts = append(parts, skinName+" |")
			} else {
				parts = append(parts, skinName)
			}
		}
	}

	baseName := ""
	if info, ok := retiredKeys[item.Defindex]; ok {
		baseName = info.Name
	} else {
		baseName = schemaItem.ItemName
	}

	if proper && len(parts) == 0 && schemaItem.ProperName {
		baseName = "The " + baseName
	}

	parts = append(parts, baseName)

	if item.Wear != 0 {
		wears := []string{"Factory New", "Minimal Wear", "Field-Tested", "Well-Worn", "Battle Scarred"}
		if item.Wear >= 1 && item.Wear <= 5 {
			parts = append(parts, "("+wears[item.Wear-1]+")")
		}
	}

	for _, spell := range item.Spells {
		parts = append(parts, "(Spell: "+s.GetSpellName(spell)+")")
	}

	for _, partID := range item.Parts {
		// Find part name in schema
		partName := "Unknown Part"
		for _, p := range s.Raw.Schema.KillEaterScoreTypes {
			if p.Type == partID {
				partName = p.TypeName
				break
			}
		}

		parts = append(parts, "("+partName+": 0)")
	}

	if item.Crateseries != 0 {
		if scmFormat {
			hasSeriesAttr := false

			if schemaItem.Attributes != nil {
				for _, attr := range schemaItem.Attributes {
					if attr.Class == "supply_crate_series" {
						hasSeriesAttr = true
						break
					}
				}
			}

			if hasSeriesAttr {
				parts = append(parts, fmt.Sprintf("Series %%23%d", item.Crateseries))
			}
		} else {
			parts = append(parts, fmt.Sprintf("#%d", item.Crateseries))
		}
	} else if item.Craftnumber != 0 {
		parts = append(parts, fmt.Sprintf("#%d", item.Craftnumber))
	}

	if !scmFormat && item.Paint != 0 {
		paintName := s.GetPaintNameByDecimal(item.Paint)
		if paintName != "" {
			parts = append(parts, fmt.Sprintf("(Paint: %s)", paintName))
		}
	}

	if scmFormat && schemaItem.ItemName == "Chemistry Set" && item.Output == 6522 {
		if item.Target != 0 {
			if series, ok := strangifierChemistrySetSeries[item.Target]; ok {
				parts = append(parts, fmt.Sprintf("Series %%23%d", series))
			}
		}
	}

	if scmFormat && item.Wear != 0 && item.Effect != 0 && item.Quality == QualityDecorated {
		parts = append([]string{"Unusual"}, parts...)
	}

	return strings.Join(parts, " ")
}

// GetItemObjectFromName parses a display name into an item object.
func (s *Schema) GetItemObjectFromName(name string) *sku.Item {
	item := &sku.Item{
		Craftable: true,
		Tradable:  true,
	}
	originalName := name
	name = strings.ToLower(name)

	debugLog("GetItemObjectFromName start:", originalName)

	// Special cases: strange parts, filters, etc.
	if strings.Contains(name, "strange part:") ||
		strings.Contains(name, "strange cosmetic part:") ||
		strings.Contains(name, "strange filter:") ||
		strings.Contains(name, "strange count transfer tool") ||
		strings.Contains(name, "strange bacon grease") {
		schemaItem := s.GetItemByName(originalName)
		if schemaItem != nil {
			item.Defindex = schemaItem.Defindex
			if item.Quality == 0 {
				item.Quality = schemaItem.ItemQuality
			}
		}

		debugLog("return early (strange part)", item)

		return item
	}

	// Wear
	for w, val := range wears {
		if strings.Contains(name, w) {
			debugLog("wear before", name, item)
			name = strings.ReplaceAll(name, w, "")
			name = strings.TrimSpace(name)
			item.Wear = val
			debugLog("wear after", name, item)

			break
		}
	}

	// Strange(e)
	isExplicitElevatedStrange := false

	if strings.Contains(name, "strange(e)") {
		debugLog("strange(e) before", name, item)
		item.Quality2 = QualityStrange
		isExplicitElevatedStrange = true
		name = strings.ReplaceAll(name, "strange(e)", "")
		name = strings.TrimSpace(name)
		debugLog("strange(e) after", name, item)
	}

	hasStrangePrefix := false

	if strings.Contains(name, "strange") && !strings.Contains(name, "strangifier") {
		debugLog("strange before", name, item)

		hasStrangePrefix = true
		name = strings.ReplaceAll(name, "strange", "")
		name = strings.TrimSpace(name)
		debugLog("strange after", name, item)
	}

	// Uncraftable
	name = strings.ReplaceAll(name, "uncraftable", "non-craftable")
	if strings.Contains(name, "non-craftable") {
		debugLog("non-craftable before", name, item)
		name = strings.ReplaceAll(name, "non-craftable", "")
		name = strings.TrimSpace(name)
		item.Craftable = false
		debugLog("non-craftable after", name, item)
	}

	// Untradable
	name = strings.ReplaceAll(name, "untradeable", "non-tradable")
	name = strings.ReplaceAll(name, "untradable", "non-tradable")

	name = strings.ReplaceAll(name, "non-tradeable", "non-tradable")
	if strings.Contains(name, "non-tradable") {
		debugLog("non-tradable before", name, item)
		name = strings.ReplaceAll(name, "non-tradable", "")
		name = strings.TrimSpace(name)
		item.Tradable = false
		debugLog("non-tradable after", name, item)
	}

	// Unusualifier
	if strings.Contains(name, "unusualifier") {
		debugLog("unusualifier before", name, item)
		name = strings.ReplaceAll(name, "unusual ", "")
		name = strings.ReplaceAll(name, " unusualifier", "")
		name = strings.ReplaceAll(name, "unusualifier", "")
		name = strings.TrimSpace(name)
		item.Defindex = 9258
		item.Quality = QualityUnusual

		schemaItem := s.GetItemByName(name)
		if schemaItem != nil {
			item.Target = schemaItem.Defindex
		}

		debugLog("unusualifier after", name, item)

		return item
	}

	kitFabricatorDetected := strings.Contains(name, "kit fabricator")

	killstreaks := []struct {
		phrase string
		value  int
	}{
		{"professional killstreak", 3},
		{"specialized killstreak", 2},
		{"killstreak", 1},
	}
	for _, ks := range killstreaks {
		if strings.Contains(name, ks.phrase) {
			debugLog("killstreak before", name, item)
			name = strings.Replace(name, ks.phrase, "", 1)
			name = strings.TrimSpace(name)
			item.Killstreak = ks.value
			debugLog("killstreak after", name, item)

			break
		}
	}

	// Australium
	if strings.Contains(name, "australium") && !strings.Contains(name, "australium gold") {
		debugLog("australium before", name, item)
		name = strings.ReplaceAll(name, "australium", "")
		name = strings.TrimSpace(name)
		item.Australium = true
		debugLog("australium after", name, item)
	}

	// Festivized
	if strings.Contains(name, "festivized") && !strings.Contains(name, "festivized formation") {
		debugLog("festivized before", name, item)
		name = strings.ReplaceAll(name, "festivized", "")
		name = strings.TrimSpace(name)
		item.Festivized = true
		debugLog("festivized after", name, item)
	}

	// Quality detection
	exception := []string{
		"haunted ghosts", "haunted phantasm jr", "haunted phantasm",
		"haunted metal scrap", "haunted hat", "unusual cap",
		"vintage tyrolean", "vintage merryweather", "haunted kraken",
		"haunted forever!", "haunted cremation", "haunted wick",
	}

	qualitySearch := name
	for _, ex := range exception {
		if strings.Contains(name, ex) {
			qualitySearch = strings.ReplaceAll(name, ex, "")
			qualitySearch = strings.TrimSpace(qualitySearch)

			break
		}
	}

	if !slices.Contains(exception, qualitySearch) {
		for qName, qID := range s.qualByName {
			// Special case: "Decorated Weapon" is a quality but items are usually "SkinName (WeaponName)"
			if qID == QualityDecorated {
				continue
			}

			if qID == QualityCollectors && strings.Contains(qualitySearch, "collector's") &&
				strings.Contains(qualitySearch, "chemistry set") {
				continue
			}

			if qID == QualityCommunity && strings.HasPrefix(qualitySearch, "community sparkle") {
				continue
			}

			if strings.HasPrefix(qualitySearch, qName) {
				debugLog("quality before", name, item)

				if item.Quality != 0 && item.Quality != qID {
					if item.Quality2 == Quality2None {
						item.Quality2 = item.Quality
					}

					item.Quality = qID
				} else {
					item.Quality = qID
				}

				name = strings.Replace(name, qName, "", 1)
				name = strings.TrimSpace(name)

				debugLog("quality after", name, item)

				break
			}
		}
	}

	// Effect detection
	excludeAtomic := strings.Contains(name, "bonk! atomic punch") || strings.Contains(name, "atomic accolade")

	for effName, effID := range s.effByName {
		if effName == "" {
			continue
		}

		if strings.Contains(name, effName) {
			// Skip conditions
			if effName == "stardust" && strings.Contains(name, "starduster") {
				sub := strings.ReplaceAll(name, "stardust", "")
				if !strings.Contains(sub, "starduster") {
					continue
				}
			}

			if effName == "showstopper" && !strings.Contains(name, "taunt: ") &&
				!strings.Contains(name, "shred alert") {
				continue
			}

			if effName == "smoking" && (name == "smoking jacket" || strings.Contains(name, "smoking skid lid")) {
				if !strings.HasPrefix(name, "smoking smoking") {
					continue
				}
			}

			if effName == "haunted ghosts" && strings.Contains(name, "haunted ghosts") && item.Wear != 0 {
				continue
			}

			if effName == "pumpkin patch" && strings.Contains(name, "pumpkin patch") && item.Wear != 0 {
				continue
			}

			if effName == "stardust" && strings.Contains(name, "stardust") && item.Wear != 0 {
				continue
			}

			if effName == "atomic" && (strings.Contains(name, "subatomic") || excludeAtomic) {
				continue
			}

			if effName == "spellbound" && (strings.Contains(name, "taunt:") || strings.Contains(name, "shred alert")) {
				continue
			}

			if effName == "accursed" && strings.Contains(name, "accursed apparition") {
				continue
			}

			if effName == "haunted" && strings.Contains(name, "haunted kraken") {
				continue
			}

			if effName == "frostbite" && strings.Contains(name, "frostbite bonnet") {
				continue
			}

			if effName == "hot" {
				if item.Wear == 0 {
					continue
				}

				if !strings.Contains(name, "hot ") && (strings.Contains(name, "shotgun") ||
					strings.Contains(name, "shot ") || strings.Contains(name, "plaid potshotter")) {
					continue
				}

				if !strings.HasPrefix(name, "hot ") {
					continue
				}
			}

			if effName == "cool" && item.Wear == 0 {
				continue
			}

			debugLog("effect before", name, item)
			name = strings.ReplaceAll(name, effName, "")
			name = strings.TrimSpace(name)

			item.Effect = effID
			if effID == 4 {
				if item.Quality == 0 {
					item.Quality = QualityUnusual
				}
			} else if item.Quality != QualityUnusual {
				if item.Quality2 == Quality2None {
					item.Quality2 = item.Quality
				}

				item.Quality = QualityUnusual
			}

			debugLog("effect after", name, item)

			break
		}
	}

	// Paintkit detection
	if item.Wear != 0 {
		for pkName, pkID := range s.paintKitByName {
			if strings.Contains(name, pkName) {
				// Skip conditions
				if strings.Contains(name, "mk.ii") && !strings.Contains(pkName, "mk.ii") {
					continue
				}

				if strings.Contains(name, "(green)") && !strings.Contains(pkName, "(green)") {
					continue
				}

				if strings.Contains(name, "chilly") && !strings.Contains(pkName, "chilly") {
					continue
				}

				debugLog("paintkit before", name, item)
				name = strings.ReplaceAll(name, pkName, "")
				name = strings.ReplaceAll(name, " | ", "")
				name = strings.TrimSpace(name)
				item.Paintkit = pkID

				if item.Effect != 0 {
					if item.Quality == QualityUnusual && item.Quality2 == QualityStrange {
						if !isExplicitElevatedStrange {
							item.Quality = QualityStrange
							item.Quality2 = Quality2None
						} else {
							item.Quality = QualityDecorated
						}
					} else if item.Quality == QualityUnusual && item.Quality2 == Quality2None {
						item.Quality = QualityDecorated
					}
				}

				if item.Quality == 0 {
					item.Quality = QualityDecorated
				}

				debugLog("paintkit after", name, item)

				break
			}
		}

		// Weapon skin mapping
		if !strings.Contains(name, "war paint") {
			oldDefindex := item.Defindex
			switch {
			case strings.Contains(name, "pistol") && pistolSkins[item.Paintkit] != 0:
				item.Defindex = pistolSkins[item.Paintkit]
			case strings.Contains(name, "rocket launcher") && rocketLauncherSkins[item.Paintkit] != 0:
				item.Defindex = rocketLauncherSkins[item.Paintkit]
			case strings.Contains(name, "medi gun") && medicgunSkins[item.Paintkit] != 0:
				item.Defindex = medicgunSkins[item.Paintkit]
			case strings.Contains(name, "revolver") && revolverSkins[item.Paintkit] != 0:
				item.Defindex = revolverSkins[item.Paintkit]
			case strings.Contains(name, "stickybomb launcher") && stickybombSkins[item.Paintkit] != 0:
				item.Defindex = stickybombSkins[item.Paintkit]
			case strings.Contains(name, "sniper rifle") && sniperRifleSkins[item.Paintkit] != 0:
				item.Defindex = sniperRifleSkins[item.Paintkit]
			case strings.Contains(name, "flame thrower") && flameThrowerSkins[item.Paintkit] != 0:
				item.Defindex = flameThrowerSkins[item.Paintkit]
			case strings.Contains(name, "minigun") && minigunSkins[item.Paintkit] != 0:
				item.Defindex = minigunSkins[item.Paintkit]
			case strings.Contains(name, "scattergun") && scattergunSkins[item.Paintkit] != 0:
				item.Defindex = scattergunSkins[item.Paintkit]
			case strings.Contains(name, "shotgun") && shotgunSkins[item.Paintkit] != 0:
				item.Defindex = shotgunSkins[item.Paintkit]
			case strings.Contains(name, "smg") && smgSkins[item.Paintkit] != 0:
				item.Defindex = smgSkins[item.Paintkit]
			case strings.Contains(name, "grenade launcher") && grenadeLauncherSkins[item.Paintkit] != 0:
				item.Defindex = grenadeLauncherSkins[item.Paintkit]
			case strings.Contains(name, "wrench") && wrenchSkins[item.Paintkit] != 0:
				item.Defindex = wrenchSkins[item.Paintkit]
			case strings.Contains(name, "knife") && knifeSkins[item.Paintkit] != 0:
				item.Defindex = knifeSkins[item.Paintkit]
			}

			if oldDefindex != item.Defindex {
				debugLog("return after skin mapping", name, item)
				return item
			}
		}
	}

	// Painted
	if strings.Contains(name, "(paint: ") {
		debugLog("paint before loop", name, item)
		name = strings.ReplaceAll(name, "(paint: ", "")
		name = strings.ReplaceAll(name, ")", "")

		name = strings.TrimSpace(name)
		for pName, pVal := range s.paintByName {
			if strings.Contains(name, pName) {
				debugLog("paint in loop before", name, item)
				name = strings.ReplaceAll(name, pName, "")
				name = strings.TrimSpace(name)
				item.Paint = pVal
				debugLog("paint after", name, item)

				break
			}
		}
	}

	// Kit fabricator
	if kitFabricatorDetected && item.Killstreak > 1 {
		debugLog("kit fabricator before", name, item)
		name = strings.ReplaceAll(name, "kit fabricator", "")
		name = strings.TrimSpace(name)

		if item.Killstreak > 2 {
			item.Defindex = 20003
		} else {
			item.Defindex = 20002
		}

		if name != "" {
			schemaItem := s.GetItemByName(name)
			if schemaItem != nil {
				item.Target = schemaItem.Defindex
				if item.Quality == 0 {
					item.Quality = schemaItem.ItemQuality
				}
			} else {
				debugLog("return kit fabricator (no target)", name, item)
				return item
			}
		}

		if item.Quality == 0 {
			item.Quality = QualityUnique
		}

		if item.Killstreak > 2 {
			item.Output = 6526
		} else {
			item.Output = 6523
		}

		item.OutputQuality = QualityUnique
		item.Killstreak = 0
		debugLog("kit fabricator after", name, item)
	}

	// Collector's Chemistry Set
	if strings.Contains(name, "chemistry set") &&
		(!strings.Contains(name, "strangifier chemistry set") || strings.Contains(name, "collector's")) {
		debugLog("collector's chemistry set before", name, item)
		name = strings.ReplaceAll(name, "collector's ", "")
		name = strings.ReplaceAll(name, "chemistry set", "")

		name = strings.TrimSpace(name)
		if strings.Contains(name, "festive") && !strings.Contains(name, "a rather festive tree") {
			item.Defindex = 20007
		} else {
			item.Defindex = 20006
		}

		schemaItem := s.GetItemByName(name)
		if schemaItem != nil {
			item.Output = schemaItem.Defindex

			item.OutputQuality = QualityCollectors
			if item.Quality == 0 {
				item.Quality = schemaItem.ItemQuality
			}
		} else {
			debugLog("return collector's chemistry set (no target)", name, item)
			return item
		}

		debugLog("collector's chemistry set after", name, item)
	}

	// Strangifier Chemistry Set
	if strings.Contains(name, "strangifier chemistry set") {
		debugLog("strangifier chemistry set before", name, item)
		name = strings.ReplaceAll(name, "strangifier chemistry set", "")
		name = strings.TrimSpace(name)

		schemaItem := s.GetItemByName(name)
		if schemaItem != nil {
			item.Defindex = 20000
			item.Target = schemaItem.Defindex
			item.Quality = QualityUnique
			item.Output = 6522
			item.OutputQuality = QualityUnique
		} else {
			debugLog("return strangifier chemistry set (no target)", name, item)
			return item
		}

		debugLog("strangifier chemistry set after", name, item)
	}

	// Strangifier
	if strings.Contains(name, "strangifier") && !strings.Contains(name, "strangifier chemistry set") {
		debugLog("strangifier before", name, item)
		name = strings.ReplaceAll(name, "strangifier", "")
		name = strings.TrimSpace(name)
		item.Defindex = 6522

		schemaItem := s.GetItemByName(name)
		if schemaItem != nil {
			item.Target = schemaItem.Defindex
			if item.Quality == 0 {
				item.Quality = schemaItem.ItemQuality
			}
		} else {
			debugLog("return strangifier (no target)", name, item)
			return item
		}

		debugLog("strangifier after", name, item)
	}

	if !kitFabricatorDetected && strings.Contains(name, "kit") && item.Killstreak > 0 {
		debugLog("kit before", name, item)
		kitType := item.Killstreak
		item.Killstreak = 0

		name = strings.ReplaceAll(name, "kit", "")
		name = strings.TrimSpace(name)

		switch kitType {
		case 1:
			item.Defindex = 6527
		case 2:
			item.Defindex = 6523
		case 3:
			item.Defindex = 6526
		}

		if name != "" {
			schemaItem := s.GetItemByName(name)
			if schemaItem != nil {
				item.Target = schemaItem.Defindex
			} else {
				debugLog("return kit (no target)", name, item)
				return item
			}
		}

		if item.Quality == 0 {
			item.Quality = QualityUnique
		}

		debugLog("kit after", name, item)
	}

	if item.Defindex != 0 {
		debugLog("return after defindex set", name, item)
		return item
	}

	// War Paint
	if item.Paintkit != 0 && strings.Contains(name, "war paint") {
		debugLog("war paint before", name, item)

		searchName := fmt.Sprintf("Paintkit %d", item.Paintkit)
		if item.Quality == 0 {
			item.Quality = QualityDecorated
		}

		for _, it := range s.Raw.Schema.Items {
			if it.Name == searchName {
				item.Defindex = it.Defindex
				break
			}
		}

		debugLog("war paint after", name, item)

		return item
	}

	name = strings.ReplaceAll(name, " series ", " ")
	name = strings.ReplaceAll(name, " series#", " #")

	var number int

	if strings.Contains(name, "#") {
		debugLog("with # before", name, item)
		parts := strings.SplitN(name, "#", 2)
		name = strings.TrimSpace(parts[0])
		numberStr := strings.TrimSpace(parts[1])
		number = atoi(numberStr)

		debugLog("with # after", name, item)
	}

	if strings.Contains(name, "salvaged mann co. supply crate") {
		debugLog("salvaged crate", name, item)
		item.Crateseries = number
		item.Defindex = 5068
		item.Quality = QualityUnique
		debugLog("return salvaged crate", name, item)

		return item
	}

	if strings.Contains(name, "select reserve mann co. supply crate") {
		item.Defindex = 5660
		item.Crateseries = 60
		item.Quality = QualityUnique

		return item
	}

	if strings.Contains(name, "mann co. supply crate") {
		debugLog("mann co crate", name, item)

		crateseries := number
		switch crateseries {
		case 1, 3, 7, 12, 13, 18, 19, 23, 26, 31, 34, 39, 43, 47, 54, 57, 75:
			item.Defindex = 5022
		case 2, 4, 8, 11, 14, 17, 20, 24, 27, 32, 37, 42, 44, 49, 56, 71, 76:
			item.Defindex = 5041
		case 5, 9, 10, 15, 16, 21, 25, 28, 29, 33, 38, 41, 45, 55, 59, 77:
			item.Defindex = 5045
		}

		item.Crateseries = crateseries
		item.Quality = QualityUnique
		debugLog("return mann co crate", name, item)

		return item
	}

	if strings.Contains(name, "mann co. supply munition") {
		debugLog("munition crate", name, item)

		crateseries := number
		if def, ok := munitionCrate[crateseries]; ok {
			item.Defindex = def
		}

		item.Crateseries = crateseries
		item.Quality = QualityUnique
		debugLog("return munition crate", name, item)

		return item
	}

	// Retired keys
	for _, keyName := range retiredKeysNames {
		if strings.ToLower(name) == keyName {
			for _, info := range retiredKeys {
				if strings.ToLower(info.Name) == keyName {
					item.Defindex = info.Defindex
					if item.Quality == 0 {
						item.Quality = QualityUnique
					}

					debugLog("return retired key", name, item)

					return item
				}
			}
		}
	}

	schemaItem := s.GetItemByNameWithThe(name)
	if schemaItem == nil {
		debugLog("return no schema item", name, item)
		return item
	}

	item.Defindex = schemaItem.Defindex
	if item.Quality == 0 {
		item.Quality = schemaItem.ItemQuality
	}

	// Exclusive genuine fix
	if item.Quality == QualityGenuine {
		if newDef, ok := exclusiveGenuine[item.Defindex]; ok {
			item.Defindex = newDef
		}
	}

	if hasStrangePrefix {
		isElevatedCapable := item.Quality == QualityUnusual ||
			item.Quality == QualityVintage ||
			item.Quality == QualityGenuine ||
			item.Quality == QualityHaunted ||
			item.Quality == QualityCollectors ||
			item.Quality == QualityDecorated

		if isElevatedCapable {
			item.Quality2 = QualityStrange
		} else {
			item.Quality = QualityStrange
		}
	}

	if schemaItem.ItemClass == "supply_crate" {
		debugLog("supply_crate before", name, item)

		if series, ok := s.crateSeriesList[item.Defindex]; ok {
			item.Crateseries = series
		} else if number != 0 {
			item.Crateseries = number
		}

		debugLog("supply_crate after", name, item)
	} else if number != 0 {
		debugLog("craftnumber before", name, item)
		item.Craftnumber = number
		debugLog("craftnumber after", name, item)
	}

	debugLog("final return", name, item)

	return item
}

// GetSkuFromName returns the SKU string for the given item name.
// NOTE: This method relies on string parsing and is subject to naming variations.
// Use GetSKUFromObject for direct data-driven identification whenever possible.
func (s *Schema) GetSkuFromName(name string) string {
	item := s.GetItemObjectFromName(name)
	return sku.FromObject(item)
}

// GetSKUFromObject normalizes the given SKU item and returns its string representation.
// This is the preferred method for generating SKUs from structured data.
func (s *Schema) GetSKUFromObject(item *sku.Item) string {
	if item == nil {
		return ""
	}

	s.NormalizeItem(item)

	return sku.FromObject(item)
}

// GetSKUFromEconItem converts a generic Steam WebAPI item into a strict TF2 SKU string.
// NOTE: This method relies on MarketHashName parsing and is maintained for
// legacy WebAPI compatibility. For internal bot logic, use tf2.Item.GetSKU(s).
func (s *Schema) GetSKUFromEconItem(item *trading.Item) string {
	nameToParse := item.MarketHashName
	if nameToParse == "" {
		nameToParse = item.MarketName
	}

	skuItem := s.GetItemObjectFromName(nameToParse)
	if skuItem == nil {
		return "unknown"
	}

	// Tags detection (Reliable for Wear/Quality)
	for _, tag := range item.Tags {
		if tag.Category == "Exterior" {
			if wearID := s.GetWearByName(tag.LocalizedName); wearID != 0 {
				skuItem.Wear = wearID
			}
		}
	}

	// Skin (Paintkit) detection by name
	if skuItem.Quality == QualityDecorated {
		lowerName := strings.ToLower(item.MarketHashName)
		for pkName, pkID := range s.paintKitByName {
			if strings.Contains(lowerName, pkName) {
				skuItem.Paintkit = pkID
				break
			}
		}
	}

	skuItem.Tradable = item.Tradable

	// Killstreak, Paint, Crate Series, Spells, Strange Parts, Wear, Paintkit
	for _, desc := range item.Descriptions {
		val := strings.TrimSpace(desc.Value)
		if val == "" {
			continue
		}

		// Exterior (Wear): "Exterior: Factory New"
		if wearName, ok := strings.CutPrefix(val, "Exterior: "); ok {
			if wearID := s.GetWearByName(wearName); wearID != 0 {
				skuItem.Wear = wearID
			}

			continue
		}

		if strings.Contains(val, "( Not Usable in Crafting )") {
			skuItem.Craftable = false
			break
		}
	}

	// Attribute detection from descriptions
	for _, d := range item.Descriptions {
		val := d.Value

		// Unusual Effect
		isUnusual := skuItem.Quality == QualityUnusual || skuItem.Quality2 == QualityUnusual ||
			skuItem.Quality == QualityDecorated
		if isUnusual && skuItem.Effect == 0 {
			if after, ok := strings.CutPrefix(val, "★ Unusual Effect: "); ok {
				if id := s.GetEffectIdByName(after); id != 0 {
					skuItem.Effect = id
				}
			}
		}

		// Killstreak Tier
		if strings.Contains(val, "Killstreak Active") {
			switch {
			case strings.Contains(val, "Professional"):
				skuItem.Killstreak = 3
			case strings.Contains(val, "Specialized"):
				skuItem.Killstreak = 2
			case strings.Contains(val, "Killstreak"):
				skuItem.Killstreak = 1
			}
		}

		// Paint
		if paintName, ok := strings.CutPrefix(val, "Paint Color: "); ok {
			if paintID := s.GetPaintDecimalByName(paintName); paintID != 0 {
				skuItem.Paint = paintID
			}
		}

		// Crate Series
		if strings.Contains(val, "Crate Series #") {
			parts := strings.Split(val, "#")
			if len(parts) == 2 {
				if series, err := strconv.Atoi(parts[1]); err == nil {
					skuItem.Crateseries = series
				}
			}
		}

		// Festive/Festivized
		if strings.Contains(val, "Festivized") {
			skuItem.Festivized = true
		}

		// Strange Parts (Color: 756b5e)
		if d.Color == "756b5e" {
			clean := strings.Trim(val, "()")
			if before, _, ok := strings.Cut(clean, ":"); ok {
				partName := strings.TrimSpace(before)
				for name, suffix := range s.GetStrangeParts() {
					if strings.Contains(partName, name) {
						if partID, err := strconv.Atoi(strings.TrimPrefix(suffix, "sp")); err == nil {
							skuItem.Parts = append(skuItem.Parts, partID)
						}

						break
					}
				}
			}
		}

		// Spells (Color: 7ea9d1)
		if strings.ToLower(d.Color) == "7ea9d1" {
			spellName := strings.TrimSpace(val)
			if spell, ok := s.GetSpellIdByName(spellName); ok {
				skuItem.Spells = append(skuItem.Spells, spell)
			}
		}
	}

	// Extra detections from name if not already set
	if !skuItem.Festivized && (strings.Contains(nameToParse, "Festivized") || s.IsNativeFestive(skuItem.Defindex)) {
		skuItem.Festivized = true
	}

	if !skuItem.Australium && strings.Contains(nameToParse, "Australium") {
		skuItem.Australium = true
	}

	// Strange Unusual / Strange Decorated detection
	if skuItem.Quality != 11 && strings.HasPrefix(item.MarketHashName, "Strange ") {
		skuItem.Quality2 = 11
	}

	s.NormalizeItem(skuItem)

	return sku.FromObject(skuItem)
}

// IsPromoItem checks if the item is a promo version.
func (s *Schema) IsPromoItem(it *Item) bool {
	return strings.HasPrefix(it.Name, "Promo ") && it.CraftClass == ""
}

// NormalizeItem "fixes" an item, bringing its Defindex and quality combinations
// into line with a single trade standard. The method modifies the passed [sku.Item] object using its pointer.
func (s *Schema) NormalizeItem(item *sku.Item) {
	// 1. Defindex Normalization (using centralized map)
	// We do this BEFORE schema lookup because we want to normalize even if the old ID isn't in schema
	item.Defindex = NormalizeDefindex(item.Defindex)

	schemaItem := s.GetItemByDef(item.Defindex)
	if schemaItem == nil {
		return
	}

	// Fix for "Upgradeable" weapons (Stock weapons that can be renamed)
	// We do this AFTER map normalization in case the canonical ID is also upgradeable
	if strings.Contains(schemaItem.Name, strings.ToUpper(schemaItem.ItemClass)) {
		for _, it := range s.Raw.Schema.Items {
			if it.ItemClass == schemaItem.ItemClass && strings.HasPrefix(it.Name, "Upgradeable ") {
				item.Defindex = it.Defindex
				break
			}
		}
	}

	// 2. Promo/Genuine Logic
	isPromo := s.IsPromoItem(schemaItem)
	if isPromo && item.Quality != QualityGenuine {
		for _, it := range s.Raw.Schema.Items {
			if !s.IsPromoItem(it) && it.ItemName == schemaItem.ItemName {
				item.Defindex = it.Defindex
				break
			}
		}
	} else if !isPromo && item.Quality == QualityGenuine {
		for _, it := range s.Raw.Schema.Items {
			if s.IsPromoItem(it) && it.ItemName == schemaItem.ItemName {
				item.Defindex = it.Defindex
				break
			}
		}
	}

	// 3. Crate Series assignment from schema if missing
	if item.Crateseries == 0 && schemaItem.ItemClass == "supply_crate" {
		if series, ok := s.crateSeriesList[item.Defindex]; ok {
			item.Crateseries = series
		}
	}

	// 4. Quality combinations (Strange Unusual / Decorated)
	if item.Effect != 0 {
		if item.Paintkit != 0 || item.Quality == QualityDecorated {
			if item.Quality == QualityStrange || item.Quality2 == QualityStrange {
				item.Quality2 = QualityStrange
			}

			item.Quality = QualityDecorated
		} else if item.Quality == QualityStrange || item.Quality2 == QualityStrange {
			item.Quality = QualityUnusual
			item.Quality2 = QualityStrange
		}
	}
}

// ToJSON returns a representation for serialization.
func (s *Schema) ToJSON() map[string]any {
	return map[string]any{
		"version": s.Version,
		"time":    s.Time.Unix(),
		"raw":     s.Raw,
	}
}

func atoi(s string) int {
	s = strings.TrimSpace(s)
	val, _ := strconv.Atoi(s)

	return val
}
