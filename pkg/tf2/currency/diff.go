// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package currency

import "fmt"

// ValueDiff represents the difference between our side and their side.
type ValueDiff struct {
	OurValue   Scrap
	TheirValue Scrap
	KeyPrice   Scrap // Key price in scrap
}

// NewValueDiff calculates the difference in trade value.
// values are expected to be in Scrap.
func NewValueDiff(our, their, keyPrice Scrap) ValueDiff {
	return ValueDiff{
		OurValue:   our,
		TheirValue: their,
		KeyPrice:   keyPrice,
	}
}

func (v ValueDiff) Diff() Scrap {
	return v.TheirValue - v.OurValue
}

// IsProfitable returns true if they are paying equal or more than us.
func (v ValueDiff) IsProfitable() bool {
	return v.TheirValue >= v.OurValue
}

// MissingRefined returns how much metal is missing in Refined format.
func (v ValueDiff) MissingRefined() float64 {
	if v.IsProfitable() {
		return 0
	}

	diff := v.OurValue - v.TheirValue

	return float64(diff) / 9.0
}

// MissingString formats the missing amount (e.g., "0.55 ref" or "1 key, 2 ref").
func (v ValueDiff) MissingString() string {
	if v.IsProfitable() {
		return "0 ref"
	}

	missingScrap := v.OurValue - v.TheirValue

	if v.KeyPrice > 0 && missingScrap >= v.KeyPrice {
		keys := int(missingScrap / v.KeyPrice)
		leftoverScrap := missingScrap % v.KeyPrice

		if leftoverScrap == 0 {
			return fmt.Sprintf("%d keys", keys)
		}

		return fmt.Sprintf("%d keys, %s", keys, FormatRefinedForDisplay(leftoverScrap))
	}

	return FormatRefinedForDisplay(missingScrap)
}

// FormatRefinedForDisplay uses %.2f, which correctly rounds 0.555... to 0.56
func FormatRefinedForDisplay(s Scrap) string {
	return fmt.Sprintf("%.2f ref", float64(s)/9.0)
}
