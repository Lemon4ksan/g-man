// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package backpack

import "github.com/lemon4ksan/g-man/pkg/bus"

// FullEvent is published when the backpack reaches its maximum capacity.
type FullEvent struct {
	bus.BaseEvent
	Count, Max int
}

// Topic returns the topic for the event.
func (b FullEvent) Topic() string {
	return "backpack.full"
}
