// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package heap

import (
	"sync"
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

func TestPriorityQueue_Push_Deduplication_100Times(t *testing.T) {
	t.Parallel()

	pq := NewPriorityQueue()
	offer := &trading.TradeOffer{
		ID:          12345,
		TimeUpdated: 1000,
		State:       trading.OfferStateActive,
	}

	for i := 0; i < 100; i++ {
		pq.Push(offer)
	}

	assert.Equal(t, 1, pq.Len(), "queue length must be 1 after 100 pushes of the same offer")
	assert.True(t, pq.Has(12345))

	popped := pq.Pop()
	assert.NotNil(t, popped)
	assert.Equal(t, uint64(12345), popped.ID)
	assert.Equal(t, 0, pq.Len())
	assert.False(t, pq.Has(12345))
}

func TestPriorityQueue_Push_UpdateInPlace_TimestampAdjustments(t *testing.T) {
	t.Parallel()

	pq := NewPriorityQueue()

	pq.Push(&trading.TradeOffer{ID: 1, TimeUpdated: 200})
	pq.Push(&trading.TradeOffer{ID: 2, TimeUpdated: 300})
	pq.Push(&trading.TradeOffer{ID: 3, TimeUpdated: 400})

	assert.Equal(t, 3, pq.Len())
	assert.Equal(t, uint64(1), pq.Peek(func(*trading.TradeOffer) bool { return true }).ID)

	// Update ID 1 with newer timestamp (t=500). ID 2 (t=300) must become root.
	pq.Push(&trading.TradeOffer{ID: 1, TimeUpdated: 500})
	assert.Equal(t, 3, pq.Len())
	assert.Equal(t, uint64(2), pq.Peek(func(*trading.TradeOffer) bool { return true }).ID)

	// Update ID 3 with older timestamp (t=100). ID 3 must sift up to root.
	pq.Push(&trading.TradeOffer{ID: 3, TimeUpdated: 100})
	assert.Equal(t, 3, pq.Len())
	assert.Equal(t, uint64(3), pq.Peek(func(*trading.TradeOffer) bool { return true }).ID)
}

func TestPriorityQueue_Remove(t *testing.T) {
	t.Parallel()

	pq := NewPriorityQueue()
	offers := []*trading.TradeOffer{
		{ID: 10, TimeUpdated: 100},
		{ID: 20, TimeUpdated: 200},
		{ID: 30, TimeUpdated: 300},
		{ID: 40, TimeUpdated: 400},
		{ID: 50, TimeUpdated: 500},
	}
	for _, o := range offers {
		pq.Push(o)
	}

	assert.True(t, pq.Remove(30))
	assert.Equal(t, 4, pq.Len())
	assert.False(t, pq.Has(30))
	assert.False(t, pq.Remove(30))
	assert.False(t, pq.Remove(999))

	// Remove root
	assert.True(t, pq.Remove(10))
	assert.Equal(t, 3, pq.Len())
	assert.Equal(t, uint64(20), pq.Peek(func(*trading.TradeOffer) bool { return true }).ID)
}

func TestPriorityQueue_Concurrent_Deduplication(t *testing.T) {
	t.Parallel()

	pq := NewPriorityQueue()
	const (
		goroutines = 25
		offers     = 10
		iterations = 100
	)

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(gid int) {
			defer wg.Done()
			for iter := 0; iter < iterations; iter++ {
				for id := 1; id <= offers; id++ {
					pq.Push(&trading.TradeOffer{
						ID:          uint64(id),
						TimeUpdated: int64(iter*10 + id),
					})
				}
			}
		}(g)
	}

	wg.Wait()

	assert.Equal(t, offers, pq.Len(), "queue must contain exactly 10 items after high concurrency")
	for id := 1; id <= offers; id++ {
		assert.True(t, pq.Has(uint64(id)))
	}
}

func TestPriorityQueue_Peek_LazyPruning_CleansIndex(t *testing.T) {
	t.Parallel()

	pq := NewPriorityQueue()
	pq.Push(&trading.TradeOffer{ID: 1, TimeUpdated: 100})
	pq.Push(&trading.TradeOffer{ID: 2, TimeUpdated: 200})
	pq.Push(&trading.TradeOffer{ID: 3, TimeUpdated: 300})

	// Reject ID 1 and 2
	valid := pq.Peek(func(off *trading.TradeOffer) bool {
		return off.ID == 3
	})

	assert.NotNil(t, valid)
	assert.Equal(t, uint64(3), valid.ID)
	assert.Equal(t, 1, pq.Len())
	assert.False(t, pq.Has(1))
	assert.False(t, pq.Has(2))
	assert.True(t, pq.Has(3))
}

func TestPriorityQueue_Push_InPlacePointerMutationHeapInvariant(t *testing.T) {
	t.Parallel()

	pq := NewPriorityQueue()

	off1 := &trading.TradeOffer{ID: 1, TimeUpdated: 200}
	off2 := &trading.TradeOffer{ID: 2, TimeUpdated: 300}
	off3 := &trading.TradeOffer{ID: 3, TimeUpdated: 400}

	pq.Push(off1)
	pq.Push(off2)
	pq.Push(off3)

	assert.Equal(t, uint64(1), pq.Peek(func(*trading.TradeOffer) bool { return true }).ID)

	// Scenario 1: Mutate off3 in place to a smaller timestamp than root (400 -> 50)
	// Same pointer is modified in-place and re-pushed
	off3.TimeUpdated = 50
	pq.Push(off3)

	// off3 must now be at the root of the min-heap
	root := pq.Peek(func(*trading.TradeOffer) bool { return true })
	assert.NotNil(t, root)
	assert.Equal(t, uint64(3), root.ID)
	assert.Equal(t, int64(50), root.TimeUpdated)

	// Scenario 2: Mutate off3 (currently root) in place to a timestamp larger than all children (50 -> 500)
	// Same pointer is modified in-place and re-pushed
	off3.TimeUpdated = 500
	pq.Push(off3)

	// off1 (TimeUpdated: 200) must now sift up to become root
	root = pq.Peek(func(*trading.TradeOffer) bool { return true })
	assert.NotNil(t, root)
	assert.Equal(t, uint64(1), root.ID)
	assert.Equal(t, int64(200), root.TimeUpdated)

	// Scenario 3: Verify all items pop in strictly non-decreasing timestamp order
	popped1 := pq.Pop()
	assert.Equal(t, uint64(1), popped1.ID)
	assert.Equal(t, int64(200), popped1.TimeUpdated)

	popped2 := pq.Pop()
	assert.Equal(t, uint64(2), popped2.ID)
	assert.Equal(t, int64(300), popped2.TimeUpdated)

	popped3 := pq.Pop()
	assert.Equal(t, uint64(3), popped3.ID)
	assert.Equal(t, int64(500), popped3.TimeUpdated)

	assert.Equal(t, 0, pq.Len())
}

