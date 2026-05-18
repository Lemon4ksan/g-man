// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package backpack

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/lemon4ksan/g-man/pkg/steam/community/inventory"
	"github.com/lemon4ksan/g-man/pkg/tf2/schema"
	"github.com/lemon4ksan/g-man/pkg/tf2/sku"
	"github.com/lemon4ksan/g-man/pkg/trading"
)

var (
	// ErrItemNotFound is returned when an item is not found in the inventory.
	ErrItemNotFound = errors.New("backpack: could not find item in inventory")
	// ErrSteamAPI is returned when the Steam API returns an error status.
	ErrSteamAPI = errors.New("backpack: steam api returned error status")
)

// HistoryStatus represents the result of checking the item's history.
type HistoryStatus struct {
	// Recorded reports whether the service knows about this item.
	Recorded bool
	// IsDuped reports whether the item is considered a duplicate.
	IsDuped bool
}

// DupeChecker defines an interface for any service that can
// check the history of an item (e.g., backpack.tf, reps.tf).
type DupeChecker interface {
	CheckHistory(ctx context.Context, assetID uint64) (HistoryStatus, error)
}

// PlayerItemsResponse represents the response from the Steam Community Inventory API.
type PlayerItemsResponse struct {
	Result struct {
		Status           int       `json:"status"`
		StatusDetail     string    `json:"statusDetail"`
		NumBackpackSlots int       `json:"num_backpack_slots"`
		Items            []TF2Item `json:"items"`
	} `json:"result"`
}

// TF2Item represents an item in the Team Fortress 2 inventory.
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

