// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package review

import (
	"fmt"
	"strings"

	"github.com/lemon4ksan/g-man/pkg/offer/reason"
)

func formatNote(template, defaultNote, itemsNameStr string, count int) string {
	if template != "" {
		isOrAre := "is"
		if count > 1 {
			isOrAre = "are"
		}

		replacer := strings.NewReplacer(
			"%itemsName%", itemsNameStr,
			"%isOrAre%", isOrAre,
		)
		return replacer.Replace(template)
	}
	return defaultNote
}

func processOverstocked(meta *Meta, schema SchemaProvider, cfg ConfigProvider) (note string, namesOur []string) {
	var theirNames []string

	for _, rawReason := range meta.Reasons {
		if r, ok := rawReason.(*ReasonOverstocked); ok {
			name := schema.GetName(r.SKU, false)

			ourFmt := fmt.Sprintf("%s (can only buy %d, offering %d)", name, r.AmountCanTrade, r.AmountOffered)
			if cfg.IsWebhookEnabled() {
				ourFmt = fmt.Sprintf("_%s_ (can only buy %d, offering %d)", name, r.AmountCanTrade, r.AmountOffered)
			}
			namesOur = append(namesOur, ourFmt)

			theirNames = append(theirNames, fmt.Sprintf("%d - %s", r.AmountCanTrade, name))
		}
	}

	joinedTheir := strings.Join(theirNames, ", ")
	template := cfg.GetReviewTemplate(reason.ReviewOverstocked)
	defaultNote := fmt.Sprintf(reason.ReviewOverstocked.String()+" - I can only buy %s right now.", joinedTheir)

	note = formatNote(template, defaultNote, joinedTheir, len(theirNames))
	if template != "" {
		note = reason.ReviewOverstocked.String() + " - " + note
	}

	return note, namesOur
}

func processInvalidItems(meta *Meta, schema SchemaProvider, cfg ConfigProvider) (note string, namesOur []string) {
	var theirNames []string

	for _, rawReason := range meta.Reasons {
		if r, ok := rawReason.(*ReasonInvalidItems); ok {
			name := schema.GetName(r.SKU, false)

			ourFmt := fmt.Sprintf("%s - %s", name, r.Price)
			if cfg.IsWebhookEnabled() {
				ourFmt = fmt.Sprintf("_%s_ - %s", name, r.Price)
			}
			namesOur = append(namesOur, ourFmt)
			theirNames = append(theirNames, name)
		}
	}

	joinedTheir := strings.Join(theirNames, ", ")
	template := cfg.GetReviewTemplate(reason.ReviewInvalidItems)

	isOrAre := "is"
	if len(theirNames) > 1 {
		isOrAre = "are"
	}

	defaultNote := fmt.Sprintf(reason.ReviewInvalidItems.String()+" - %s %s not in my pricelist.", joinedTheir, isOrAre)

	note = formatNote(template, defaultNote, joinedTheir, len(theirNames))
	if template != "" {
		note = reason.ReviewInvalidItems.String() + " - " + note
	}

	return note, namesOur
}

func processDisabledItems(meta *Meta, schema SchemaProvider, cfg ConfigProvider) (note string, namesOur []string) {
	var theirNames []string
	for _, rawReason := range meta.Reasons {
		if r, ok := rawReason.(*BaseReason); ok && r.ReasonType() == reason.ReviewDisabledItems {
			name := schema.GetName(r.SKU, false)
			namesOur = append(namesOur, name)
			theirNames = append(theirNames, name)
		}
	}
	joined := strings.Join(theirNames, ", ")
	template := cfg.GetReviewTemplate(reason.ReviewDisabledItems)
	defaultNote := fmt.Sprintf(reason.ReviewDisabledItems.String()+" - %s %s currently disabled.", joined, "is/are")
	return formatNote(template, defaultNote, joined, len(theirNames)), namesOur
}

func processUnderstocked(meta *Meta, schema SchemaProvider, cfg ConfigProvider) (note string, namesOur []string) {
	var theirNames []string
	for _, rawReason := range meta.Reasons {
		if r, ok := rawReason.(*BaseReason); ok && r.ReasonType() == reason.ReviewUnderstocked {
			name := schema.GetName(r.SKU, false)
			namesOur = append(namesOur, name)
			theirNames = append(theirNames, name)
		}
	}
	joined := strings.Join(theirNames, ", ")
	template := cfg.GetReviewTemplate(reason.ReviewUnderstocked)
	defaultNote := fmt.Sprintf(reason.ReviewUnderstocked.String()+" - I can only sell %s right now.", joined)
	return formatNote(template, defaultNote, joined, len(theirNames)), namesOur
}

func processDuped(meta *Meta, schema SchemaProvider, cfg ConfigProvider) (note string, namesOur []string) {
	var theirNames []string
	for _, rawReason := range meta.Reasons {
		if r, ok := rawReason.(*ReasonDuped); ok {
			name := schema.GetName(r.SKU, false)
			link := fmt.Sprintf("https://backpack.tf/item/%s", r.AssetID)

			ourFmt := fmt.Sprintf("%s - history page: %s", name, link)
			if cfg.IsWebhookEnabled() {
				ourFmt = fmt.Sprintf("_%s_ - [history page](%s)", name, link)
			}
			namesOur = append(namesOur, ourFmt)
			theirNames = append(theirNames, fmt.Sprintf("%s, history page: %s", name, link))
		}
	}
	joined := strings.Join(theirNames, ", ")
	template := cfg.GetReviewTemplate(reason.ReviewDupedItems)
	defaultNote := fmt.Sprintf(reason.ReviewDupedItems.String()+" - %s appeared to be duped.", joined)
	return formatNote(template, defaultNote, joined, len(theirNames)), namesOur
}

func processDupeCheckFailed(meta *Meta, schema SchemaProvider, cfg ConfigProvider) (note string, namesOur []string) {
	var theirNames []string
	for _, rawReason := range meta.Reasons {
		if r, ok := rawReason.(*ReasonDuped); ok {
			name := schema.GetName(r.SKU, false)
			link := fmt.Sprintf("https://backpack.tf/item/%s", r.AssetID)

			ourFmt := fmt.Sprintf("%s - history page: %s", name, link)
			if cfg.IsWebhookEnabled() {
				ourFmt = fmt.Sprintf("_%s_ - [history page](%s)", name, link)
			}
			namesOur = append(namesOur, ourFmt)
			theirNames = append(theirNames, fmt.Sprintf("%s, history page: %s", name, link))
		}
	}
	template := cfg.GetReviewTemplate(reason.ReviewDupeCheckFailed)
	return formatNote(template, reason.ReviewDupeCheckFailed.String()+" - Failed to check for dupes.", "items", 1), namesOur
}
