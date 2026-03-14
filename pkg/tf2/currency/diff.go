// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package currency

import (
	"fmt"
	"math"
)

// ValueDiff represents the difference between our side and their side.
type ValueDiff struct {
	OurValueScrap   int
	TheirValueScrap int
	DiffScrap       int
	RateScrap       int // Key price in scrap
}

// NewValueDiff calculates the difference in trade value.
// values are expected to be in Scrap.
func NewValueDiff(ourScrap, theirScrap, keyPriceScrap int) ValueDiff {
	return ValueDiff{
		OurValueScrap:   ourScrap,
		TheirValueScrap: theirScrap,
		DiffScrap:       theirScrap - ourScrap,
		RateScrap:       keyPriceScrap,
	}
}

// IsProfitable returns true if they are paying equal or more than us.
func (v ValueDiff) IsProfitable() bool {
	return v.DiffScrap >= 0
}

// MissingRefined returns how much metal is missing in Refined format.
func (v ValueDiff) MissingRefined() float64 {
	if v.IsProfitable() {
		return 0
	}
	return float64(math.Abs(float64(v.DiffScrap))) / 9.0
}

// MissingString formats the missing amount (e.g., "0.55 ref" or "1 key, 2 ref").
func (v ValueDiff) MissingString() string {
	if v.IsProfitable() {
		return "0 ref"
	}

	missing := int(math.Abs(float64(v.DiffScrap)))

	if missing >= v.RateScrap && v.RateScrap > 0 {
		keys := missing / v.RateScrap
		leftoverScrap := missing % v.RateScrap
		leftoverRef := float64(leftoverScrap) / 9.0

		if leftoverScrap > 0 {
			return fmt.Sprintf("%d keys, %.2f ref", keys, leftoverRef)
		}
		return fmt.Sprintf("%d keys", keys)
	}

	return fmt.Sprintf("%.2f ref", float64(missing)/9.0)
}
