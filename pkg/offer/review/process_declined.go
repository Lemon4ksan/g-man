// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package review

import (
	"fmt"
	"strings"

	"github.com/lemon4ksan/g-man/pkg/offer/reason"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// ProcessDeclined collects information about the rejected trade.
func ProcessDeclined(meta *TradeMetadata, schema SchemaProvider, isWebhookEnabled bool) DeclinedSummary {
	declined := DeclinedSummary{
		InvalidItems:        make([]string, 0),
		DisabledItems:       make([]string, 0),
		Overstocked:         make([]string, 0),
		Understocked:        make([]string, 0),
		DupedItems:          make([]string, 0),
		HighNotSellingItems: make([]string, 0),
		HighValue:           make([]string, 0),
	}

	if !meta.IsOfferSent {
		declined.ReasonDescription = getPrimaryReasonDescription(meta)
	}

	for _, rawReason := range meta.Reasons {
		switch r := rawReason.(type) {
		case *ReasonInvalidItems:
			name := schema.GetName(r.SKU, false)
			declined.InvalidItems = append(declined.InvalidItems, formatItem(name, r.Price, isWebhookEnabled))

		case *ReasonDisabledItems:
			name := schema.GetName(r.SKU, false)
			declined.DisabledItems = append(declined.DisabledItems, formatItem(name, "", isWebhookEnabled))

		case *ReasonOverstocked:
			name := schema.GetName(r.SKU, false)
			desc := fmt.Sprintf("(amount can buy was %d, offered %d)", r.AmountCanTrade, r.AmountOffered)
			declined.Overstocked = append(declined.Overstocked, formatItem(name, desc, isWebhookEnabled))

		case *ReasonUnderstocked:
			name := schema.GetName(r.SKU, false)
			desc := fmt.Sprintf("(amount can sell was %d, taken %d)", r.AmountCanTrade, r.AmountTaking)
			declined.Understocked = append(declined.Understocked, formatItem(name, desc, isWebhookEnabled))

		case *ReasonDuped:
			name := schema.GetName(r.SKU, false)
			declined.DupedItems = append(declined.DupedItems, formatItem(name, "", isWebhookEnabled))
		}
	}

	if meta.PrimaryReason == reason.DeclineHighValueNotSell {
		declined.HighNotSellingItems = append(declined.HighNotSellingItems, meta.HighValueNamesOur...)
	}

	for _, name := range meta.HighValueNamesTheir {
		declined.HighValue = append(declined.HighValue, formatItem(name, "", isWebhookEnabled))
	}
	for _, name := range meta.HighValueNamesOur {
		declined.HighValue = append(declined.HighValue, formatItem(name, "", isWebhookEnabled))
	}

	return declined
}

func formatItem(name, suffix string, isWebhook bool) string {
	formattedName := name
	if isWebhook {
		formattedName = fmt.Sprintf("_%s_", name)
	}
	if suffix != "" {
		return fmt.Sprintf("%s %s", formattedName, suffix)
	}
	return formattedName
}

func getPrimaryReasonDescription(meta *TradeMetadata) string {
	r := meta.PrimaryReason
	prefix := r.String() + ": "

	switch r {
	case reason.DeclineManual:
		return prefix + "Manually declined by the owner."
	case reason.DeclineHalted:
		return prefix + "The bot is halted."
	case reason.DeclineEscrow:
		return prefix + "Partner has trade hold."
	case reason.DeclineBanned:
		var checks []string
		i := 1
		for website, status := range meta.BannedStatus {
			if status != "clean" {
				checks = append(checks, fmt.Sprintf("(%d) %s: %s", i, website, status))
				i++
			}
		}
		desc := prefix + "Partner is banned in one or more communities."
		if len(checks) > 0 {
			desc += "\nCheck results:\n" + strings.Join(checks, "\n")
		}
		return desc
	case reason.DeclineNonTF2:
		return prefix + "Trade includes non-TF2 items."
	case reason.DeclineGiftNoNote:
		return prefix + "We dont accept gift without gift messages."
	case reason.DeclineCrimeAttempt:
		return prefix + "Tried to take our items for free."
	case reason.DeclineIntentBuy:
		return prefix + "Tried to take/buy our item(s) with intent buy."
	case reason.DeclineIntentSell:
		return prefix + "Tried to give their item(s) with intent sell."
	case reason.DeclineOverpay:
		return prefix + "We are not accepting overpay."
	case reason.DeclineDuelingUses:
		return prefix + "We only accept 5 use Dueling Mini-Games."
	case reason.DeclineNoisemakerUses:
		return prefix + "We only accept 25 use Noise Makers."
	case reason.DeclineHighValueNotSell:
		return prefix + "Tried to take our high value items that we are not selling."
	case reason.DeclineOnlyMetal:
		return prefix + "Offer contains only metal."
	case reason.DeclineNotTradingKeys:
		return prefix + "We are not trading keys."
	case reason.DeclineNotSellingKeys:
		return prefix + "We are not selling keys."
	case reason.DeclineNotBuyingKeys:
		return prefix + "We are not buying keys."
	case reason.ReviewOverstocked:
		return prefix + "Offer contains items that'll make us overstocked."
	case reason.ReviewUnderstocked:
		return prefix + "Offer contains items that'll make us understocked."
	case reason.ReviewDisabledItems:
		return prefix + "Offer contains disabled items."
	case reason.ReviewInvalidItems:
		return prefix + "Offer contains invalid items."
	case reason.ReviewDupedItems:
		return prefix + "Offer contains duped items."
	case reason.ReviewDupeCheckFailed:
		return prefix + "I was unable to determine if this item is duped. Please make sure your inventory is public."
	case reason.ReviewInvalidValue:
		return prefix + "We are paying more than them."
	case reason.DeclineCounterInvalidValue:
		return prefix + "We are paying more than them and we failed to counter the offer."
	}

	if strings.HasPrefix(r.String(), "ONLY_") {
		parts := strings.Split(r.String(), "_")
		if len(parts) > 1 {
			caser := cases.Title(language.English)
			humanized := caser.String(strings.ToLower(strings.Join(parts[1:], " ")))
			return prefix + "We are auto declining " + humanized
		}
	}

	return prefix + "Unknown reason."
}