func mapCEconToTF2(econ inventory.CEconItem, s *schema.Schema) TF2Item {
	asset := econ.Asset
	desc := econ.Description

	item := TF2Item{
		ID:         mustParseUint64(asset.AssetID),
		Attributes: []TF2Attribute{},
		Quantity:   1,
	}

	if amount, err := strconv.Atoi(asset.Amount); err == nil {
		item.Quantity = amount
	}

	if desc == nil {
		return item
	}

	if desc.AppData != nil {
		if di, ok := desc.AppData["def_index"].(string); ok {
			item.Defindex = mustParseInt(di)
		}

		if q, ok := desc.AppData["quality"].(string); ok {
			item.Quality = mustParseInt(q)
		}
	}

	if item.Defindex == 0 || item.Quality == 0 {
		for _, tag := range desc.Tags {
			switch tag.Category {
			case "Quality":
				if item.Quality == 0 {
					item.Quality = s.QualityIDByName(tag.LocalizedTagName)
				}
			case "Type":
			}
		}
	}

	item.FlagCannotTrade = desc.Tradable == 0
	item.FlagCannotCraft = false

	for _, d := range desc.Descriptions {
		val := d.Value

		if strings.Contains(val, "( Not Usable in Crafting )") {
			item.FlagCannotCraft = true
			continue
		}

		// Exterior (Wear): "Exterior: Factory New"
		if wearName, ok := strings.CutPrefix(val, "Exterior: "); ok {
			if wearID := s.WearByName(wearName); wearID != 0 {
				item.Attributes = append(item.Attributes, TF2Attribute{
					Defindex: schema.AttrWear,
					Value:    float64(wearID),
				})
			}

			continue
		}

		if effectName, ok := strings.CutPrefix(val, "★ Unusual Effect: "); ok {
			if effectID := s.EffectIDByName(effectName); effectID != 0 {
				item.Attributes = append(item.Attributes, TF2Attribute{
					Defindex: schema.AttrUnusualEffect,
					Value:    float64(effectID),
				})
			}

			continue
		}

		if strings.Contains(val, "Killstreak Active") {
			ksLevel := 0
			switch {
			case strings.Contains(val, "Professional"):
				ksLevel = 3
			case strings.Contains(val, "Specialized"):
				ksLevel = 2
			case strings.Contains(val, "Killstreak"):
				ksLevel = 1
			}

			if ksLevel > 0 {
				item.Attributes = append(item.Attributes, TF2Attribute{
					Defindex: schema.AttrKillstreak,
					Value:    float64(ksLevel),
				})
			}
		}

		if paintName, ok := strings.CutPrefix(val, "Paint Color: "); ok {
			if paintID := s.PaintDecimalByName(paintName); paintID != 0 {
				item.Attributes = append(item.Attributes, TF2Attribute{
					Defindex: schema.AttrPaintColor,
					Value:    float64(paintID),
				})
			}
		}

		// Crate Series: "Crate Series #82"
		if strings.Contains(val, "Crate Series #") {
			parts := strings.Split(val, "#")
			if len(parts) == 2 {
				if series, err := strconv.Atoi(parts[1]); err == nil {
					item.Attributes = append(item.Attributes, TF2Attribute{
						Defindex: schema.AttrCrateSeries,
						Value:    float64(series),
					})
				}
			}
		}

		// Strange detection for non-strange qualities (e.g. Strange Unusual)
		if item.Quality != schema.QualityStrange &&
			(strings.Contains(val, "Strange Stat") || strings.Contains(val, "Strange Part")) {
			item.Attributes = append(item.Attributes, TF2Attribute{
				Defindex: schema.AttrStrangeScore,
				Value:    float64(1),
			})
		}

		// Strange Parts (Color: 756b5e)
		if d.Color == "756b5e" {
			// Extract part name from string like "Kills: 123" or "(Airborne Enemies Killed: 0)"
			clean := strings.Trim(val, "()")
			if before, _, ok := strings.Cut(clean, ":"); ok {
				partName := strings.TrimSpace(before)
				// Match against schema parts
				for name, suffix := range s.StrangeParts() {
					if strings.Contains(partName, name) {
						if partID, err := strconv.Atoi(strings.TrimPrefix(suffix, "sp")); err == nil {
							item.Attributes = append(item.Attributes, TF2Attribute{
								Defindex: schema.DefPartsProxy + len(item.Attributes),
								Value:    float64(partID),
							})
						}

						break
					}
				}
			}
		}

		// Spells (Color: 7ea9d1)
		if d.Color == "7ea9d1" {
			spellName := strings.TrimSpace(val)
			if spell, ok := s.SpellIDByName(spellName); ok {
				item.Attributes = append(item.Attributes, TF2Attribute{
					Defindex: schema.DefSpellProxy + len(item.Attributes),
					Value:    spell,
				})
			}
		}
	}

	// Paintkit detection for Decorated Weapons
	if item.Quality == 15 {
		name := strings.ToLower(desc.MarketHashName)
		for pkName, pkID := range s.PaintKitsByName() {
			if strings.Contains(name, pkName) {
				item.Attributes = append(item.Attributes, TF2Attribute{
					Defindex: schema.AttrPaintkit,
					Value:    float64(pkID),
				})

				break
			}
		}
	}

	// Australium detection (Attribute 2027)
	// 1. Try to find it in AppData attributes if available
	hasAustraliumAttr := false
	if attrData, ok := desc.AppData["attributes"].(map[string]any); ok {
		if _, exists := attrData["2027"]; exists {
			hasAustraliumAttr = true
		}
	}

	// 2. Fallback to name check (only on MarketHashName to avoid custom name baiting)
	// Australiums are always Strange and must be in the whitelist
	if !hasAustraliumAttr && item.Quality == schema.QualityStrange &&
		s.IsAustraliumDefindex(item.Defindex) &&
		strings.Contains(desc.MarketHashName, "Australium") {
		hasAustraliumAttr = true
	}

	if hasAustraliumAttr {
		item.Attributes = append(item.Attributes, TF2Attribute{
			Defindex: schema.AttrAustralium,
			Value:    float64(1),
		})
	}

	// Festive logic: Native Festive items OR Festivized attribute
	isFestive := strings.Contains(desc.Name, "Festivized") || s.IsNativeFestive(item.Defindex)
	if isFestive {
		item.Attributes = append(item.Attributes, TF2Attribute{
			Defindex: schema.AttrFestivized,
			Value:    float64(1),
		})
	}

	item.Defindex = s.NormalizeDefindex(item.Defindex)

	return item
}

