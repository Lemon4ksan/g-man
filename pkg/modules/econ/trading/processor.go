// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trading

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/lemon4ksan/g-man/pkg/log"
)

var ErrMaxRetriesReached = errors.New("max retries reached")

// Processor handles a sequential queue of incoming trade offers.
// It ensures that only one offer is evaluated at a time to prevent race conditions
// with inventory and pure calculations.
type Processor struct {
	manager *Manager
	handler OfferHandler
	logger  log.Logger

	queue chan *TradeOffer

	// ItemsInTrade tracks assetIDs that are currently involved in active offers.
	itemsInTrade sync.Map
}

// NewProcessor creates a new sequential offer processor.
func NewProcessor(manager *Manager, handler OfferHandler, logger log.Logger) *Processor {
	return &Processor{
		manager: manager,
		handler: handler,
		logger:  logger,
		queue:   make(chan *TradeOffer, 500), // Buffered queue for incoming offers
	}
}

// Start begins the worker goroutine.
func (p *Processor) Start(ctx context.Context) {
	go p.worker(ctx)
}

// Enqueue adds an offer to the processing queue if it isn't already handled.
func (p *Processor) Enqueue(offer *TradeOffer) {
	// Lock items from this offer immediately so other background tasks know they are pending
	for _, item := range offer.ItemsToGive {
		p.SetItemInTrade(item.AssetID)
	}

	select {
	case p.queue <- offer:
		p.logger.Debug("Added offer to queue", log.Uint64("offerID", offer.ID))
	default:
		p.logger.Warn("Offer queue is full, dropping offer", log.Uint64("offerID", offer.ID))
	}
}

// worker processes offers one by one.
func (p *Processor) worker(ctx context.Context) {
	p.logger.Info("Trade offer processor started")

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("Trade offer processor stopped")
			return
		case offer := <-p.queue:
			p.processSingleOffer(ctx, offer)
		}
	}
}

func (p *Processor) processSingleOffer(ctx context.Context, offer *TradeOffer) {
	start := time.Now()
	p.logger.Debug("Handling offer", log.Uint64("offerID", offer.ID))

	// Call your bot's business logic (e.g., check prices, check bans)
	decision, err := p.handler.ProcessOffer(ctx, offer)
	if err != nil {
		p.logger.Error("Handler failed to process offer", log.Err(err), log.Uint64("offerID", offer.ID))
		return
	}

	p.logger.Debug("Handler decision", log.String("action", string(decision.Action)))

	// Execute the action (with retries)
	err = p.applyAction(ctx, offer, decision)
	if err != nil {
		p.handler.OnActionFailed(ctx, offer, decision.Action, decision.Reason, err)

		// If counter failed, fallback to decline (as per TS logic)
		if decision.Action == ActionCounter {
			p.logger.Warn("Counter failed, falling back to decline", log.Uint64("offerID", offer.ID))
			decision.Action = ActionDecline
			decision.Reason = "COUNTER_INVALID_VALUE_FAILED"
			_ = p.applyAction(ctx, offer, decision)
		}
	}

	// Unlock items if skipped or declined
	if decision.Action == ActionSkip || decision.Action == ActionDecline {
		for _, item := range offer.ItemsToGive {
			p.UnsetItemInTrade(item.AssetID)
		}
	}

	timeTaken := time.Since(start)
	p.logger.Debug("Finished processing offer", log.Uint64("offerID", offer.ID), log.Duration("took", timeTaken))
}

// Item Tracking Helpers

func (p *Processor) SetItemInTrade(assetID uint64) {
	p.itemsInTrade.Store(assetID, struct{}{})
}

func (p *Processor) UnsetItemInTrade(assetID uint64) {
	p.itemsInTrade.Delete(assetID)
}

func (p *Processor) IsInTrade(assetID uint64) bool {
	_, exists := p.itemsInTrade.Load(assetID)
	return exists
}

// CheckEscrow checks if the partner has a Trade Hold.
func (p *Processor) CheckEscrow(ctx context.Context, offer *TradeOffer) (bool, error) {
	if offer.EscrowEndDate > 0 {
		return true, nil
	}

	var details EscrowDetails

	err := p.withRetry(ctx, 5, func() error {
		var fetchErr error
		details, fetchErr = p.manager.GetEscrowDuration(ctx, offer.ID)

		if errors.Is(fetchErr, ErrEscrowNotFound) {
			return fetchErr
		}
		return fetchErr
	})

	if err != nil {
		return false, fmt.Errorf("escrow check failed after retries: %w", err)
	}

	p.logger.Debug("Escrow check success",
		log.Int("myHoldDays", details.MyDays),
		log.Int("theirHoldDays", details.TheirDays),
	)

	return details.TheirDays > 0, nil
}

// applyAction executes the decision and handles retries automatically.
func (p *Processor) applyAction(ctx context.Context, offer *TradeOffer, decision ActionDecision) error {
	switch decision.Action {
	case ActionAccept:
		return p.withRetry(ctx, 5, func() error {
			return p.manager.AcceptOffer(ctx, offer.ID)
		})
	case ActionDecline:
		return p.withRetry(ctx, 5, func() error {
			return p.manager.DeclineOffer(ctx, offer.ID)
		})
	case ActionCounter:
		// Counter logic is complex, assuming p.manager.CounterOffer exists
		// return p.manager.CounterOffer(ctx, offer, decision.Meta)
		return errors.New("counter not fully implemented in manager yet")
	case ActionSkip:
		return nil
	default:
		return errors.New("unknown action type")
	}
}

// withRetry implements exponential backoff retry logic.
// Matches TS logic: attempt -> wait(2^attempts * 1000) -> retry.
func (p *Processor) withRetry(ctx context.Context, maxRetries int, fn func() error) error {
	var err error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		err = fn()
		if err == nil {
			return nil // Success
		}

		// Don't sleep on the last attempt
		if attempt == maxRetries {
			break
		}

		// TODO: Check if error is fatal (e.g. Steam dropped connection permanently)
		// if isFatalError(err) { return err }

		// Calculate backoff: 2^attempt seconds (1s, 2s, 4s, 8s, 16s)
		backoffDuration := time.Duration(math.Pow(2, float64(attempt))) * time.Second
		p.logger.Warn("Action failed, retrying",
			log.Err(err),
			log.Int("attempt", attempt+1),
			log.Duration("backoff", backoffDuration),
		)

		// Wait for backoff or context cancellation
		select {
		case <-time.After(backoffDuration):
			// continue loop
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return err
}
