// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package processor

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sync"
	"time"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/trading"
	"github.com/lemon4ksan/g-man/pkg/trading/web/offer"
)

var (
	RxTheir = regexp.MustCompile(`(?i)g_DaysTheirEscrow\s*=\s*(\d+);`)
	RxMy    = regexp.MustCompile(`(?i)g_DaysMyEscrow\s*=\s*(\d+);`)

	ErrMaxRetriesReached = errors.New("max retries reached")
	ErrCommunityNotReady = errors.New("community client is not ready (bot not logged in)")
	ErrEscrowNotFound    = errors.New(
		"escrow data not found on the page (Steam might be down or offer is invalid)",
	)
)

// Details contains information about the trade delay (in days).
type Details struct {
	MyDays    int
	TheirDays int
}

// HasHold returns true if either side has a hold.
func (e Details) HasHold() bool {
	return e.MyDays > 0 || e.TheirDays > 0
}

type ManagerProvider interface {
	GetEscrowDuration(ctx context.Context, offerID uint64) (Details, error)
	AcceptOffer(ctx context.Context, offerID uint64) error
	DeclineOffer(ctx context.Context, offerID uint64) error
	SendOffer(ctx context.Context, p trading.OfferParams) (uint64, error)
}

type BackpackProvider interface {
	LockItems(ids []uint64)
	UnlockItems(ids []uint64)
}

// OfferHandler is implemented by your main bot logic.
// The Processor will call these methods sequentially.
type OfferHandler interface {
	// ProcessOffer analyzes the offer and decides what to do.
	ProcessOffer(ctx context.Context, offer *offer.TradeOffer) (offer.ActionDecision, error)
	// OnActionFailed is called if the SDK completely fails to execute the action after all retries.
	OnActionFailed(ctx context.Context, offer *offer.TradeOffer, action offer.ActionType, reason string, err error)
}

func WithLogger(l log.Logger) bus.Option[*Processor] {
	return func(p *Processor) {
		p.logger = l
	}
}

// Processor handles a sequential queue of incoming trade offers.
// It ensures that only one offer is evaluated at a time to prevent race conditions
// with inventory and pure calculations.
type Processor struct {
	manager  ManagerProvider
	backpack BackpackProvider
	handler  OfferHandler
	logger   log.Logger

	queue chan *offer.TradeOffer

	// ItemsInTrade tracks assetIDs that are currently involved in active offers.
	processing sync.Map
}

// NewProcessor creates a new sequential offer processor.
func NewProcessor(
	manager ManagerProvider,
	backpack BackpackProvider,
	handler OfferHandler,
	opts ...bus.Option[*Processor],
) *Processor {
	return &Processor{
		manager:  manager,
		handler:  handler,
		backpack: backpack,
		logger:   log.Discard,
		queue:    make(chan *offer.TradeOffer, 500), // Buffered queue for incoming offers
	}
}

// Start begins the worker goroutine.
func (p *Processor) Start(ctx context.Context) {
	go p.worker(ctx)
}

// Enqueue adds an offer to the processing queue if it isn't already handled.
func (p *Processor) Enqueue(off *offer.TradeOffer) {
	if _, loaded := p.processing.LoadOrStore(off.ID, true); loaded {
		return
	}

	select {
	case p.queue <- off:
		p.logger.Debug("Offer enqueued for processing", log.Uint64("offerID", off.ID))
	default:
		p.logger.Warn("Offer queue full, dropping offer", log.Uint64("offerID", off.ID))
		p.processing.Delete(off.ID)
	}
}

// CheckEscrow checks if the partner has a Trade Hold.
func (p *Processor) CheckEscrow(ctx context.Context, offer *offer.TradeOffer) (bool, error) {
	if offer.EscrowEndDate > 0 {
		return true, nil
	}

	var details Details

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

func (p *Processor) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case off := <-p.queue:
			p.processSingleOffer(ctx, off)
			p.processing.Delete(off.ID)
		}
	}
}

func (p *Processor) processSingleOffer(ctx context.Context, off *offer.TradeOffer) {
	start := time.Now()
	l := p.logger.With(log.Uint64("offerID", off.ID))

	ourItemIDs := make([]uint64, 0, len(off.ItemsToGive))
	for _, it := range off.ItemsToGive {
		ourItemIDs = append(ourItemIDs, it.AssetID)
	}

	if len(ourItemIDs) > 0 {
		p.backpack.LockItems(ourItemIDs)
		l.Debug("Locked our items for processing")
	}

	decision, err := p.handler.ProcessOffer(ctx, off)
	if err != nil {
		l.Error("Handler failed to process offer", log.Err(err))

		if len(ourItemIDs) > 0 {
			p.backpack.UnlockItems(ourItemIDs)
		}

		return
	}

	err = p.applyAction(ctx, off, decision)
	if err != nil {
		p.handler.OnActionFailed(ctx, off, decision.Action, decision.Reason, err)
	}

	if decision.Action == offer.ActionDecline || decision.Action == offer.ActionSkip {
		if len(ourItemIDs) > 0 {
			p.backpack.UnlockItems(ourItemIDs)
			l.Debug("Unlocked our items after decline/skip")
		}
	}

	l.Debug("Finished processing offer", log.Duration("took", time.Since(start)))
}

// applyAction executes the decision and handles retries automatically.
func (p *Processor) applyAction(ctx context.Context, off *offer.TradeOffer, decision offer.ActionDecision) error {
	switch decision.Action {
	case offer.ActionAccept:
		return p.withRetry(ctx, 5, func() error {
			return p.manager.AcceptOffer(ctx, off.ID)
		})
	case offer.ActionDecline:
		return p.withRetry(ctx, 5, func() error {
			return p.manager.DeclineOffer(ctx, off.ID)
		})
	case offer.ActionCounter:
		if decision.CounterParams == nil {
			return errors.New("counter params are missing for counter action")
		}

		params := trading.OfferParams{
			PartnerID:      off.OtherSteamID,
			Token:          decision.CounterParams.Token,
			Message:        decision.CounterParams.Message,
			ItemsToGive:    decision.CounterParams.ItemsToGive,
			ItemsToReceive: decision.CounterParams.ItemsToReceive,
			CounteredID:    off.ID,
		}

		_, err := p.manager.SendOffer(ctx, params)

		return err

	case offer.ActionSkip:
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
