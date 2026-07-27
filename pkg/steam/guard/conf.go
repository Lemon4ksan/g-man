// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package guard

import (
	"fmt"
	"time"
)

type ConfirmationsList struct {
	Success       bool            `json:"success"`
	Confirmations []*Confirmation `json:"conf"`
	Message       string          `json:"message"`
	Detail        string          `json:"detail"`
	NeedAuth      bool            `json:"needauth"`
}

type ConfirmationType int

const (
	ConfTypeGeneric ConfirmationType = iota
	ConfTypeTrade
	ConfTypeMarket
	ConfTypeLogin
	ConfTypeAccountChange
)

func (ct ConfirmationType) String() string {
	switch ct {
	case ConfTypeGeneric:
		return "generic"
	case ConfTypeTrade:
		return "trade"
	case ConfTypeMarket:
		return "market"
	case ConfTypeLogin:
		return "login"
	case ConfTypeAccountChange:
		return "account_change"
	default:
		return "unknown"
	}
}

// Confirmation represents a Steam Guard mobile confirmation payload.
type Confirmation struct {
	ID          uint64           `json:"id,string"`
	Nonce       uint64           `json:"nonce,string"`
	CreatorID   uint64           `json:"creator_id,string"`
	Type        ConfirmationType `json:"type"`
	Title       string           `json:"title"`
	Receiving   string           `json:"receiving"`
	Time        string           `json:"time"`
	Icon        string           `json:"icon"`
	Requester   string           `json:"requester,omitempty"`
	Description string           `json:"description,omitempty"`
	expiresAt   time.Time
}

// IsExpired checks if the confirmation has passed its expiration time.
func (c *Confirmation) IsExpired() bool {
	return !c.expiresAt.IsZero() && time.Now().After(c.expiresAt)
}

// TimeRemaining returns remaining time until confirmation expiration.
func (c *Confirmation) TimeRemaining() time.Duration {
	if c.expiresAt.IsZero() {
		return 2 * time.Minute
	}

	return time.Until(c.expiresAt)
}

func (c *Confirmation) String() string {
	return fmt.Sprintf("Confirmation{ID=%d, Type=%s, Title=%q, ExpiresIn=%v}",
		c.ID, c.Type, truncate(c.Title, 20), c.TimeRemaining())
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return s[:n-3] + "..."
}
