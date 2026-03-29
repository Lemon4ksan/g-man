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
)

var debugLog = func(v ...any) {
	if os.Getenv("DEBUG_SCHEMA") == "true" {
		log.Println(v...)
	}
}

type RawSchema struct {
	Schema struct {
		Items                                []*ItemSchema         `json:"items"`
		Attributes                           []*AttributeSchema    `json:"attributes"`
		Qualities                            map[string]int        `json:"qualities"`
		QualityNames                         map[string]string     `json:"qualityNames"` // Note: Some API responses omit this
		OriginNames                          []*OriginName         `json:"originNames"`
		ItemSets                             []*ItemSet            `json:"item_sets"`
		AttributeControlledAttachedParticles []*ParticleEffect     `json:"attribute_controlled_attached_particles"`
		ItemLevels                           []*ItemLevel          `json:"item_levels"`
		KillEaterScoreTypes                  []*KillEaterScoreType `json:"kill_eater_score_types"`
		StringLookups                        []*StringLookup       `json:"string_lookups"`

		PaintKits map[string]string `json:"paintkits"` // Injected from protodefs
	} `json:"schema"`

	ItemsGame map[string]any `json:"items_game"` // Parsed items_game.txt (should be nilled after init)
}

// ItemSchema represents a single item definition.
type ItemSchema struct {
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
func (a *ItemAttribute) UnmarshalJSON(data[]byte) error {
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
	Items[]string        `json:"items"`
	Attributes[]ItemAttribute `json:"attributes"`
}

// OriginName maps an origin ID to its display name (e.g., 0 = Timed Drop, 4 = Crafted).
type OriginName struct {
	Origin int    `json:"origin"`
	Name   string `json:"name"`
}

// ItemLevel represents strange rank thresholds (e.g., Hale's Own).
type ItemLevel struct {
	Name   string `json:"name"`
	Levels[]struct {
		Level         int    `json:"level"`
		RequiredScore int    `json:"required_score"`
		Name          string `json:"name"`
	} `json:"levels"`
}

// StringLookup contains lookup tables for string-based attributes (like Spells!).
type StringLookup struct {
	TableName string `json:"table_name"`
	Strings[]struct {
		Index  int    `json:"index"`
		String string `json:"string"`
	} `json:"strings"`
}

// Schema is the main type.
type Schema struct {
	Version string
	Raw     *RawSchema
	Time    time.Time

	// Primary indices - O(1) lookups
	itemsByDef  map[int]*ItemSchema
	itemsByName map[string]*ItemSchema

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
}

// NewSchema creates a Schema from the given raw data and builds all indices.
func NewSchema(raw *RawSchema) *Schema {
	s := &Schema{
		Raw:            raw,
		itemsByDef:     make(map[int]*ItemSchema),
		itemsByName:    make(map[string]*ItemSchema),
		attrsByDef:     make(map[int]*AttributeSchema),
		qualByID:       make(map[int]string),
		qualByName:     make(map[string]int),
		effByID:        make(map[int]string),
		effByName:      make(map[string]int),
		paintKitByID:   make(map[int]string),
		paintKitByName: make(map[string]int),
		paintByDecimal: make(map[int]string),
		paintByName:    make(map[string]int),
	}
	s.buildIndices()
	return s
}

// buildIndices creates all O(1) lookup maps from the raw data.
func (s *Schema) buildIndices() {
	// Item indices
	for _, item := range s.Raw.Schema.Items {
		s.itemsByDef[item.Defindex] = item
		s.itemsByName[strings.ToLower(item.ItemName)] = item
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
				// Original JS: delete obj['Orbiting Fire']; obj['Orbiting Fire'] = 33;
				s.effByName["orbiting fire"] = 33
				s.effByID[33] = "Orbiting Fire"
			case "Nether Trail":
				// Original JS: delete obj['Ether Trail']; obj['Ether Trail'] = 103;
				s.effByName["ether trail"] = 103
				s.effByID[103] = "Ether Trail"
			case "Refragmenting Reality":
				// Original JS: delete obj['Fragmenting Reality']; obj['Fragmenting Reality'] = 141;
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
				var decimal int
				decimal = int(it.Attributes[0].Value)
				s.paintByDecimal[decimal] = it.ItemName
				s.paintByName[strings.ToLower(it.ItemName)] = decimal
			}
		}
	}

	s.paintByDecimal[5801378] = "Legacy Paint"
	s.paintByName["legacy paint"] = 5801378

	s.crateSeriesList = s.buildCrateSeriesList()
	s.Raw.ItemsGame = nil
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

func (s *Schema) GetItemByDef(def int) *ItemSchema {
	return s.itemsByDef[def]
}

func (s *Schema) GetItemByName(name string) *ItemSchema {
	return s.itemsByName[strings.ToLower(name)]
}

func (s *Schema) GetAttributeByDef(def int) *AttributeSchema {
	return s.attrsByDef[def]
}

func (s *Schema) GetQualityById(id int) string {
	return s.qualByID[id]
}

