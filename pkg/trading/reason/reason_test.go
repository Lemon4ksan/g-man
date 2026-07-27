// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package reason

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTradeReason_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		reason TradeReason
		want   string
	}{
		{
			name:   "decline_manual",
			reason: DeclineManual,
			want:   "MANUAL",
		},
		{
			name:   "accept_donation",
			reason: AcceptDonation,
			want:   "DONATION",
		},
		{
			name:   "review_overstocked",
			reason: ReviewOverstocked,
			want:   "🟦_OVERSTOCKED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.reason.String())
		})
	}
}
