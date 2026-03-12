// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package notifications provides customizable chat responses
package notifications

import (
	"context"
	"fmt"
	"strings"

	"github.com/lemon4ksan/g-man/pkg/offer/reason"
)

func SendAccepted(ctx context.Context, info *TradeInfo, chat ChatProvider, cfg ConfigProvider) error {
	msg := cfg.GetCustomMessage("success")
	if msg == "" {
		msg = "/pre ✅ Success! The offer went through successfully."
	}
	return chat.SendMessage(ctx, info.PartnerSteamID, msg)
}

func SendAcceptEscrow(ctx context.Context, info *TradeInfo, chat ChatProvider, cfg ConfigProvider) error {
	msg := cfg.GetCustomMessage("successEscrow")
	if msg == "" {
		msg = "✅ Success! The offer has gone through successfully, but you will receive your items after several days. " +
			"To prevent this from happening in the future, please enable Steam Guard Mobile Authenticator.\nRead:\n" +
			"• Steam Guard Mobile Authenticator - https://support.steampowered.com/kb_article.php?ref=8625-WRAH-9030\n" +
			"• How to set up the Steam Guard Mobile Authenticator - https://support.steampowered.com/kb_article.php?ref=4440-RTUI-9218"
	}
	return chat.SendMessage(ctx, info.PartnerSteamID, msg)
}

func SendInvalid(ctx context.Context, info *TradeInfo, chat ChatProvider, cfg ConfigProvider) error {
	msg := cfg.GetCustomMessage("tradedAway")
	if msg == "" {
		msg = "/pre ❌ Ohh nooooes! Your offer is no longer available. Reason: Items not available (traded away in a different trade)."
	}
	return chat.SendMessage(ctx, info.PartnerSteamID, msg)
}

func SendCancelled(ctx context.Context, info *TradeInfo, chat ChatProvider, cfg ConfigProvider) error {
	var msg string

	if info.IsCanceledByUser {
		msg = cfg.GetCustomMessage("successCancel")
		if msg == "" {
			msg = "/pre ❌ Ohh nooooes! The offer is no longer available. Reason: Offer was canceled by user."
		}
	} else if info.OldState == StateCreatedNeedsConfirmation {
		msg = cfg.GetCustomMessage("failedMobileConfirmation")
		if msg == "" {
			msg = "/pre ❌ Ohh nooooes! The offer is no longer available. Reason: Failed to accept mobile confirmation"
		}
	} else {
		msg = cfg.GetCustomMessage("cancelledActiveForAwhile")
		if msg == "" {
			msg = "/pre ❌ Ohh nooooes! The offer is no longer available. Reason: The offer has been active for a while. " +
				"If the offer was just created, this is likely an issue on Steam's end. Please try again"
		}
	}

	return chat.SendMessage(ctx, info.PartnerSteamID, msg)
}

