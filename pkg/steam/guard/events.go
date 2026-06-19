// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package guard

import (
	"github.com/lemon4ksan/miyako/bus"
)

// ConfirmationRequiredEvent is emitted when a trade action (sending or accepting)
// requires mobile or email confirmation.
type ConfirmationRequiredEvent struct {
	bus.BaseEvent
	// TradeOfferID is the unique identifier of the trade offer.
	TradeOfferID string
	// IsAppConfirm is true if the confirmation must be completed via the mobile app.
	IsAppConfirm bool
	// IsEmail is true if the confirmation code was sent via email.
	IsEmail bool
	// EmailDomain is the target domain name where the email confirmation was sent.
	EmailDomain string
}

// NeedAuthEvent is emitted when confirmation is returned with NeedAuth field set to True.
type NeedAuthEvent struct {
	bus.BaseEvent
	// Message is the authentication failure description from Steam.
	Message string
}
