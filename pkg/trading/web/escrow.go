// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package web

import (
	"context"
	"fmt"
	"io"
	"net/url"

	"github.com/lemon4ksan/aoni-contrib/extract"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"

	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/trading"
	"github.com/lemon4ksan/g-man/pkg/trading/web/processor"
)

func parseEscrowFromHTML(bodyBytes []byte) (processor.Details, error) {
	theirRaw, err1 := extract.Between(bodyBytes, "g_daysTheirEscrow = ", ";")
	if err1 != nil {
		theirRaw, err1 = extract.Between(bodyBytes, "g_DaysTheirEscrow = ", ";")
	}

	myRaw, err2 := extract.Between(bodyBytes, "g_daysMyEscrow = ", ";")
	if err2 != nil {
		myRaw, err2 = extract.Between(bodyBytes, "g_DaysMyEscrow = ", ";")
	}

	if err1 != nil || err2 != nil {
		theirRaw, err1 = extract.Regex(bodyBytes, `(?i)g_daysTheirEscrow\s*=\s*(\d+)\s*;`)
		myRaw, err2 = extract.Regex(bodyBytes, `(?i)g_daysMyEscrow\s*=\s*(\d+)\s*;`)
	}

	if err1 != nil || err2 != nil {
		return processor.Details{}, ErrEscrowNotFound
	}

	theirDaysVal, _ := bytesconv.ParseUintFast(theirRaw)
	myDaysVal, _ := bytesconv.ParseUintFast(myRaw)

	return processor.Details{
		TheirDays: int(theirDaysVal),
		MyDays:    int(myDaysVal),
	}, nil
}

// ParseEscrowFromHTML extracts escrow hold days for both parties from trade offer page HTML.
//
// Invariant: Matches @tf2autobot/tradeoffer-manager where Valve HTML fluctuates between
// lowercase "g_daysTheirEscrow" and uppercase "g_DaysTheirEscrow". Lowercase is evaluated first.
//
// Parity: matches @tf2autobot/tradeoffer-manager (lib/classes/TradeOffer.js: getUserDetails).
func ParseEscrowFromHTML(bodyBytes []byte) (processor.Details, error) {
	return parseEscrowFromHTML(bodyBytes)
}

// GetEscrowDuration parses escrow hold days from the trade offer web page.
//
// Parity: matches @tf2autobot/tradeoffer-manager (lib/index.js: TradeOfferManager.prototype.getEscrowDuration).
// Valve historically fluctuates between lowercase "g_daysTheirEscrow" and uppercase "g_DaysTheirEscrow"
// in Steam Community HTML responses. The between extractions and regex fallback handle both cases.
func (m *Manager) GetEscrowDuration(ctx context.Context, offerID uint64) (processor.Details, error) {
	comm := m.Community()
	if comm == nil {
		return processor.Details{}, ErrCommunityNotReady
	}

	body, err := community.GetHTML(
		ctx, comm, "tradeoffer/{offerID}/",
		mod.WithVar("offerID", offerID),
	)
	if err != nil {
		return processor.Details{}, fmt.Errorf("failed to fetch offer page: %w", err)
	}

	defer body.Close()

	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		return processor.Details{}, err
	}

	return parseEscrowFromHTML(bodyBytes)
}

// GetEscrowDurationForPartner queries the pre-trade page (tradeoffer/new/?partner=...&token=...)
// to determine escrow hold durations before sending an offer.
//
// Parity: matches @tf2autobot/tradeoffer-manager (lib/classes/TradeOffer.js: getUserDetails).
func (m *Manager) GetEscrowDurationForPartner(
	ctx context.Context,
	partnerID id.ID,
	token string,
) (processor.Details, error) {
	comm := m.Community()
	if comm == nil {
		return processor.Details{}, ErrCommunityNotReady
	}

	normPartner, err := NormalizePartnerSteamID(partnerID)
	if err != nil {
		return processor.Details{}, err
	}

	endpoint := fmt.Sprintf("tradeoffer/new/?partner=%d", normPartner.AccountID())
	if token != "" {
		endpoint += "&token=" + url.QueryEscape(token)
	}

	body, err := community.GetHTML(ctx, comm, endpoint)
	if err != nil {
		return processor.Details{}, fmt.Errorf("failed to fetch pre-trade page: %w", err)
	}

	defer body.Close()

	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		return processor.Details{}, err
	}

	return parseEscrowFromHTML(bodyBytes)
}

// CheckEscrow verifies whether the trade offer has an escrow hold duration.
//
// For incoming offers:
//   - Fast path: Checks whether offer.State is OfferStateInEscrow or offer.EscrowEndDate > 0.
//   - Fallback: Scrapes "tradeoffer/{offerID}/".
//
// For outgoing offers (offer.IsOurOffer == true):
//   - Fast path: Checks whether offer.State is OfferStateInEscrow or offer.EscrowEndDate > 0.
//   - Pre-trade / active outgoing check: Scrapes "tradeoffer/new/?partner={partnerID.AccountID()}"
//     to inspect escrow holds with the partner.
//
// Parity: matches @tf2autobot/tradeoffer-manager (lib/classes/TradeOffer.js: getUserDetails).
// Parity improvement: enables escrow duration inspection for sent offers by querying the pre-trade URL.
func (m *Manager) CheckEscrow(ctx context.Context, offer *trading.TradeOffer) (bool, error) {
	if offer == nil {
		return false, nil
	}

	if offer.State == trading.OfferStateInEscrow || offer.EscrowEndDate > 0 {
		return true, nil
	}

	if offer.IsOurOffer {
		if offer.OtherSteamID != 0 {
			details, err := m.GetEscrowDurationForPartner(ctx, offer.OtherSteamID, "")
			if err != nil {
				return false, err
			}

			return details.HasHold(), nil
		}

		return false, nil
	}

	details, err := m.GetEscrowDuration(ctx, offer.ID)
	if err != nil {
		if offer.OtherSteamID != 0 {
			if partnerDetails, partnerErr := m.GetEscrowDurationForPartner(
				ctx,
				offer.OtherSteamID,
				"",
			); partnerErr == nil {
				return partnerDetails.HasHold(), nil
			}
		}

		return false, err
	}

	return details.HasHold(), nil
}
