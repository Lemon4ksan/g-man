// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package heap provides thread-safe priority queues for trade offer prioritization.
package heap

import (
	"sync"

	"github.com/lemon4ksan/g-man/pkg/trading"
)

// PriorityQueue wraps an indexed min-heap ordered by update timestamp.
//
// Thread Safety:
//   - Safe for concurrent use across goroutines.
type PriorityQueue struct {
	mu    sync.Mutex
	items []*trading.TradeOffer
	index map[uint64]int // offerID -> index in items
}

// NewPriorityQueue constructs a PriorityQueue pre-allocated for 64 entries.
func NewPriorityQueue() *PriorityQueue {
	return &PriorityQueue{
		items: make([]*trading.TradeOffer, 0, 64),
		index: make(map[uint64]int, 64),
	}
}

// Push adds an offer to the queue or updates it in-place if already present.
func (pq *PriorityQueue) Push(off *trading.TradeOffer) {
	if off == nil {
		return
	}

	pq.mu.Lock()
	defer pq.mu.Unlock()

	if idx, exists := pq.index[off.ID]; exists {
		pq.items[idx] = off
		if idx > 0 && pq.items[idx].TimeUpdated < pq.items[(idx-1)/2].TimeUpdated {
			pq.siftUp(idx)
		} else {
			pq.siftDown(idx)
		}
		return
	}

	idx := len(pq.items)
	pq.items = append(pq.items, off)
	pq.index[off.ID] = idx
	pq.siftUp(idx)
}

// Remove deletes an offer by its ID from the priority queue in O(log N) time.
// Returns true if the offer was found and removed, false otherwise.
func (pq *PriorityQueue) Remove(offerID uint64) bool {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	idx, exists := pq.index[offerID]
	if !exists {
		return false
	}

	pq.removeLocked(idx)
	return true
}

// Has checks whether an offer ID exists in the queue.
func (pq *PriorityQueue) Has(offerID uint64) bool {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	_, exists := pq.index[offerID]
	return exists
}

// Pop removes and returns the oldest updated trade offer, or nil if empty.
func (pq *PriorityQueue) Pop() *trading.TradeOffer {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	return pq.popLocked()
}

func (pq *PriorityQueue) popLocked() *trading.TradeOffer {
	if len(pq.items) == 0 {
		return nil
	}

	return pq.removeLocked(0)
}

func (pq *PriorityQueue) removeLocked(i int) *trading.TradeOffer {
	n := len(pq.items)
	if i < 0 || i >= n {
		return nil
	}

	target := pq.items[i]
	delete(pq.index, target.ID)

	n--
	if i == n {
		pq.items[n] = nil
		pq.items = pq.items[:n]
		return target
	}

	last := pq.items[n]
	pq.items[i] = last
	pq.index[last.ID] = i
	pq.items[n] = nil
	pq.items = pq.items[:n]

	if i < len(pq.items) {
		if i > 0 && pq.items[i].TimeUpdated < pq.items[(i-1)/2].TimeUpdated {
			pq.siftUp(i)
		} else {
			pq.siftDown(i)
		}
	}

	return target
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

func (pq *PriorityQueue) swap(i, j int) {
	pq.items[i], pq.items[j] = pq.items[j], pq.items[i]
	pq.index[pq.items[i].ID] = i
	pq.index[pq.items[j].ID] = j
}

func (pq *PriorityQueue) siftUp(i int) {
	for i > 0 {
		parent := (i - 1) / 2
		if pq.items[i].TimeUpdated >= pq.items[parent].TimeUpdated {
			break
		}

		pq.swap(i, parent)
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

		pq.swap(i, smallest)
		i = smallest
	}
}