func (s *Schema) GetQualityIdByName(name string) int {
	return s.qualByName[strings.ToLower(name)]
}

func (s *Schema) GetEffectById(id int) string {
	return s.effByID[id]
}

func (s *Schema) GetEffectIdByName(name string) int {
	return s.effByName[strings.ToLower(name)]
}

func (s *Schema) GetSkinById(id int) string {
	return s.paintKitByID[id]
}

func (s *Schema) GetSkinIdByName(name string) int {
	return s.paintKitByName[strings.ToLower(name)]
}

func (s *Schema) GetPaintNameByDecimal(decimal int) string {
	return s.paintByDecimal[decimal]
}

func (s *Schema) GetPaintDecimalByName(name string) int {
	return s.paintByName[strings.ToLower(name)]
}

// GetItemByNameWithThe tries to find an item after stripping "The " from the name.
func (s *Schema) GetItemByNameWithThe(name string) *ItemSchema {
	name = strings.ToLower(name)
	name = strings.TrimPrefix(name, "the ")
	name = strings.TrimSpace(name)

	for _, it := range s.Raw.Schema.Items {
		itemName := strings.ToLower(it.ItemName)
		itemName = strings.TrimPrefix(itemName, "the ")
		itemName = strings.TrimSpace(itemName)

		if name == itemName {
			if it.ItemName == "Name Tag" && it.Defindex == 2093 {
				continue
			}
			if it.ItemQuality == 0 {
				continue
			}
			return it
		}
	}
	return nil
}

// GetItemBySKU returns the item for a given SKU string.
func (s *Schema) GetItemBySKU(itemSku string) *ItemSchema {
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
	var out []struct {
		Name string
		ID   int
	}
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

var weaponsToExclude = map[int]bool{
	266: true, 452: true, 466: true, 474: true,
	572: true, 574: true, 587: true, 638: true,
	735: true, 736: true, 737: true, 851: true,
	880: true, 933: true, 939: true, 947: true,
	1013: true, 1152: true, 30474: true,
}

// GetCraftableWeaponsSchema returns all craftable weapon items.
func (s *Schema) GetCraftableWeaponsSchema() []*ItemSchema {
	var out []*ItemSchema
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
	var out []string
	for _, it := range s.GetCraftableWeaponsSchema() {
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

// GetQualities returns the quality name to ID map.
func (s *Schema) GetQualities() map[string]int {
	return s.qualByName
}

// GetParticleEffects returns the effect name to ID map.
func (s *Schema) GetParticleEffects() map[string]int {
	return s.effByName
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
		if item.Quality == QualityUnusual && item.Quality2 != Quality2Strange {
			return false
		}
		if item.Quality2 == item.Quality {
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
	if item.Quality == QualityUnique && item.Quality2 != Quality2None {
		addPrimaryQuality = true
	} else if item.Quality != QualityUnique && item.Quality != QualityDecorated && item.Quality != QualityUnusual {
		addPrimaryQuality = true
	} else if item.Quality == QualityUnusual && item.Effect == 0 {
		addPrimaryQuality = true
	} else if item.Quality == QualityUnusual && scmFormat {
		addPrimaryQuality = true
	} else if schemaItem.ItemQuality == QualityUnusual {
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
	wears := map[string]int{
		"(factory new)":    1,
		"(minimal wear)":   2,
		"(field-tested)":   3,
		"(well-worn)":      4,
		"(battle scarred)": 5,
	}
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
	if strings.Contains(name, "strange") && !strings.Contains(name, "strangifier") {
		debugLog("strange before", name, item)
		item.Quality = QualityStrange
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

	if !kitFabricatorDetected {
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

	if !containsAny(qualitySearch, exception) {
		for qName, qID := range s.qualByName {
			if qName == "collector's" && strings.Contains(qualitySearch, "collector's") &&
				strings.Contains(qualitySearch, "chemistry set") {
				continue
			}
			if qName == "community" && strings.HasPrefix(qualitySearch, "community sparkle") {
				continue
			}
			if strings.HasPrefix(qualitySearch, qName) {
				debugLog("quality before", name, item)
				name = strings.ReplaceAll(name, qName, "")
				name = strings.TrimSpace(name)
				if item.Quality2 == Quality2None {
					item.Quality2 = item.Quality
				}
				item.Quality = qID
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
			if effName == "showstopper" && !strings.Contains(name, "taunt: ") && !strings.Contains(name, "shred alert") {
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
	if (strings.Contains(name, "chemistry set") && !strings.Contains(name, "strangifier chemistry set")) ||
		strings.Contains(name, "collector's") {
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

// GetSkuFromName returns the SKU string for the given name.
func (s *Schema) GetSkuFromName(name string) string {
	item := s.GetItemObjectFromName(name)
	skuStr, err := sku.FromObject(item)
	if err != nil {
		return ""
	}
	return skuStr
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

func containsAny(s string, list []string) bool {
	for _, substr := range list {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}
