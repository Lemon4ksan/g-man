// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package backpack

import (
	"slices"

	"github.com/lemon4ksan/g-man/pkg/tf2"
	"github.com/lemon4ksan/g-man/pkg/tf2/schema"
)

type Filter func(item *tf2.Item, s *schema.Schema) bool

type PageLayout struct {
	Filters []Filter
}

type Layout struct {
	Pages map[int]PageLayout
}

func BySKU(targetSKU string) Filter {
	return func(item *tf2.Item, s *schema.Schema) bool {
		return s.GetSKUFromEconItem(item.ToEconItem()) == targetSKU
	}
}

func ByQuality(q uint32) Filter {
	return func(item *tf2.Item, s *schema.Schema) bool {
		return item.Quality == q
	}
}

func ByClass(class string) Filter {
	return func(item *tf2.Item, s *schema.Schema) bool {
		sch := item.GetSchema(s)
		if sch == nil {
			return false
		}

		return slices.Contains(sch.UsedByClasses, class)
	}
}

func IsPure() Filter {
	return func(item *tf2.Item, s *schema.Schema) bool {
		d := item.DefIndex
		return d == 5021 || d == 5002 || d == 5001 || d == 5000
	}
}
