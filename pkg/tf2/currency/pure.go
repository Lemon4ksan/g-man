// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package currency

import (
	"fmt"
	"math"
)

// PureSKUs constants
const (
	SKUKey       = "5021;6"
	SKURefined   = "5002;6"
	SKUReclaimed = "5001;6"
	SKUScrap     = "5000;6"
)

// PureStock represents the current pure currency a bot holds.
type PureStock struct {
	Keys      int
	Refined   int
	Reclaimed int
	Scrap     int
}

// TotalScrap returns the total value of metal in scrap (excluding keys).
func (p PureStock) TotalScrap() int {
	return (p.Refined * 9) + (p.Reclaimed * 3) + p.Scrap
}

// TotalRefined returns the total value of metal in refined (float).
func (p PureStock) TotalRefined() float64 {
	return float64(p.TotalScrap()) / 9.0
}

// FormatStock returns a human-readable string array of pure stock (e.g. ["10 keys", "5.33 ref"]).
func (p PureStock) FormatStock() []string {
	var result []string

	// Keys
	if p.Keys > 0 {
		keyStr := "key"
		if p.Keys > 1 {
			keyStr = "keys"
		}
		result = append(result, fmt.Sprintf("%d %s", p.Keys, keyStr))
	}

	// Metal
	totalRef := p.TotalRefined()
	if totalRef > 0 {
		// Truncate to 2 decimal places for Refined
		refTruncated := math.Trunc(totalRef*100) / 100

		metalStr := fmt.Sprintf("%.2f ref", refTruncated)

		// Add detailed breakdown if needed (e.g., "(5 ref, 1 rec)")
		if p.Refined > 0 || p.Reclaimed > 0 || p.Scrap > 0 {
			var details []string
			if p.Refined > 0 {
				details = append(details, fmt.Sprintf("%d ref", p.Refined))
			}
			if p.Reclaimed > 0 {
				details = append(details, fmt.Sprintf("%d rec", p.Reclaimed))
			}
			if p.Scrap > 0 {
				details = append(details, fmt.Sprintf("%d scrap", p.Scrap))
			}
		}

		result = append(result, metalStr)
	}

	return result
}
