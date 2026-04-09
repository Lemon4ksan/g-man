// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package currency provides structures for tf2 currencies calculations.
package currency

import (
	"errors"
	"fmt"
	"math"
	"strconv"
)

type Scrap int

const (
	ScrapInRec = 3
	ScrapInRef = 9
)

// Currency represent a currency object (keys and metal).
type Currency struct {
	Keys  float64 `json:"keys"`
	Metal float64 `json:"metal"`
}

// New creates a new Currencies instance.
func New(keys, metal float64) *Currency {
	return &Currency{Keys: keys, Metal: metal}
}

// String implements the Stringer interface.
// Returns a string representation, for example: "1 key, 20.11 ref".
func (c *Currency) String() string {
	if c.Keys == 0 && c.Metal == 0 {
		return "0 keys, 0 ref"
	}

	var parts []string

	if c.Keys != 0 || c.Keys == c.Metal {
		parts = append(parts, pluralize("key", c.Keys))
	}

	if c.Metal != 0 || c.Keys == c.Metal {
		metalStr := strconv.FormatFloat(truncate(c.Metal, 2), 'f', -1, 64)
		parts = append(parts, metalStr+" ref")
	}

	return c.formatString()
}

func (c *Currency) formatString() string {
	var kStr, mStr string
	if c.Keys > 0 {
		kStr = fmt.Sprintf("%g key", c.Keys)
		if c.Keys != 1 {
			kStr += "s"
		}
	}
	if c.Metal > 0 || c.Keys == 0 {
		mStr = fmt.Sprintf("%.2f ref", c.Metal)
	}
	if kStr != "" && mStr != "" {
		return kStr + ", " + mStr
	}
	return kStr + mStr
}

// ToValue returns the value of currencies in scrap metal.
// Conversion is the cost of one key in refined units.
// If there are no keys, conversion can be passed as 0.
func (c *Currency) ToValue(keyPriceRef float64) (Scrap, error) {
	if keyPriceRef == 0 && c.Keys != 0 {
		return 0, errors.New("missing conversion rate")
	}

	metalValue := ToScrap(c.Metal)
	if c.Keys != 0 {
		keyPriceScrap := ToScrap(keyPriceRef)
		keyValue := Scrap(math.Round(c.Keys * float64(keyPriceScrap)))
		return metalValue + keyValue, nil
	}
	return metalValue, nil
}

// AddRefined adds the values ​​of the refs.
func AddRefined(args ...float64) float64 {
	var total Scrap
	for _, ref := range args {
		total += ToScrap(ref)
	}
	return ToRefined(total)
}

// ScrapToCurrencies converts scrap metal into a Currencies object.
// value - the value in scrap.
// conversion - the key exchange rate in refs (if 0/undefined, returns only metal).
func ScrapToCurrencies(total Scrap, keyPriceRef float64) *Currency {
	if keyPriceRef <= 0 {
		return New(0, ToRefined(total))
	}

	keyPriceScrap := ToScrap(keyPriceRef)
	keys := int(total) / int(keyPriceScrap)
	leftover := total % Scrap(keyPriceScrap)

	return New(float64(keys), ToRefined(leftover))
}

// ToScrap converts refs (refined) to scrap.
func ToScrap(refined float64) Scrap {
	return Scrap(math.Round(refined * float64(ScrapInRef)))
}

// ToRefined converts scrap to refs.
func ToRefined(s Scrap) float64 {
	return float64(s) / float64(ScrapInRef)
}

func FormatRefined(s Scrap) string {
	return fmt.Sprintf("%.2f ref", ToRefined(s))
}

func truncate(number float64, decimals int) float64 {
	factor := math.Pow(10, float64(decimals))
	return rounding(number*factor) / factor
}

func rounding(number float64) float64 {
	isPositive := number >= 0
	absNum := math.Abs(number)

	var res float64
	if absNum+0.001 > math.Ceil(absNum) {
		res = math.Round(absNum)
	} else {
		res = math.Floor(absNum)
	}

	if isPositive {
		return res
	}
	return -res
}

func pluralize(word string, count float64) string {
	strCount := strconv.FormatFloat(count, 'f', -1, 64)
	if count == 1 {
		return strCount + " " + word
	}
	return strCount + " " + word + "s"
}
