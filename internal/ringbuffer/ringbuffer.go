// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package ringbuffer provides a lock-free ring buffer for ultra-fast message passing between goroutines.
package ringbuffer

import (
	"sync/atomic"

	"golang.org/x/sys/cpu"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
)

type slot struct {
	sequence uint64
	msg      *protocol.InboundMessage
}

// MPMCRingBuffer is a lock-free ring buffer for ultra-fast message passing between goroutines.
type MPMCRingBuffer struct {
	buffer []slot
	mask   uint64

	_    cpu.CacheLinePad
	head uint64
	_    cpu.CacheLinePad
	tail uint64
	_    cpu.CacheLinePad
}

// New creates a new MPMCRingBuffer with the specified capacity.
func New(capacity uint64) *MPMCRingBuffer {
	var cap uint64 = 1
	for cap < capacity {
		cap <<= 1
	}

	rb := &MPMCRingBuffer{
		buffer: make([]slot, cap),
		mask:   cap - 1,
	}

	for i := uint64(0); i < cap; i++ {
		rb.buffer[i].sequence = i
	}

	return rb
}

// Push adds a message to the buffer without blocking.
func (rb *MPMCRingBuffer) Push(msg *protocol.InboundMessage) bool {
	var cell *slot

	pos := atomic.LoadUint64(&rb.head)

	for {
		cell = &rb.buffer[pos&rb.mask]
		seq := atomic.LoadUint64(&cell.sequence)
		dif := int64(seq) - int64(pos)

		if dif == 0 && atomic.CompareAndSwapUint64(&rb.head, pos, pos+1) {
			break
		}

		if dif < 0 {
			return false
		}

		pos = atomic.LoadUint64(&rb.head)
	}

	// Assign message data before publishing the updated sequence number
	cell.msg = msg
	atomic.StoreUint64(&cell.sequence, pos+1)

	return true
}

// Pop removes a message from the buffer without blocking.
func (rb *MPMCRingBuffer) Pop() (*protocol.InboundMessage, bool) {
	var cell *slot

	pos := atomic.LoadUint64(&rb.tail)

	for {
		cell = &rb.buffer[pos&rb.mask]
		seq := atomic.LoadUint64(&cell.sequence)
		dif := int64(seq) - int64(pos+1)

		if dif == 0 && atomic.CompareAndSwapUint64(&rb.tail, pos, pos+1) {
			break
		}

		if dif < 0 {
			return nil, false
		}

		pos = atomic.LoadUint64(&rb.tail)
	}

	msg := cell.msg
	cell.msg = nil
	atomic.StoreUint64(&cell.sequence, pos+rb.mask+1)

	return msg, true
}
