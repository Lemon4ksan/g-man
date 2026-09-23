// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package processor executes automated sequential offer processing, asset locking, escrow checking, and exponential retry policies.
package processor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	log "github.com/lemon4ksan/foundation/async/logkit"
	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/silicon/clock"
	"github.com/lemon4ksan/foundation/silicon/pool"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/trading"
)

var (
	// ErrMaxRetriesReached indicates an operation failed repeatedly and exhausted its retry budget.
	ErrMaxRetriesReached = errors.New("max retries reached")
	// ErrCommunityNotReady indicates the community requester is not authenticated or bot is logged off.
	ErrCommunityNotReady = errors.New("community client is not ready (bot not logged in)")
	// ErrEscrowNotFound indicates escrow holding data could not be parsed from Steam Community HTML.
	ErrEscrowNotFound = errors.New("escrow data not found on the page (Steam might be down or offer is invalid)")
	// ErrCounterParamsMissing indicates a counter action was returned without counter parameters.
	ErrCounterParamsMissing = errors.New("processor: counter params missing for counter action")
	// ErrUnknownActionType indicates an unhandled action decision type was encountered.
	ErrUnknownActionType = errors.New("processor: unknown action type")
)

// Details encapsulates trade hold duration in days for both our account and the trade partner.
type Details struct {
	MyDays    int
	TheirDays int
}

// HasHold reports whether either party has an active escrow hold.
func (e Details) HasHold() bool {
	return e.MyDays > 0 || e.TheirDays > 0
}

// HasTheirHold reports whether the partner has an escrow hold.
func (e Details) HasTheirHold() bool { return e.TheirDays > 0 }

// HasMyHold reports whether our account has an escrow hold.
func (e Details) HasMyHold() bool { return e.MyDays > 0 }

// ManagerProvider defines trading manager operations required by the offer processor.
type ManagerProvider interface {
	GetEscrowDuration(ctx context.Context, offerID uint64) (Details, error)
	AcceptOffer(ctx context.Context, offerID uint64) error
	AcceptOfferWithPartner(ctx context.Context, offerID uint64, partnerID id.ID) error
	DeclineOffer(ctx context.Context, offerID uint64) error
	SendOffer(ctx context.Context, p trading.OfferParams) (uint64, error)
}

// BackpackProvider manages item locks in local inventory to prevent double-offering.
type BackpackProvider interface {
	LockItems(ids []uint64)
	UnlockItems(ids []uint64)
}

// OfferHandler evaluates trade offers and determines decisions (accept, decline, counter, skip).
type OfferHandler interface {
	ProcessOffer(ctx context.Context, offer *trading.TradeOffer) (trading.ActionDecision, error)
	OnActionFailed(ctx context.Context, offer *trading.TradeOffer, action trading.ActionType, reason string, err error)
}

// Option configures Processor instances.
type Option = generic.Option[*Processor]

// WithLogger configures a custom logger for the Processor.
func WithLogger(l log.Logger) Option {
	return func(p *Processor) {
		p.logger = l
	}
}

// Processor manages a buffered queue of incoming trade offers and executes trade actions sequentially.
type Processor struct {
	manager  ManagerProvider
	backpack BackpackProvider
	handler  OfferHandler
	logger   log.Logger

	queue chan *trading.TradeOffer

	processing sync.Map
}

