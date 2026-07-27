// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ringbuffer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
)

func TestNew_RoundsCapacityToPowerOfTwo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		requested    uint64
		expectedMask uint64
	}{
		{
			name:         "exact_power_of_two",
			requested:    16,
			expectedMask: 15,
		},
		{
			name:         "non_power_of_two_rounds_up",
			requested:    10,
			expectedMask: 15, // rounds up to 16, mask = 15
		},
		{
			name:         "large_non_power_of_two",
			requested:    1000,
			expectedMask: 1023, // rounds up to 1024, mask = 1023
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rb := New(tt.requested)
			require.NotNil(t, rb)
			assert.Equal(t, tt.expectedMask, rb.mask)
		})
	}
}

func TestMPMCRingBuffer_PushPop(t *testing.T) {
	t.Parallel()

	t.Run("push_pop_fifo_order", func(t *testing.T) {
		t.Parallel()

		rb := New(4)
		msg1 := &protocol.InboundMessage{}
		msg2 := &protocol.InboundMessage{}

		assert.True(t, rb.Push(msg1))
		assert.True(t, rb.Push(msg2))

		popped1, ok1 := rb.Pop()
		assert.True(t, ok1)
		assert.Same(t, msg1, popped1)

		popped2, ok2 := rb.Pop()
		assert.True(t, ok2)
		assert.Same(t, msg2, popped2)
	})

	t.Run("pop_empty_buffer_returns_false", func(t *testing.T) {
		t.Parallel()

		rb := New(4)
		msg, ok := rb.Pop()
		assert.False(t, ok)
		assert.Nil(t, msg)
	})

	t.Run("push_overflow_returns_false", func(t *testing.T) {
		t.Parallel()

		rb := New(2)
		assert.True(t, rb.Push(&protocol.InboundMessage{}))
		assert.True(t, rb.Push(&protocol.InboundMessage{}))

		// Buffer is full (capacity 2)
		assert.False(t, rb.Push(&protocol.InboundMessage{}))

		// Pop one and push again
		_, ok := rb.Pop()
		assert.True(t, ok)
		assert.True(t, rb.Push(&protocol.InboundMessage{}))
	})
}
