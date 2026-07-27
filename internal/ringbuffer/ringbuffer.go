// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package ringbuffer provides a lock-free multi-producer multi-consumer (MPMC) bounded ring buffer.
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

// MPMCRingBuffer implements a lock-free bounded queue optimized for high-throughput packet passing between goroutines without mutex contention.
//
// Thread Safety:
//   - Fully thread-safe and lock-free across concurrent producers and consumers.
type MPMCRingBuffer struct {
	buffer []slot
	mask   uint64

	_    cpu.CacheLinePad
	head uint64
	_    cpu.CacheLinePad
	tail uint64
	_    cpu.CacheLinePad
}

// New creates an MPMCRingBuffer with a capacity rounded up to the nearest power-of-two boundary.
func New(capacity uint64) *MPMCRingBuffer {
	var cap uint64 = 1
	for cap < capacity {
		cap <<= 1
	}

	rb := &MPMCRingBuffer{
		buffer: make([]slot, cap),
		mask:   cap - 1,
	}

	for i := range cap {
		rb.buffer[i].sequence = i
	}

	return rb
}

// Push enqueues a message without blocking or memory allocations.
//
// Returns:
//   - true if the message was enqueued.
//   - false if the buffer is full.
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

	cell.msg = msg
	atomic.StoreUint64(&cell.sequence, pos+1)

	return true
}

// Pop dequeues the next message without blocking or memory allocations.
//
// Returns:
//   - (msg, true) if a message was dequeued.
//   - (nil, false) if the buffer is empty.
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
