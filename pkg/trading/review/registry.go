// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package review

import (
	"fmt"

	"github.com/lemon4ksan/g-man/pkg/trading/reason"
)

// ReasonProcessor is a function that formats a specific trade reason into a human-readable string.
type ReasonProcessor func(raw any, s SchemaProvider, f Formatter) string

var reasonRegistry = map[reason.TradeReason]struct {
	Description string
	Processor   ReasonProcessor
}{
	reason.DeclineEscrow: {Description: "Partner has trade hold."},
	reason.DeclineBanned: {Description: "Partner is banned in one or more communities."},
	reason.ReviewOverstocked: {
		Description: "Offer contains items that'll make us overstocked.",
		Processor: func(raw any, s SchemaProvider, f Formatter) string {
			r := raw.(*ReasonOverstocked)

			return fmt.Sprintf(
				"%s (can buy %d, offered %d)",
				f.Item(s.GetName(r.SKU, false)),
				r.AmountCanTrade,
				r.AmountOffered,
			)
		},
	},
	reason.ReviewInvalidItems: {
		Description: "Items not in pricelist.",
		Processor: func(raw any, s SchemaProvider, f Formatter) string {
			r := raw.(*ReasonInvalidItems)
			return fmt.Sprintf("%s (%s)", f.Item(s.GetName(r.SKU, false)), r.Price)
		},
	},
}

// RegisterReason adds or updates a reason in the registry.
func RegisterReason(r reason.TradeReason, description string, processor ReasonProcessor) {
	reasonRegistry[r] = struct {
		Description string
		Processor   ReasonProcessor
	}{
		Description: description,
		Processor:   processor,
	}
}
