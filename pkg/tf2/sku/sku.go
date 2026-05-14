// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package sku implements the TF2 Stock Keeping Unit format.
// It allows converting complex item attributes into a compact string representation.
//
// NOTE: Spells are TBD.
package sku

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var rxPriceKey = regexp.MustCompile(
	`^(\d+);([0-9]|[1][0-5])(;((uncraftable)|(untrad(e)?able)|(australium)|(festive)|(strange)|((u|pk|td-|c|od-|oq-|p)\d+)|(w[1-5])|(kt-[1-3])|(n((100)|[1-9]\d?))))*?$|^\d+$`,
)

// IsValid tests if a string matches the standard TF2 SKU format.
func IsValid(sku string) bool {
	return rxPriceKey.MatchString(sku)
}

// Item represents a TF2 item with all possible SKU attributes.
type Item struct {
	Defindex      int
	Quality       int
	Craftable     bool
	Tradable      bool
	Killstreak    int
	Australium    bool
	Effect        int
	Festivized    bool
	Paintkit      int
	Wear          int
	Quality2      int // 11 for strange
	Craftnumber   int
	Crateseries   int
	Target        int
	Output        int
	OutputQuality int
	Paint         int
	Spells        []Spell
	Parts         []int
}

// Spell represents a Halloween spell attached to an item.
type Spell struct {
	Attribute int
	Value     int
}

// FromString parses a SKU string into an Item.
// The expected format is "defindex;quality[;attribute]*".
// Attributes may include dashes (e.g., "kt-2") which are ignored during parsing.
func FromString(sku string) (*Item, error) {
	parts := strings.Split(sku, ";")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid SKU: %s", sku)
	}

	item := &Item{
		Craftable: true,
		Tradable:  true,
		// all other fields default to zero/false
	}

	// defindex
	defindex, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid defindex: %s", parts[0])
	}

	item.Defindex = defindex

	// quality
	quality, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid quality: %s", parts[1])
	}

	item.Quality = quality

	// process remaining attributes
	for _, part := range parts[2:] {
		attr := part
		// Special case: some attributes use dashes (kt-3, td-400), we should normalize them for matching
		// but keep them for others (like spells s-1009-1)
		normAttr := strings.ReplaceAll(attr, "-", "")

		switch {
		case normAttr == "uncraftable":
			item.Craftable = false
		case normAttr == "untradeable" || normAttr == "untradable":
			item.Tradable = false
		case normAttr == "australium":
			item.Australium = true
		case normAttr == "festive":
			item.Festivized = true
		case normAttr == "strange":
			item.Quality2 = 11
		case strings.HasPrefix(normAttr, "kt") && len(normAttr) > 2:
			if val, err := strconv.Atoi(normAttr[2:]); err == nil {
				item.Killstreak = val
			}
		case strings.HasPrefix(normAttr, "u") && len(normAttr) > 1:
			if val, err := strconv.Atoi(normAttr[1:]); err == nil {
				item.Effect = val
			}
		case strings.HasPrefix(normAttr, "pk") && len(normAttr) > 2:
			if val, err := strconv.Atoi(normAttr[2:]); err == nil {
				item.Paintkit = val
			}
		case strings.HasPrefix(normAttr, "w") && len(normAttr) > 1:
			if val, err := strconv.Atoi(normAttr[1:]); err == nil {
				item.Wear = val
			}
		case strings.HasPrefix(normAttr, "td") && len(normAttr) > 2:
			if val, err := strconv.Atoi(normAttr[2:]); err == nil {
				item.Target = val
			}
		case strings.HasPrefix(normAttr, "n") && len(normAttr) > 1:
			if val, err := strconv.Atoi(normAttr[1:]); err == nil {
				item.Craftnumber = val
			}
		case strings.HasPrefix(normAttr, "c") && len(normAttr) > 1:
			if val, err := strconv.Atoi(normAttr[1:]); err == nil {
				item.Crateseries = val
			}
		case strings.HasPrefix(normAttr, "od") && len(normAttr) > 2:
			if val, err := strconv.Atoi(normAttr[2:]); err == nil {
				item.Output = val
			}
		case strings.HasPrefix(normAttr, "oq") && len(normAttr) > 2:
			if val, err := strconv.Atoi(normAttr[2:]); err == nil {
				item.OutputQuality = val
			}
		case strings.HasPrefix(normAttr, "p") && len(normAttr) > 1 && !strings.Contains(attr, "-"):
			if val, err := strconv.Atoi(normAttr[1:]); err == nil {
				item.Paint = val
			}
		case strings.HasPrefix(attr, "s-") && len(attr) > 2:
			spellParts := strings.Split(attr[2:], "-")
			if len(spellParts) == 2 {
				a, _ := strconv.Atoi(spellParts[0])
				v, _ := strconv.Atoi(spellParts[1])
				item.Spells = append(item.Spells, Spell{Attribute: a, Value: v})
			}

		case strings.HasPrefix(attr, "sp") && len(attr) > 2:
			if val, err := strconv.Atoi(attr[2:]); err == nil {
				item.Parts = append(item.Parts, val)
			}
		case strings.HasPrefix(attr, "s") && len(attr) > 1:
			if val, err := strconv.Atoi(attr[1:]); err == nil {
				item.Spells = append(item.Spells, Spell{Attribute: val, Value: 1})
			}
		}
	}

	return item, nil
}