// ToSKU generates an SKU string for an item using inventory data.
// This allows you to compare items in someone else's inventory with our price list.
func (it *TF2Item) ToSKU() string {
	quality := it.Quality
	defindex := it.Defindex
	isCraftable := !it.FlagCannotCraft

	effect := 0
	wear := 0
	isAustralium := false
	paintkit := 0
	killstreak := 0
	isFestivized := false
	paint := 0
	quality2 := 0
	crateseries := 0

	var (
		spells []sku.Spell
		parts  []int
	)

	for _, attr := range it.Attributes {
		switch attr.Defindex {
		case schema.AttrUnusualEffect:
			if val, ok := attr.Value.(float64); ok {
				effect = int(val)
			}
		case schema.AttrWear:
			if val, ok := attr.Value.(float64); ok {
				wear = int(val * 5)
			}
		case schema.AttrAustralium:
			isAustralium = true
		case schema.AttrPaintkit:
			if val, ok := attr.Value.(float64); ok {
				paintkit = int(val)
			}
		case schema.AttrKillstreak:
			if val, ok := attr.Value.(float64); ok {
				killstreak = int(val)
			}
		case schema.AttrFestivized:
			isFestivized = true
		case schema.AttrPaintColor, schema.AttrPaintColor2:
			if val, ok := attr.Value.(float64); ok {
				paint = int(val)
			}
		case schema.AttrCrateSeries:
			if val, ok := attr.Value.(float64); ok {
				crateseries = int(val)
			}
		case schema.AttrStrangeScore:
			quality2 = schema.QualityStrange
		}

		if attr.Defindex >= schema.DefSpellProxy && attr.Defindex < schema.DefSpellProxy+100 {
			if spell, ok := attr.Value.(sku.Spell); ok {
				spells = append(spells, spell)
			}
		}

		if attr.Defindex >= schema.DefPartsProxy && attr.Defindex < schema.DefPartsProxy+100 {
			if val, ok := attr.Value.(float64); ok {
				parts = append(parts, int(val))
			}
		}
	}

	return sku.FromObject(&sku.Item{
		Defindex:    defindex,
		Quality:     quality,
		Craftable:   isCraftable,
		Tradable:    !it.FlagCannotTrade,
		Australium:  isAustralium,
		Effect:      effect,
		Wear:        wear,
		Paintkit:    paintkit,
		Killstreak:  killstreak,
		Festivized:  isFestivized,
		Paint:       paint,
		Quality2:    quality2,
		Crateseries: crateseries,
		Spells:      spells,
		Parts:       parts,
	})
}

// ToEconItem converts the internal TF2Item structure into a universal exchange format (Econ Item).
func (it *TF2Item) ToEconItem() *trading.Item {
	item := &trading.Item{
		AppID:     440,
		ContextID: 2,
		AssetID:   it.ID,
		ClassID:   uint64(it.Defindex),
		Amount:    int64(it.Quantity),
		Tradable:  !it.FlagCannotTrade,
		Name:      it.CustomName,
	}

	if len(it.Attributes) > 0 {
		item.Attributes = make([]trading.Attribute, 0, len(it.Attributes))
		for _, attr := range it.Attributes {
			valStr := ""

			var floatVal float64

			switch v := attr.Value.(type) {
			case float64:
				floatVal = v
				valStr = fmt.Sprintf("%g", v)
			case string:
				valStr = v
				floatVal, _ = strconv.ParseFloat(v, 64)
			}

			item.Attributes = append(item.Attributes, trading.Attribute{
				Defindex:   attr.Defindex,
				Value:      valStr,
				FloatValue: floatVal,
			})
		}
	}

	if it.FlagCannotCraft {
		item.Descriptions = append(item.Descriptions, trading.Description{
			Value: "( Not Usable in Crafting )",
			Color: "ff4040",
		})
	}

	if it.CustomDesc != "" {
		item.Descriptions = append(item.Descriptions, trading.Description{
			Value: it.CustomDesc,
		})
	}

	return item
}

// TF2Attribute represents an attribute of an item.
type TF2Attribute struct {
	Defindex   int     `json:"defindex"`
	Value      any     `json:"value"` // int or string
	FloatValue float64 `json:"float_value,omitempty"`
}

func mustParseUint64(s string) uint64 {
	v, _ := strconv.ParseUint(s, 10, 64)
	return v
}

func mustParseInt(s string) int {
	v, _ := strconv.Atoi(s)
	return v
}
