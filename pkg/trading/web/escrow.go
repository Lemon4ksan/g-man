// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package web

import (
	"context"
	"fmt"
	"io"

	"github.com/lemon4ksan/aoni/codec/extract"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"

	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/trading"
	"github.com/lemon4ksan/g-man/pkg/trading/web/processor"
)

// GetEscrowDuration parses escrow hold days from the trade offer web page.
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

	theirRaw, err1 := extract.Between(bodyBytes, "g_DaysTheirEscrow = ", ";")
	myRaw, err2 := extract.Between(bodyBytes, "g_DaysMyEscrow = ", ";")

	if err1 != nil || err2 != nil {
		theirRaw, err1 = extract.Regex(bodyBytes, `(?i)g_DaysTheirEscrow\s*=\s*(\d+);`)
		myRaw, err2 = extract.Regex(bodyBytes, `(?i)g_DaysMyEscrow\s*=\s*(\d+);`)
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

// CheckEscrow verifies whether the trade offer has an escrow hold duration.
func (m *Manager) CheckEscrow(ctx context.Context, offer *trading.TradeOffer) (bool, error) {
	details, err := m.GetEscrowDuration(ctx, offer.ID)
	if err != nil {
		return false, err
	}

	return details.HasHold(), nil
}