// FromObject converts an Item into its SKU string representation.
// The output format follows the conventions used in the original JavaScript code.
func FromObject(item *Item) string {
	var b strings.Builder
	b.Grow(64)

	b.WriteString(strconv.Itoa(item.Defindex))
	b.WriteByte(';')
	b.WriteString(strconv.Itoa(item.Quality))

	if item.Effect != 0 {
		b.WriteString(";u")
		b.WriteString(strconv.Itoa(item.Effect))
	}

	if item.Australium {
		b.WriteString(";australium")
	}

	if !item.Craftable {
		b.WriteString(";uncraftable")
	}

	if !item.Tradable {
		b.WriteString(";untradable")
	}

	if item.Wear != 0 {
		b.WriteByte(';')
		b.WriteByte('w')
		b.WriteString(strconv.Itoa(item.Wear))
	}

	if item.Paintkit != 0 {
		b.WriteString(";pk")
		b.WriteString(strconv.Itoa(item.Paintkit))
	}

	if item.Quality2 == 11 {
		b.WriteString(";strange")
	}

	if item.Killstreak != 0 {
		b.WriteString(";kt-")
		b.WriteString(strconv.Itoa(item.Killstreak))
	}

	if item.Target != 0 {
		b.WriteString(";td-")
		b.WriteString(strconv.Itoa(item.Target))
	}

	if item.Festivized {
		b.WriteString(";festive")
	}

	if item.Craftnumber != 0 {
		b.WriteByte(';')
		b.WriteByte('n')
		b.WriteString(strconv.Itoa(item.Craftnumber))
	}

	if item.Crateseries != 0 {
		b.WriteByte(';')
		b.WriteByte('c')
		b.WriteString(strconv.Itoa(item.Crateseries))
	}

	if item.Output != 0 {
		b.WriteString(";od-")
		b.WriteString(strconv.Itoa(item.Output))
	}

	if item.OutputQuality != 0 {
		b.WriteString(";oq-")
		b.WriteString(strconv.Itoa(item.OutputQuality))
	}

	if item.Paint != 0 {
		b.WriteByte(';')
		b.WriteByte('p')
		b.WriteString(strconv.Itoa(item.Paint))
	}

	for _, spell := range item.Spells {
		if spell.Attribute == 0 {
			continue
		}

		b.WriteString(";s-")
		b.WriteString(strconv.Itoa(spell.Attribute))
		b.WriteByte('-')
		b.WriteString(strconv.Itoa(spell.Value))
	}

	for _, partID := range item.Parts {
		b.WriteString(";sp")
		b.WriteString(strconv.Itoa(partID))
	}

	return b.String()
}
