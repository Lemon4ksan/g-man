// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package heap provides thread-safe priority queues for trade offer prioritization.
package heap

import (
	"sync"

	"github.com/lemon4ksan/g-man/pkg/trading"
)

// PriorityQueue wraps a generic min-heap ordered by update timestamp.
//
// Thread Safety:
//   - Safe for concurrent use across goroutines.
type PriorityQueue struct {
	mu    sync.Mutex
	items []*trading.TradeOffer
}

// NewPriorityQueue constructs a PriorityQueue pre-allocated for 64 entries.
func NewPriorityQueue() *PriorityQueue {
	return &PriorityQueue{
		items: make([]*trading.TradeOffer, 0, 64),
	}
}

// Push adds an offer to the queue.
func (pq *PriorityQueue) Push(off *trading.TradeOffer) {
	if off == nil {
		return
	}

	pq.mu.Lock()
	pq.items = append(pq.items, off)
	pq.siftUp(len(pq.items) - 1)
	pq.mu.Unlock()
}

// Pop removes and returns the oldest updated trade offer, or nil if empty.
func (pq *PriorityQueue) Pop() *trading.TradeOffer {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	return pq.popLocked()
}

func (pq *PriorityQueue) popLocked() *trading.TradeOffer {
	n := len(pq.items)
	if n == 0 {
		return nil
	}

	root := pq.items[0]
	last := pq.items[n-1]
	pq.items[0] = last
	pq.items[n-1] = nil
	pq.items = pq.items[:n-1]

	if len(pq.items) > 1 {
		pq.siftDown(0)
	}

	return root
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

		pq.popLocked()
	}

	return nil
}

// Len returns current number of items in the queue.
func (pq *PriorityQueue) Len() int {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	return len(pq.items)
}

func (pq *PriorityQueue) siftUp(i int) {
	for i > 0 {
		parent := (i - 1) / 2
		if pq.items[i].TimeUpdated >= pq.items[parent].TimeUpdated {
			break
		}

		pq.items[i], pq.items[parent] = pq.items[parent], pq.items[i]
		i = parent
	}
}

func (pq *PriorityQueue) siftDown(i int) {
	n := len(pq.items)
	for {
		left := 2*i + 1
		if left >= n || left < 0 {
			break
		}

		smallest := left
		if right := left + 1; right < n && pq.items[right].TimeUpdated < pq.items[left].TimeUpdated {
			smallest = right
		}

		if pq.items[i].TimeUpdated <= pq.items[smallest].TimeUpdated {
			break
		}

		pq.items[i], pq.items[smallest] = pq.items[smallest], pq.items[i]
		i = smallest
	}
}
