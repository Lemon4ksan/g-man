// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trading

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

var (
	rxTheirEscrow = regexp.MustCompile(`g_DaysTheirEscrow\s*=\s*(\d+);`)
	rxMyEscrow    = regexp.MustCompile(`g_DaysMyEscrow\s*=\s*(\d+);`)

	ErrCommunityNotReady = errors.New("trading: community client is not ready (bot not logged in)")
	ErrEscrowNotFound    = errors.New("trading: escrow data not found on the page (Steam might be down or offer is invalid)")
)

// EscrowDetails contains information about the trade delay (in days).
type EscrowDetails struct {
	MyDays    int
	TheirDays int
}

// HasHold returns true if either side has a hold.
func (e EscrowDetails) HasHold() bool {
	return e.MyDays > 0 || e.TheirDays > 0
}

// GetEscrowDuration loads the trade page and parses the Trade Hold information.
func (m *Manager) GetEscrowDuration(ctx context.Context, offerID uint64) (EscrowDetails, error) {
	m.mu.RLock()
	community := m.community
	m.mu.RUnlock()

	if community == nil {
		return EscrowDetails{}, ErrCommunityNotReady
	}

	resp, err := community.Get(ctx, fmt.Sprintf("tradeoffer/%d/", offerID))
	if err != nil {
		return EscrowDetails{}, fmt.Errorf("failed to fetch offer page: %w", err)
	}

	html := string(resp.Body)

	theirMatches := rxTheirEscrow.FindStringSubmatch(html)
	myMatches := rxMyEscrow.FindStringSubmatch(html)

	if len(theirMatches) < 2 || len(myMatches) < 2 {
		return EscrowDetails{}, ErrEscrowNotFound
	}

	theirDays, _ := strconv.Atoi(theirMatches[1])
	myDays, _ := strconv.Atoi(myMatches[1])

	return EscrowDetails{
		TheirDays: theirDays,
		MyDays:    myDays,
	}, nil
}
