// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package reason contains the possible trade failure reasons for processing.
package reason

// TradeReason is the type for string identifiers of trade reasons.
type TradeReason string

const (
	ReviewInvalidItems         TradeReason = "🟨_INVALID_ITEMS"
	ReviewDisabledItems        TradeReason = "🟧_DISABLED_ITEMS"
	ReviewOverstocked          TradeReason = "🟦_OVERSTOCKED"
	ReviewUnderstocked         TradeReason = "🟩_UNDERSTOCKED"
	ReviewDupedItems           TradeReason = "🟫_DUPED_ITEMS"
	ReviewDupeCheckFailed      TradeReason = "🟪_DUPE_CHECK_FAILED"
	ReviewInvalidValue         TradeReason = "🟥_INVALID_VALUE"
	ReviewBannedCheckFailed    TradeReason = "⬜_BANNED_CHECK_FAILED"
	ReviewEscrowCheckFailed    TradeReason = "⬜_ESCROW_CHECK_FAILED"
	ReviewHalted               TradeReason = "⬜_HALTED"
	ReviewReviewForced         TradeReason = "⬜_REVIEW_FORCED"
	DeclineCounterInvalidValue TradeReason = "COUNTER_INVALID_VALUE_FAILED"
	DeclineManual              TradeReason = "MANUAL"
	DeclineHalted              TradeReason = "HALTED"
	DeclineEscrow              TradeReason = "ESCROW"
	DeclineBanned              TradeReason = "BANNED"
	DeclineNonTF2              TradeReason = "CONTAINS_NON_TF2"
	DeclineGiftNoNote          TradeReason = "GIFT_NO_NOTE"
	DeclineCrimeAttempt        TradeReason = "CRIME_ATTEMPT"
	DeclineIntentBuy           TradeReason = "TAKING_ITEMS_WITH_INTENT_BUY"
	DeclineIntentSell          TradeReason = "GIVING_ITEMS_WITH_INTENT_SELL"
	DeclineOverpay             TradeReason = "OVERPAY"
	DeclineDuelingUses         TradeReason = "DUELING_NOT_5_USES"
	DeclineNoisemakerUses      TradeReason = "NOISE_MAKER_NOT_25_USES"
	DeclineHighValueNotSell    TradeReason = "HIGH_VALUE_ITEMS_NOT_SELLING"
	DeclineOnlyMetal           TradeReason = "ONLY_METAL"
	DeclineNotTradingKeys      TradeReason = "NOT_TRADING_KEYS"
	DeclineNotSellingKeys      TradeReason = "NOT_SELLING_KEYS"
	DeclineNotBuyingKeys       TradeReason = "NOT_BUYING_KEYS"
	DeclineKeysOnBothSides     TradeReason = "CONTAINS_KEYS_ON_BOTH_SIDES"
	DeclineItemsOnBothSides    TradeReason = "CONTAINS_ITEMS_ON_BOTH_SIDES"
)

func (r TradeReason) String() string {
	return string(r)
}
