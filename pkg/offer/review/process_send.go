// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package review

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lemon4ksan/g-man/pkg/offer/reason"
)

// SendReview processes the offer that requires manual review and notifies the affiliate and the bot owner.
func SendReview(ctx context.Context, offerID uint64, partnerSteamID uint64, meta *Meta, schema SchemaProvider, chat ChatProvider, cfg ConfigProvider) error {
	content := ProcessReview(meta, schema, cfg)

	reasonsStr := make([]string, len(meta.UniqueReasons))
	for _, r := range meta.UniqueReasons {
		reasonsStr = append(reasonsStr, r.String())
	}
	reasons := strings.Join(reasonsStr, ", ")

	if isCriticalHalt(meta) {
		reply := getCriticalHaltReply(meta, cfg)
		_ = chat.SendMessage(ctx, partnerSteamID, reply)
	} else {
		reply := fmt.Sprintf("⚠️ Your offer is pending review.\nReasons: %s", reasonsStr)

		if len(content.Notes) > 0 {
			reply += fmt.Sprintf("\n\nNote:\n%s\n\nPlease wait for a response from the owner.", strings.Join(content.Notes, "\n"))
		}

		_ = chat.SendMessage(ctx, partnerSteamID, reply)
	}

	if cfg.IsWebhookEnabled() {
		// TODO: Implement
		return nil
	}

	// Wait 2 seconds before sending to the admin (avoid RateLimitExceeded)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(2 * time.Second):
	}

	return SendToAdmin(ctx, offerID, partnerSteamID, reasons, content, chat)
}

// SendToAdmin sends a detailed message to the bot owner in Steam Chat.
func SendToAdmin(ctx context.Context, offerID uint64, partnerSteamID uint64, reasons string, content Content, chat ChatProvider) error {
	msg1 := fmt.Sprintf("⚠️ Offer #%d from %d is pending review.\nReasons: %s", offerID, partnerSteamID, reasons)
	msg2 := "\n\nsummary..."
	msg3 := ""
	if len(content.ItemNamesOur) > 0 {
		msg3 = "\n\nItem lists:\n" + formatItemLists(content.ItemNamesOur)
	}

	msg4 := fmt.Sprintf("\n\n⚠️ Send \"!accept %d\" to accept or \"!decline %d\" to decline this offer.", offerID, offerID)

	fullMessage := msg1 + msg2 + msg3 + msg4

	if len(fullMessage) > 5000 {
		parts := []string{msg1, msg2, msg3, msg4}
		for _, part := range parts {
			if part == "" {
				continue
			}

			if err := chat.MessageAdmins(ctx, part); err != nil {
				return err
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(1500 * time.Millisecond):
			}
		}
		return nil
	}

	return chat.MessageAdmins(ctx, fullMessage)
}

// SendDeclinedAlert formats and sends the decline message.
func SendDeclinedAlert(
	ctx context.Context,
	offerID uint64,
	partnerID uint64,
	offerMessage string,
	meta *TradeMetadata,
	schema SchemaProvider,
	chat ChatProvider,
	cfg ConfigProvider,
	prices PricelistProvider,
	stats BotStatsProvider,
	autokeys AutokeysProvider,
) error {
	isWebhookEnabled := cfg.IsWebhookEnabled()
	declined := ProcessDeclined(meta, schema, isWebhookEnabled)

	if isWebhookEnabled {
		// TODO: return sendWebhook(ctx, declined, ...)
		return nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "/me Trade #%d with %d was declined. ❌\n", offerID, partnerID)
	fmt.Fprintf(&sb, "Reason: %s\n", declined.ReasonDescription)

	if offerMessage != "" {
		fmt.Fprintf(&sb, "\n💬 Offer message: \"%s\"\n", offerMessage)
	}

	itemLists := formatDeclinedItemLists(&declined)
	if itemLists != "" {
		sb.WriteString("\nItem lists:\n")
		sb.WriteString(itemLists)
	}

	buy, sell := prices.GetKeyPrices()
	fmt.Fprintf(&sb, "\n\n🔑 Key rate: %.2f/%.2f", buy, sell)

	if autokeys.IsEnabled() {
		statusStr := "🛑"
		if autokeys.IsActive() {
			statusStr = fmt.Sprintf("✅ (%s)", autokeys.GetStatus())
		}
		fmt.Fprintf(&sb, " | Autokeys: %s", statusStr)
	}

	pureKeys, pureRef := stats.GetPureStock()
	fmt.Fprintf(&sb, "\n💰 Pure stock: %.2f keys, %.2f ref", pureKeys, pureRef)
	fmt.Fprintf(&sb, "\n🎒 Total items: %d/%d", stats.GetTotalItems(), stats.GetBackpackSlots())

	processDuration := time.Duration(meta.ProcessTimeMS) * time.Millisecond
	fmt.Fprintf(&sb, "\n⏱ Time taken: %s", formatTimeTaken(processDuration.Milliseconds(), meta.IsOfferSent, true))
	fmt.Fprintf(&sb, "\n\nVersion %s", stats.GetVersion())

	return chat.MessageAdmins(ctx, sb.String())
}

func formatDeclinedItemLists(d *DeclinedSummary) string {
	var sb strings.Builder

	writeList := func(name string, items []string) {
		if len(items) > 0 {
			fmt.Fprintf(&sb, "- %s: %s\n", name, strings.Join(items, ", "))
		}
	}

	writeList("Invalid", d.InvalidItems)
	writeList("Disabled", d.DisabledItems)
	writeList("Overstock", d.Overstocked)
	writeList("Understock", d.Understocked)
	writeList("Duped", d.DupedItems)

	highValueAll := append(d.HighValue, d.HighNotSellingItems...)
	writeList("High Value", highValueAll)

	return strings.TrimSpace(sb.String())
}

func isCriticalHalt(meta *Meta) bool {
	return meta.HasReason(reason.ReviewBannedCheckFailed) ||
		meta.HasReason(reason.ReviewEscrowCheckFailed) ||
		meta.HasReason(reason.ReviewHalted) ||
		meta.HasReason(reason.ReviewReviewForced)
}

func getCriticalHaltReply(meta *Meta, cfg ConfigProvider) string {
	if meta.HasReason(reason.ReviewHalted) {
		return "❌ The bot is not operational right now, but your offer has been put to review, please wait for my owner to manually accept/decline your offer."
	}
	return "Your offer has been received and will be manually reviewed by the owner."
}

func formatItemLists(lists map[string][]string) string {
	var result strings.Builder
	for category, items := range lists {
		if len(items) > 0 {
			fmt.Fprintf(&result, "- %s: %s\n", category, strings.Join(items, ", "))
		}
	}
	return result.String()
}

func formatTimeTaken(ms int64, isOfferSent bool, showDetailed bool) string {
	d := time.Duration(ms) * time.Millisecond
	if showDetailed {
		return d.String() // "1m30s"
	}
	return fmt.Sprintf("%.2f seconds", d.Seconds())
}
