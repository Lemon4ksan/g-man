// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package web

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/lemon4ksan/aoni/mod"

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

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, body); err != nil {
		return processor.Details{}, err
	}

	theirMatches := processor.RxTheir.FindStringSubmatch(buf.String())
	myMatches := processor.RxMy.FindStringSubmatch(buf.String())

	if len(theirMatches) < 2 || len(myMatches) < 2 {
		return processor.Details{}, ErrEscrowNotFound
	}

	theirDays, _ := strconv.Atoi(theirMatches[1])
	myDays, _ := strconv.Atoi(myMatches[1])

	return processor.Details{
		TheirDays: theirDays,
		MyDays:    myDays,
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
