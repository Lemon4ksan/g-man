// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package review

import (
	"strings"

	"github.com/lemon4ksan/g-man/pkg/offer/reason"
)

// ProcessReview collects all notes and item names based on reasons.
func ProcessReview(meta *Meta, schema SchemaProvider, cfg ConfigProvider) Content {
	content := Content{
		Notes:        make([]string, 0),
		ItemNamesOur: make(map[string][]string),
	}

	type processorFunc func(*Meta, SchemaProvider, ConfigProvider) (string, []string)

	processors := map[reason.TradeReason]struct {
		fn     processorFunc
		key    string
		prefix string
	}{
		reason.ReviewInvalidItems:    {processInvalidItems, "invalidItems", reason.ReviewInvalidItems.String() + " - "},
		reason.ReviewDisabledItems:   {processDisabledItems, "disabledItems", reason.ReviewDisabledItems.String() + " - "},
		reason.ReviewOverstocked:     {processOverstocked, "overstocked", reason.ReviewOverstocked.String() + " - "},
		reason.ReviewUnderstocked:    {processUnderstocked, "understocked", reason.ReviewUnderstocked.String() + " - "},
		reason.ReviewDupedItems:      {processDuped, "duped", reason.ReviewDupedItems.String() + " - "},
		reason.ReviewDupeCheckFailed: {processDupeCheckFailed, "dupedFailed", reason.ReviewDupeCheckFailed.String() + " - "},
	}

	for r, p := range processors {
		if meta.HasReason(r) {
			note, names := p.fn(meta, schema, cfg)

			if !strings.HasPrefix(note, p.prefix) {
				note = p.prefix + note
			}

			content.Notes = append(content.Notes, note)
			content.ItemNamesOur[p.key] = names
		}
	}

	if meta.HasReason(reason.ReviewInvalidValue) && !meta.HasReason(reason.ReviewInvalidItems) {
		// TODO: Need to get pricelist here
	}

	return content
}