func SendDeclined(ctx context.Context, info *TradeInfo, chat ChatProvider, cfg ConfigProvider) error {
	prefix := cfg.GetCommandPrefix()
	baseDeclineMsg := "/pre ❌ Ohh nooooes! The offer is no longer available. Reason: The offer has been declined"

	var reply string
	var reasonForInvalidValue bool

	switch info.ReasonType {
	case "":
		reply = getOrDefault(cfg, "decline.general", baseDeclineMsg+".")

	case reason.DeclineNonTF2:
		reply = getOrDefault(cfg, "decline.hasNonTF2Items", baseDeclineMsg+" because the offer you've sent contains Non-TF2 items.")

	case reason.ReviewBannedCheckFailed:
		reply = getOrDefault(cfg, "decline.giftFailedCheckBanned", baseDeclineMsg+" because the offer you've sent is a gift, but I've failed to check your reputation status.")

	case reason.DeclineGiftNoNote:
		reply = getOrDefault(cfg, "decline.giftNoNote", baseDeclineMsg+" because the offer you've sent is an empty offer on my side without any offer message. If you wish to give it as a gift, please include \"gift\" in the offer message. Thank you.")

	case reason.DeclineCrimeAttempt:
		reply = getOrDefault(cfg, "decline.crimeAttempt", baseDeclineMsg+" because you're attempting to take items for free.")

	case reason.DeclineIntentBuy:
		reply = getOrDefault(cfg, "decline.takingItemsWithIntentBuy", baseDeclineMsg+" because you're attempting to take items that I only want to buy.")

	case reason.DeclineIntentSell:
		reply = getOrDefault(cfg, "decline.givingItemsWithIntentSell", baseDeclineMsg+" because you're attempting to give items that I only want to sell.")

	case reason.DeclineOnlyMetal:
		reply = getOrDefault(cfg, "decline.onlyMetal", baseDeclineMsg+" because you might forgot to add items into the trade.")

	case reason.DeclineDuelingUses:
		reply = getOrDefault(cfg, "decline.duelingNot5Uses", baseDeclineMsg+" because your offer contains Dueling Mini-Game(s) that does not have 5 uses.")

	case reason.DeclineNoisemakerUses:
		reply = getOrDefault(cfg, "decline.noiseMakerNot25Uses", baseDeclineMsg+" because your offer contains Noise Maker(s) that does not have 25 uses.")

	case reason.DeclineHighValueNotSell:
		custom := cfg.GetCustomMessage("decline.highValueItemsNotSelling")
		highValueName := strings.Join(info.HighValueNames, ", ")
		if custom != "" {
			reply = strings.ReplaceAll(custom, "%highValueName%", highValueName)
		} else {
			reply = baseDeclineMsg + fmt.Sprintf(" because you're attempting to purchase %s, but I am not selling it right now.", highValueName)
		}

	case reason.DeclineNotTradingKeys:
		defMsg := fmt.Sprintf("%s because I am no longer trading keys. You can confirm this by typing \"%sprice Mann Co. Supply Crate Key\" or \"%sautokeys\".", baseDeclineMsg, prefix, prefix)
		reply = getOrDefault(cfg, "decline.notTradingKeys", defMsg)

	case reason.DeclineNotSellingKeys:
		defMsg := fmt.Sprintf("%s because I am no longer selling keys. You can confirm this by typing \"%sprice Mann Co. Supply Crate Key\" or \"%sautokeys\".", baseDeclineMsg, prefix, prefix)
		reply = getOrDefault(cfg, "decline.notSellingKeys", defMsg)

	case reason.DeclineNotBuyingKeys:
		defMsg := fmt.Sprintf("%s because I am no longer buying keys. You can confirm this by typing \"%sprice Mann Co. Supply Crate Key\" or \"%sautokeys\".", baseDeclineMsg, prefix, prefix)
		reply = getOrDefault(cfg, "decline.notBuyingKeys", defMsg)

	case reason.DeclineBanned:
		var checks []string
		i := 1
		for website, status := range info.BannedStatus {
			if status != "clean" {
				checks = append(checks, fmt.Sprintf("(%d) %s: %s", i, website, status))
				i++
			}
		}
		custom := cfg.GetCustomMessage("decline.banned")
		checkStr := ""
		if len(checks) > 0 {
			checkStr = "\nCheck results:\n" + strings.Join(checks, "\n")
		}
		if custom != "" {
			reply = custom + checkStr
		} else {
			reply = baseDeclineMsg + " because you're currently banned in one or more trading communities.\n\n" + checkStr
		}

	case reason.DeclineEscrow:
		reply = getOrDefault(cfg, "decline.escrow", baseDeclineMsg+" because I do not accept escrow (trade holds). To prevent this from happening in the future, please enable Steam Guard Mobile Authenticator.\nRead:\n• Steam Guard Mobile Authenticator - https://support.steampowered.com/kb_article.php?ref=8625-WRAH-9030\n• How to set up Steam Guard Mobile Authenticator - https://support.steampowered.com/kb_article.php?ref=4440-RTUI-9218")

	case reason.DeclineManual:
		reply = getOrDefault(cfg, "decline.manual", baseDeclineMsg+" by the owner.")

	case reason.DeclineCounterInvalidValue:
		reasonForInvalidValue = true
		reply = getOrDefault(cfg, "decline.failedToCounter", baseDeclineMsg+". Counteroffer is not possible because either one of us does not have enough pure, or Steam might be down, or your inventory is private (failed to load your inventory).")

	case "ONLY_INVALID_VALUE", reason.ReviewInvalidValue:
		if info.ReasonType == "ONLY_INVALID_VALUE" || info.ManualReviewDisabled {
			reasonForInvalidValue = true
			reply = getOrDefault(cfg, "offerReceived.invalidValue.autoDecline.declineReply", baseDeclineMsg+" because you've sent a trade with an invalid value (your side and my side do not hold equal value).")
		}

	case "ONLY_INVALID_ITEMS", reason.ReviewInvalidItems:
		if info.ReasonType == "ONLY_INVALID_ITEMS" || info.ManualReviewDisabled {
			reasonForInvalidValue = info.DiffValueRef < 0
			reply = getOrDefault(cfg, "offerReceived.invalidItems.autoDecline.declineReply", baseDeclineMsg+" because you've sent a trade with an invalid items (not exist in my pricelist).")
		}

	case "ONLY_DISABLED_ITEMS", reason.ReviewDisabledItems:
		if info.ReasonType == "ONLY_DISABLED_ITEMS" || info.ManualReviewDisabled {
			reasonForInvalidValue = info.DiffValueRef < 0
			reply = getOrDefault(cfg, "offerReceived.disabledItems.autoDecline.declineReply", baseDeclineMsg+" because the item(s) you're trying to take/give is currently disabled")
		}

	case "ONLY_OVERSTOCKED", reason.ReviewOverstocked:
		if info.ReasonType == "ONLY_OVERSTOCKED" || info.ManualReviewDisabled {
			reasonForInvalidValue = info.DiffValueRef < 0
			reply = getOrDefault(cfg, "offerReceived.overstocked.autoDecline.declineReply", baseDeclineMsg+" because you're attempting to sell item(s) that I can't buy more of.")
		}

	case "ONLY_UNDERSTOCKED", reason.ReviewUnderstocked:
		if info.ReasonType == "ONLY_UNDERSTOCKED" || info.ManualReviewDisabled {
			reasonForInvalidValue = info.DiffValueRef < 0
			reply = getOrDefault(cfg, "offerReceived.understocked.autoDecline.declineReply", baseDeclineMsg+" because you're attempting to purchase item(s) that I can't sell more of.")
		}

	case reason.ReviewDupedItems:
		reply = getOrDefault(cfg, "offerReceived.duped.autoDecline.declineReply", baseDeclineMsg+" because I don't accept duped items.")

	case reason.DeclineHalted:
		reply = getOrDefault(cfg, "decline.halted", baseDeclineMsg+" because I am not operational right now. Please come back later.")

	case reason.DeclineKeysOnBothSides:
		reply = getOrDefault(cfg, "decline.containsKeysOnBothSides", baseDeclineMsg+" because the offer sent contains Mann Co. Supply Crate Key on both sides.")

	case reason.DeclineItemsOnBothSides:
		reply = getOrDefault(cfg, "decline.containsItemsOnBothSides", baseDeclineMsg+" because the offer sent contains items on both sides.")

	default:
		reply = getOrDefault(cfg, "decline.general", baseDeclineMsg+".")
	}

	if reasonForInvalidValue {
		// summary := summarizeToChat(...)
		missingStr := fmt.Sprintf("\n[You're missing: %.2f ref]", info.DiffValueRef)
		if info.DiffValueRef > info.SellKeyPriceRef {
			missingStr = fmt.Sprintf("\n[You're missing: %s]", info.DiffValueKey)
		}

		reply += "\n" + missingStr
	}

	return chat.SendMessage(ctx, info.PartnerSteamID, reply)
}

func getOrDefault(cfg ConfigProvider, key string, defaultMsg string) string {
	val := cfg.GetCustomMessage(key)
	if val != "" {
		return val
	}
	return defaultMsg
}
