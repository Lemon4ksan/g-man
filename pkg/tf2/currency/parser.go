// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package currency

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var currencyRegex = regexp.MustCompile(`([0-9]*\.?[0-9]+)\s*([a-zA-Z]*)`)

// Parse converts a string to a Currency object.
// Supports the following formats: "1.33 ref", "2 keys, 1.33", "50 scrap", "10k"
func Parse(input string) (*Currency, error) {
	input = strings.ToLower(input)
	input = strings.ReplaceAll(input, ",", "")

	matches := currencyRegex.FindAllStringSubmatch(input, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("currency: could not parse input %q", input)
	}

	res := &Currency{}
	foundAny := false

	for _, match := range matches {
		val, err := strconv.ParseFloat(match[1], 64)
		if err != nil {
			continue
		}

		suffix := match[2]
		foundAny = true

		switch {
		case strings.HasPrefix(suffix, "key") || suffix == "k":
			res.Keys += val

		case strings.HasPrefix(suffix, "ref") || suffix == "r" || suffix == "":
			res.Metal = AddRefined(res.Metal, val)

		case strings.HasPrefix(suffix, "rec"):
			metalFromRec := float64(Scrap(val)*ScrapInRec) / float64(ScrapInRef)
			res.Metal = AddRefined(res.Metal, metalFromRec)

		case strings.HasPrefix(suffix, "scr") || strings.HasPrefix(suffix, "s"):
			metalFromScrap := float64(Scrap(val)) / float64(ScrapInRef)
			res.Metal = AddRefined(res.Metal, metalFromScrap)

		default:
			res.Metal = AddRefined(res.Metal, val)
		}
	}

	if !foundAny {
		return nil, fmt.Errorf("currency: no valid values found in %q", input)
	}

	return res, nil
}

// ParseToScrap is a convenient wrapper if we need to immediately get the value in scrap.
func ParseToScrap(input string, keyPriceRef float64) (Scrap, error) {
	curr, err := Parse(input)
	if err != nil {
		return 0, err
	}

	return curr.ToValue(keyPriceRef)
}
