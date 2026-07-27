// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package heap

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/g-man/pkg/trading"
)

func TestPriorityQueue_PushAndPeek(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		offers        []*trading.TradeOffer
		isValidFunc   func(off *trading.TradeOffer) bool
		expectedOffer *trading.TradeOffer
	}{
		{
			name: "returns_oldest_updated_offer",
			offers: []*trading.TradeOffer{
				{ID: 1, TimeUpdated: 200},
				{ID: 2, TimeUpdated: 100},
				{ID: 3, TimeUpdated: 300},
			},
			isValidFunc: func(off *trading.TradeOffer) bool {
				return true
			},
			expectedOffer: &trading.TradeOffer{ID: 2, TimeUpdated: 100},
		},
		{
			name: "lazy_pruning_invalid_top_offers",
			offers: []*trading.TradeOffer{
				{ID: 1, TimeUpdated: 100},
				{ID: 2, TimeUpdated: 200},
				{ID: 3, TimeUpdated: 300},
			},
			isValidFunc: func(off *trading.TradeOffer) bool {
				return off.ID != 1
			},
			expectedOffer: &trading.TradeOffer{ID: 2, TimeUpdated: 200},
		},
		{
			name: "returns_nil_when_all_offers_invalid",
			offers: []*trading.TradeOffer{
				{ID: 1, TimeUpdated: 100},
				{ID: 2, TimeUpdated: 200},
			},
			isValidFunc: func(off *trading.TradeOffer) bool {
				return false
			},
			expectedOffer: nil,
		},
		{
			name:   "push_nil_offer_ignored",
			offers: []*trading.TradeOffer{nil},
			isValidFunc: func(off *trading.TradeOffer) bool {
				return true
			},
			expectedOffer: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pq := NewPriorityQueue()
			for _, offer := range tt.offers {
				pq.Push(offer)
			}

			actual := pq.Peek(tt.isValidFunc)
			assert.Equal(t, tt.expectedOffer, actual)
		})
	}
}
