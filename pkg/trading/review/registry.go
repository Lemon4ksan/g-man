package review

import (
	"fmt"

	"github.com/lemon4ksan/g-man/pkg/trading/reason"
)

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
			return fmt.Sprintf("%s (can buy %d, offered %d)", f.Item(s.GetName(r.SKU, false)), r.AmountCanTrade, r.AmountOffered)
		},
	},
	reason.ReviewInvalidItems: {
		Description: "Items not in pricelist.",
		Processor: func(raw any, s SchemaProvider, f Formatter) string {
			r := raw.(*ReasonInvalidItems)
			return fmt.Sprintf("%s (%s)", f.Item(s.GetName(r.SKU, false)), r.Price)
		},
	},
	reason.ReviewDupedItems: {
		Description: "Items appeared to be duped.",
		Processor: func(raw any, s SchemaProvider, f Formatter) string {
			r := raw.(*ReasonDuped)
			name := s.GetName(r.SKU, false)
			link := fmt.Sprintf("https://backpack.tf/item/%s", r.AssetID)
			return fmt.Sprintf("%s - history: %s", f.Item(name), f.Link("view", link))
		},
	},
}