// New creates a new offer Processor with the specified dependencies.
func New(
	manager ManagerProvider,
	backpack BackpackProvider,
	handler OfferHandler,
	opts ...generic.Option[*Processor],
) *Processor {
	p := &Processor{
		manager:  manager,
		handler:  handler,
		backpack: backpack,
		logger:   log.Discard,
		queue:    make(chan *trading.TradeOffer, 500),
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Start launches the background worker goroutine for processing enqueued offers.
func (p *Processor) Start(ctx context.Context) {
	go p.worker(ctx)
}

// Enqueue adds an offer to the processing queue if it is not already being processed.
func (p *Processor) Enqueue(off *trading.TradeOffer) {
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

// CheckEscrow verifies whether the trade offer has an escrow hold duration.
//
// Invariant: Do not remove or bypass the `offer.IsOurOffer` check.
// On Steam Community, GET "/tradeoffer/{offerID}/" for an outgoing offer (offer.IsOurOffer == true)
// renders the "Waiting for partner" page which does not include `g_daysTheirEscrow` javascript variables.
// Scraping escrow on outgoing trades always yields ErrEscrowNotFound.
// Outgoing escrow is checked pre-trade via trade URL or post-trade via OfferStateInEscrow (State 11).
//
// Parity: matches node-steam-tradeoffer-manager (lib/classes/TradeOffer.js).
func (p *Processor) CheckEscrow(ctx context.Context, offer *trading.TradeOffer) (bool, error) {
	if offer == nil || offer.IsOurOffer {
		return false, nil
	}

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

	return details.HasHold(), nil
}

func (p *Processor) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return

		case off := <-p.queue:
			p.processSingleOffer(ctx, off)

			time.AfterFunc(5*time.Second, func() {
				p.processing.Delete(off.ID)
			})
		}
	}
}

func (p *Processor) processSingleOffer(ctx context.Context, off *trading.TradeOffer) {
	start := clock.CoarseTime()
	l := p.logger.With(log.Uint64("offerID", off.ID))

	ourItemIDs := make([]uint64, 0, len(off.ItemsToGive))
	for _, it := range off.ItemsToGive {
		ourItemIDs = append(ourItemIDs, it.AssetID)
	}

	if len(ourItemIDs) > 0 {
		p.backpack.LockItems(ourItemIDs)
		l.Debug("Locked our items for processing")
	}

	shouldUnlock := true
	defer func() {
		if shouldUnlock && len(ourItemIDs) > 0 {
			p.backpack.UnlockItems(ourItemIDs)
			l.Debug("Unlocked our items")
		}
	}()

	ctx = protocol.WithTransportType(ctx, protocol.TransportWebAPI)

	decision, err := p.handler.ProcessOffer(ctx, off)
	if err != nil {
		l.Error("Handler failed to process offer", log.Err(err))
		return
	}

	err = p.applyAction(ctx, off, decision)
	if err != nil {
		p.handler.OnActionFailed(ctx, off, decision.Action, decision.Reason, err)
		return
	}

	if decision.Action == trading.ActionAccept {
		shouldUnlock = false
	}

	l.Debug("Finished processing offer", log.Duration("took", time.Since(start)))
}

func (p *Processor) applyAction(ctx context.Context, off *trading.TradeOffer, decision trading.ActionDecision) error {
	switch decision.Action {
	case trading.ActionAccept:
		// Parity: matches @tf2autobot/tradeoffer-manager by passing partner 64-bit SteamID to avoid HTTP 403.
		return p.withRetry(ctx, 5, func() error {
			return p.manager.AcceptOfferWithPartner(ctx, off.ID, off.OtherSteamID)
		})

	case trading.ActionDecline:
		return p.withRetry(ctx, 5, func() error {
			return p.manager.DeclineOffer(ctx, off.ID)
		})

	case trading.ActionCounter:
		if decision.CounterParams == nil {
			return ErrCounterParamsMissing
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

	case trading.ActionSkip:
		return nil

	default:
		return ErrUnknownActionType
	}
}

func (p *Processor) withRetry(ctx context.Context, maxRetries int, fn func() error) error {
	var err error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}

		if attempt == maxRetries {
			break
		}

		if errors.Is(err, ErrEscrowNotFound) {
			return err
		}

		backoffDuration := time.Duration(1<<attempt) * time.Second
		p.logger.Warn("Action failed, retrying",
			log.Err(err),
			log.Int("attempt", attempt+1),
			log.Duration("backoff", backoffDuration),
		)

		timer := pool.AcquireTimer(backoffDuration)
		select {
		case <-timer.C:
			pool.ReleaseTimer(timer)
		case <-ctx.Done():
			pool.ReleaseTimer(timer)
			return ctx.Err()
		}
	}

	return err
}
