// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package escrow

import (
	"errors"
	"regexp"
)

var (
	RxTheir = regexp.MustCompile(`(?i)g_DaysTheirEscrow\s*=\s*(\d+);`)
	RxMy    = regexp.MustCompile(`(?i)g_DaysMyEscrow\s*=\s*(\d+);`)

	ErrCommunityNotReady = errors.New("trading: community client is not ready (bot not logged in)")
	ErrEscrowNotFound    = errors.New(
		"trading: escrow data not found on the page (Steam might be down or offer is invalid)",
	)
)

// Details contains information about the trade delay (in days).
type Details struct {
	MyDays    int
	TheirDays int
}

// HasHold returns true if either side has a hold.
func (e Details) HasHold() bool {
	return e.MyDays > 0 || e.TheirDays > 0
}
