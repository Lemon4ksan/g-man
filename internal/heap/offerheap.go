// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package heap provides thread-safe priority queues for trade offer prioritization.
package heap

import (
	"container/heap"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/trading"
)

type offerHeap []*trading.TradeOffer

func (h offerHeap) Len() int           { return len(h) }
func (h offerHeap) Less(i, j int) bool { return h[i].TimeUpdated < h[j].TimeUpdated }
func (h offerHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *offerHeap) Push(x any) {
	*h = append(*h, x.(*trading.TradeOffer))
}

func (h *offerHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]

	return x
}

// PriorityQueue wraps an offer min-heap ordered by update timestamp.
//
// Thread Safety:
//   - Safe for concurrent use across goroutines.
type PriorityQueue struct {
	mu    sync.Mutex
	items offerHeap
}

// NewPriorityQueue constructs a PriorityQueue pre-allocated for 64 entries.
func NewPriorityQueue() *PriorityQueue {
	pq := &PriorityQueue{items: make(offerHeap, 0, 64)}
	heap.Init(&pq.items)

	return pq
}

// Push adds an offer to the queue.
func (pq *PriorityQueue) Push(off *trading.TradeOffer) {
	if off == nil {
		return
	}

	pq.mu.Lock()
	heap.Push(&pq.items, off)
	pq.mu.Unlock()
}

// Peek inspects and returns the oldest valid offer without removing it, lazy-pruning invalid entries.
func (pq *PriorityQueue) Peek(isValid func(off *trading.TradeOffer) bool) *trading.TradeOffer {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	for len(pq.items) > 0 {
		top := pq.items[0]
		if isValid(top) {
			return top
		}

		heap.Pop(&pq.items)
	}

	return nil
}
