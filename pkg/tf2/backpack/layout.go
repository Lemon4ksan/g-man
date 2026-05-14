// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package backpack

import (
	"slices"

	"github.com/lemon4ksan/g-man/pkg/tf2"
	"github.com/lemon4ksan/g-man/pkg/tf2/schema"
)

// Filter is a function that filters items in the backpack.
type Filter func(item *tf2.Item, s *schema.Schema) bool

// PageLayout represents a layout of a page in the backpack.
type PageLayout struct {
	Filters []Filter
}

// Layout represents a layout of the backpack.
type Layout struct {
	Pages map[int]PageLayout
}

// BySKU returns a filter that checks if the item matches the specified SKU.
func BySKU(targetSKU string) Filter {
	return func(item *tf2.Item, s *schema.Schema) bool {
		return item.GetSKU(s) == targetSKU
	}
}

// ByQuality returns a filter that checks if the item has the specified quality.
func ByQuality(q uint32) Filter {
	return func(item *tf2.Item, s *schema.Schema) bool {
		return item.Quality == q
	}
}

// ByClass returns a filter that checks if the item is used by the specified class.
func ByClass(class string) Filter {
	return func(item *tf2.Item, s *schema.Schema) bool {
		sch := item.GetSchema(s)
		if sch == nil {
			return false
		}

		return slices.Contains(sch.UsedByClasses, class)
	}
}

// IsPure returns a filter that checks if the item is pure (reclaimed metal, refined metal, keys).
func IsPure() Filter {
	return func(item *tf2.Item, s *schema.Schema) bool {
		d := s.NormalizeDefindex(int(item.DefIndex))
		return d == schema.DefKey || d == schema.DefRefined || d == schema.DefReclaimed || d == schema.DefScrap
	}
}
