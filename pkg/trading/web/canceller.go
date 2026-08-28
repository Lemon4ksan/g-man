// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package web

import (
	"context"
	"sync"
	"time"

	log "github.com/lemon4ksan/foundation/async/logkit"
	"github.com/lemon4ksan/foundation/generic"

	"github.com/lemon4ksan/g-man/internal/heap"
	"github.com/lemon4ksan/g-man/pkg/trading"
)

// Canceller coordinates timeout and count limit auto-cancellations for active sent offers.
type Canceller struct {
	priorityQueue *heap.PriorityQueue
	cancelling    sync.Map
	logger        log.Logger
}

// NewCanceller constructs a Canceller instance.
func NewCanceller(logger log.Logger) *Canceller {
	return &Canceller{
		priorityQueue: heap.NewPriorityQueue(),
		logger:        logger,
	}
}

// PushActiveSent adds an active sent offer to the priority queue for tracking.
func (c *Canceller) PushActiveSent(off *trading.TradeOffer) {
	c.priorityQueue.Push(off)
}

// HandleAutoCancellation processes timeout and count limit cancellations on sent offers.
func (c *Canceller) HandleAutoCancellation(
	ctx context.Context,
	cfg Config,
	sent []*trading.TradeOffer,
	sentOffers map[uint64]trading.OfferState,
	mu *sync.RWMutex,
	cancelOfferFn func(ctx context.Context, offerID uint64) error,
) {
	if len(sent) == 0 {
		return
	}

	activeSent := generic.FilterInPlace(sent, func(off *trading.TradeOffer) bool {
		return off.State == trading.OfferStateActive
	})

	c.cancelTimeouts(ctx, cfg, activeSent, cancelOfferFn)
	c.cancelOverLimit(ctx, cfg, activeSent, sentOffers, mu, cancelOfferFn)
}

func (c *Canceller) cancelTimeouts(
	ctx context.Context,
	cfg Config,
	active []*trading.TradeOffer,
	cancelOfferFn func(ctx context.Context, offerID uint64) error,
) {
	if cfg.CancelTime <= 0 {
		return
	}

	for _, off := range active {
		age := time.Since(off.UpdatedAt())
		if age < cfg.CancelTime {
			continue
		}

		if _, loaded := c.cancelling.LoadOrStore(off.ID, true); loaded {
			continue
		}

		if c.logger != nil {
			c.logger.Info("Auto-cancelling active sent offer due to CancelTime timeout",
				log.Uint64("offer_id", off.ID),
				log.Duration("age", age),
			)
		}

		func(id uint64) {
			defer c.cancelling.Delete(id)

			if err := cancelOfferFn(ctx, id); err != nil {
				if c.logger != nil {
					c.logger.Error("Failed to auto-cancel offer", log.Uint64("offer_id", id), log.Err(err))
				}
			}
		}(off.ID)
	}
}

func (c *Canceller) cancelOverLimit(
	ctx context.Context,
	cfg Config,
	active []*trading.TradeOffer,
	sentOffers map[uint64]trading.OfferState,
	mu *sync.RWMutex,
	cancelOfferFn func(ctx context.Context, offerID uint64) error,
) {
	if cfg.CancelOfferCount <= 0 || len(active) < cfg.CancelOfferCount {
		return
	}

	oldest := c.priorityQueue.Peek(func(off *trading.TradeOffer) bool {
		mu.RLock()

		st, ok := sentOffers[off.ID]

		mu.RUnlock()

		if !ok || st != trading.OfferStateActive {
			return false
		}

		if cfg.CancelOfferCountMinAge > 0 && time.Since(off.UpdatedAt()) < cfg.CancelOfferCountMinAge {
			return false
		}

		return true
	})

	if oldest == nil {
		return
	}

	if _, loaded := c.cancelling.LoadOrStore(oldest.ID, true); !loaded {
		if c.logger != nil {
			c.logger.Info("Auto-cancelling oldest active sent offer due to limit",
				log.Uint64("offer_id", oldest.ID),
				log.Int("active_count", len(active)),
				log.Int("limit", cfg.CancelOfferCount),
			)
		}

		func(id uint64) {
			defer c.cancelling.Delete(id)

			if err := cancelOfferFn(ctx, id); err != nil {
				if c.logger != nil {
					c.logger.Error("Failed to auto-cancel oldest offer", log.Uint64("offer_id", id), log.Err(err))
				}
			}
		}(oldest.ID)
	}
}
