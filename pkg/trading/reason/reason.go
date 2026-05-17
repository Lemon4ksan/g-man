// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package reason contains the possible trade failure reasons for processing.
package reason

// TradeReason is the type for string identifiers of trade reasons.
type TradeReason string

// Inventory and generic trade reasons.
const (
	ReviewInvalidItems                TradeReason = "🟨_INVALID_ITEMS"
	ReviewDisabledItems               TradeReason = "🟧_DISABLED_ITEMS"
	ReviewOverstocked                 TradeReason = "🟦_OVERSTOCKED"
	ReviewUnderstocked                TradeReason = "🟩_UNDERSTOCKED"
	ReviewBannedCheckFailed           TradeReason = "⬜_BANNED_CHECK_FAILED"
	ReviewEscrowCheckFailed           TradeReason = "⬜_ESCROW_CHECK_FAILED"
	ReviewHalted                      TradeReason = "⬜_HALTED"
	ReviewReviewForced                TradeReason = "⬜_REVIEW_FORCED"
	ReviewEngineError                 TradeReason = "⬜_ENGINE_ERROR"
	ReviewPartnerInventoryFetchFailed TradeReason = "⬜_PARTNER_INVENTORY_FETCH_FAILED"
	DeclineManual                     TradeReason = "MANUAL"
	DeclineHalted                     TradeReason = "HALTED"
	DeclineEscrow                     TradeReason = "ESCROW"
	DeclineBanned                     TradeReason = "BANNED"
	DeclineBlacklisted                TradeReason = "BLACKLISTED"
	DeclineOverstocked                TradeReason = "OVERSTOCKED"
	DeclineBegging                    TradeReason = "BEGGING"

	DeclineInternalError TradeReason = "INTERNAL_ERROR"
	DeclineJunkDonation  TradeReason = "JUNK_DONATION"

	AcceptDonation     TradeReason = "DONATION"
	AcceptCorrectValue TradeReason = "CORRECT_VALUE"
)

func (r TradeReason) String() string {
	return string(r)
}
