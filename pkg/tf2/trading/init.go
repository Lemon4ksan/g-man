// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trading

import (
	"fmt"

	tf2reason "github.com/lemon4ksan/g-man/pkg/tf2/reason"
	"github.com/lemon4ksan/g-man/pkg/trading/notifications"
	"github.com/lemon4ksan/g-man/pkg/trading/review"
)

func init() {
	// Register TF2 specific notification templates.
	// This keeps the core trading package game-agnostic.
	notifications.RegisterDefaultTemplate(
		"decline."+tf2reason.DeclineNonTF2.String(),
		"/pre ❌ This bot only trades TF2 items. Your offer was declined.",
	)
	notifications.RegisterDefaultTemplate(
		"decline."+tf2reason.DeclineCrimeAttempt.String(),
		"/pre ❌ Your offer was declined for attempting to take items for free.",
	)
	notifications.RegisterDefaultTemplate(
		"decline."+tf2reason.ReviewInvalidValue.String(),
		"/pre ❌ Your offer was declined due to an invalid value. {{if .MissingValue}}Missing: {{.MissingValue}}{{end}}",
	)
	notifications.RegisterDefaultTemplate(
		"decline."+tf2reason.DeclineNoChange.String(),
		"/pre ❌ I don't have enough small items to give you change right now. Please try again later.",
	)
	notifications.RegisterDefaultTemplate(
		"decline."+tf2reason.DeclineBannedBptf.String(),
		"/pre ❌ You are banned on backpack.tf. I do not trade with banned users.",
	)
	notifications.RegisterDefaultTemplate(
		"decline."+tf2reason.DeclineUnderpaid.String(),
		"/pre ❌ You have underpaid for the items. Please check the prices and try again.",
	)

	// Register TF2 specific review reasons.
	review.RegisterReason(
		tf2reason.ReviewDupedItems,
		"Items appeared to be duped.",
		func(raw any, s review.SchemaProvider, f review.Formatter) string {
			r := raw.(*review.ReasonDuped)
			name := s.GetName(r.SKU, false)
			link := "https://backpack.tf/item/" + r.AssetID

			return fmt.Sprintf("%s - history: %s", f.Item(name), f.Link("view", link))
		},
	)
}
