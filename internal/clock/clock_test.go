// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clock

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSystemClock_Now(t *testing.T) {
	t.Parallel()

	sysClock := SystemClock{}
	before := time.Now()
	now := sysClock.Now()
	after := time.Now()

	assert.True(t, !now.Before(before) && !now.After(after))
}

func TestOffsetClock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		offset time.Duration
	}{
		{
			name:   "zero_offset",
			offset: 0,
		},
		{
			name:   "positive_offset_5_minutes",
			offset: 5 * time.Minute,
		},
		{
			name:   "negative_offset_10_seconds",
			offset: -10 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			clk := new(OffsetClock)
			clk.SetOffset(tt.offset)

			expectedMin := time.Now().Add(tt.offset).Add(-1 * time.Second)
			actual := clk.Now()
			expectedMax := time.Now().Add(tt.offset).Add(1 * time.Second)

			assert.True(t, actual.After(expectedMin) && actual.Before(expectedMax))
		})
	}
}
