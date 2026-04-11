// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package sku implements the TF2 Stock Keeping Unit format.
// It allows converting complex item attributes into a compact string representation.
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
	Spells        []int
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
		attr := strings.ReplaceAll(part, "-", "") // remove any dashes

		switch {
		case attr == "uncraftable":
			item.Craftable = false
		case attr == "untradeable" || attr == "untradable":
			item.Tradable = false
		case attr == "australium":
			item.Australium = true
		case attr == "festive":
			item.Festivized = true
		case attr == "strange":
			item.Quality2 = 11
		case strings.HasPrefix(attr, "kt") && len(attr) > 2:
			if val, err := strconv.Atoi(attr[2:]); err == nil {
				item.Killstreak = val
			}
		case strings.HasPrefix(attr, "u") && len(attr) > 1:
			if val, err := strconv.Atoi(attr[1:]); err == nil {
				item.Effect = val
			}
		case strings.HasPrefix(attr, "pk") && len(attr) > 2:
			if val, err := strconv.Atoi(attr[2:]); err == nil {
				item.Paintkit = val
			}
		case strings.HasPrefix(attr, "w") && len(attr) > 1:
			if val, err := strconv.Atoi(attr[1:]); err == nil {
				item.Wear = val
			}
		case strings.HasPrefix(attr, "td") && len(attr) > 2:
			if val, err := strconv.Atoi(attr[2:]); err == nil {
				item.Target = val
			}
		case strings.HasPrefix(attr, "n") && len(attr) > 1:
			if val, err := strconv.Atoi(attr[1:]); err == nil {
				item.Craftnumber = val
			}
		case strings.HasPrefix(attr, "c") && len(attr) > 1:
			if val, err := strconv.Atoi(attr[1:]); err == nil {
				item.Crateseries = val
			}
		case strings.HasPrefix(attr, "od") && len(attr) > 2:
			if val, err := strconv.Atoi(attr[2:]); err == nil {
				item.Output = val
			}
		case strings.HasPrefix(attr, "oq") && len(attr) > 2:
			if val, err := strconv.Atoi(attr[2:]); err == nil {
				item.OutputQuality = val
			}
		case strings.HasPrefix(attr, "p") && len(attr) > 1:
			if val, err := strconv.Atoi(attr[1:]); err == nil {
				item.Paint = val
			}
		}
	}

	return item, nil
}

// FromObject converts an Item into its SKU string representation.
// The output format follows the conventions used in the original JavaScript code.
func FromObject(item *Item) (string, error) {
	// base: defindex;quality
	sku := fmt.Sprintf("%d;%d", item.Defindex, item.Quality)

	if item.Effect != 0 {
		sku += fmt.Sprintf(";u%d", item.Effect)
	}

	if item.Australium {
		sku += ";australium"
	}

	if !item.Craftable {
		sku += ";uncraftable"
	}

	if !item.Tradable {
		sku += ";untradable"
	}

	if item.Wear != 0 {
		sku += fmt.Sprintf(";w%d", item.Wear)
	}

	if item.Paintkit != 0 {
		sku += fmt.Sprintf(";pk%d", item.Paintkit)
	}

	if item.Quality2 == 11 {
		sku += ";strange"
	}

	if item.Killstreak != 0 {
		sku += fmt.Sprintf(";kt-%d", item.Killstreak)
	}

	if item.Target != 0 {
		sku += fmt.Sprintf(";td-%d", item.Target)
	}

	if item.Festivized {
		sku += ";festive"
	}

	if item.Craftnumber != 0 {
		sku += fmt.Sprintf(";n%d", item.Craftnumber)
	}

	if item.Crateseries != 0 {
		sku += fmt.Sprintf(";c%d", item.Crateseries)
	}

	if item.Output != 0 {
		sku += fmt.Sprintf(";od-%d", item.Output)
	}

	if item.OutputQuality != 0 {
		sku += fmt.Sprintf(";oq-%d", item.OutputQuality)
	}

	if item.Paint != 0 {
		sku += fmt.Sprintf(";p%d", item.Paint)
	}

	return sku, nil
}
