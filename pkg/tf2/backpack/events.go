// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package backpack

import "github.com/lemon4ksan/g-man/pkg/bus"

type BackpackFullEvent struct {
	bus.BaseEvent
	Count, Max int
}

func (b BackpackFullEvent) Topic() string {
	return "backpack.full"
}
