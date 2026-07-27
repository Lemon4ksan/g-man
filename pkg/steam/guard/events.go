// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package guard

import (
	"github.com/lemon4ksan/miyako/bus"
)

// ConfirmationRequiredEvent is published when trade or account actions require mobile/email approval.
type ConfirmationRequiredEvent struct {
	bus.BaseEvent
	TradeOfferID string
	IsAppConfirm bool
	IsEmail      bool
	EmailDomain  string
}

// NeedAuthEvent is published when mobile confirmation endpoints report re-authentication is required.
type NeedAuthEvent struct {
	bus.BaseEvent
	Message string
}
