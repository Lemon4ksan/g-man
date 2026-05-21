// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package guard

import (
	"github.com/lemon4ksan/g-man/pkg/bus"
)

// ConfirmationRequiredEvent is emitted when a trade action (sending or accepting)
// requires mobile or email confirmation.
type ConfirmationRequiredEvent struct {
	bus.BaseEvent
	TradeOfferID string
	IsAppConfirm bool
	IsEmail      bool
	EmailDomain  string
}

// NeedAuthEvent is emitted when confirmation is returned with NeedAuth field set to True.
type NeedAuthEvent struct {
	bus.BaseEvent
	Message string
}
