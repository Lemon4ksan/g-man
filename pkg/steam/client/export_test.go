// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

func (c *Client) ForceState(state State) {
	c.fsm.ForceSet(state)
}

type (
	NoopSocketProvider = noopSocketProvider
	InitContext        = initContext
)
