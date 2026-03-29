// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package currency provides structures for tf2 currencies calculations.
package currency

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// Currency represent a currency object (keys and metal).
type Currency struct {
	Keys  float64 `json:"keys"`
	Metal float64 `json:"metal"`
}

// New creates a new Currencies instance.
func New(keys, metal float64) *Currency {
	c := &Currency{
		Keys:  keys,
		Metal: metal,
	}
	c.Metal = ToRefined(ToScrap(c.Metal))
	return c
}

// String implements the Stringer interface.
// Returns a string representation, for example: "1 key, 20.11 ref".
func (c *Currency) String() string {
	var parts []string

	if c.Keys != 0 || c.Keys == c.Metal {
		parts = append(parts, pluralize("key", c.Keys))
	}

	if c.Metal != 0 || c.Keys == c.Metal {
		metalStr := strconv.FormatFloat(truncate(c.Metal, 2), 'f', -1, 64)
		parts = append(parts, metalStr+" ref")
	}

	return strings.Join(parts, ", ")
}

// ToValue returns the value of currencies in scrap metal.
// Conversion is the cost of one key in refined units.
// If there are no keys, conversion can be passed as 0.
func (c *Currency) ToValue(conversion float64) (float64, error) {
	if conversion == 0 && c.Keys != 0 {
		return 0, errors.New("missing conversion rate for keys in refined")
	}

	value := ToScrap(c.Metal)
	if c.Keys != 0 {
		value += c.Keys * ToScrap(conversion)
	}
	return value, nil
}

// AddRefined adds the values ​​of the refs.
func AddRefined(args ...float64) float64 {
	var value float64 = 0

	for _, refined := range args {
		value += ToScrap(refined)
	}

	return ToRefined(value)
}

// ScrapToCurrencies converts scrap metal into a Currencies object.
// value - the value in scrap.
// conversion - the key exchange rate in refs (if 0/undefined, returns only metal).
func ScrapToCurrencies(value, conversion float64) *Currency {
	if conversion == 0 {
		metal := ToRefined(value)
		return New(0, metal)
	}

	conversionScrap := ToScrap(conversion)
	keys := rounding(value / conversionScrap)
	left := value - keys*conversionScrap
	metal := ToRefined(left)

	return New(keys, metal)
}

// ToScrap converts refs (refined) to scrap.
func ToScrap(refined float64) float64 {
	scrap := refined * 9
	return roundStep(scrap, 0.5)
}

// ToRefined converts scrap to refs.
func ToRefined(scrap float64) float64 {
	refined := scrap / 9
	return truncate(refined, 2)
}

func truncate(number float64, decimals int) float64 {
	factor := math.Pow(10, float64(decimals))
	return rounding(number*factor) / factor
}

func roundStep(number, step float64) float64 {
	if step == 0 {
		step = 1.0
	}
	inv := 1.0 / step
	return math.Round(number*inv) / inv
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
