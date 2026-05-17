// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package reason contains TF2-specific trade reasons.
package reason

import "github.com/lemon4ksan/g-man/pkg/trading/reason"

// TF2-specific trade reasons.
const (
	ReviewDupedItems           reason.TradeReason = "🟫_DUPED_ITEMS"
	ReviewDupeCheckFailed      reason.TradeReason = "🟪_DUPE_CHECK_FAILED"
	ReviewInvalidValue         reason.TradeReason = "🟥_INVALID_VALUE"
	DeclineCounterInvalidValue reason.TradeReason = "COUNTER_INVALID_VALUE_FAILED"
	DeclineNonTF2              reason.TradeReason = "CONTAINS_NON_TF2"
	DeclineGiftNoNote          reason.TradeReason = "GIFT_NO_NOTE"
	DeclineCrimeAttempt        reason.TradeReason = "CRIME_ATTEMPT"
	DeclineIntentBuy           reason.TradeReason = "TAKING_ITEMS_WITH_INTENT_BUY"
	DeclineIntentSell          reason.TradeReason = "GIVING_ITEMS_WITH_INTENT_SELL"
	DeclineOverpay             reason.TradeReason = "OVERPAY"
	DeclineUnderpaid           reason.TradeReason = "UNDERPAID"
	DeclineDuelingUses         reason.TradeReason = "DUELING_NOT_5_USES"
	DeclineNoisemakerUses      reason.TradeReason = "NOISE_MAKER_NOT_25_USES"
	DeclineHighValueNotSell    reason.TradeReason = "HIGH_VALUE_ITEMS_NOT_SELLING"
	DeclineOnlyMetal           reason.TradeReason = "ONLY_METAL"
	DeclineNotTradingKeys      reason.TradeReason = "NOT_TRADING_KEYS"
	DeclineNotSellingKeys      reason.TradeReason = "NOT_SELLING_KEYS"
	DeclineNotBuyingKeys       reason.TradeReason = "NOT_BUYING_KEYS"
	DeclineKeysOnBothSides     reason.TradeReason = "CONTAINS_KEYS_ON_BOTH_SIDES"
	DeclineItemsOnBothSides    reason.TradeReason = "CONTAINS_ITEMS_ON_BOTH_SIDES"
	DeclineNoChange            reason.TradeReason = "NO_CHANGE"
	DeclineBannedBptf          reason.TradeReason = "BANNED_BPTF"
	ReviewPricerDown           reason.TradeReason = "⬜_PRICER_DOWN"
	ReviewUnpricedItem         reason.TradeReason = "⬜_UNPRICED_ITEM"
	ReviewInvalidKeyPrice      reason.TradeReason = "⬜_INVALID_KEY_PRICE"
	DeclineJunkDonation        reason.TradeReason = "JUNK_DONATION"
)
