// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package processor executes automated sequential offer processing, asset locking, escrow checking, and exponential retry policies.
package processor

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sync"
	"time"

	"github.com/lemon4ksan/miyako/generic"
	"github.com/lemon4ksan/miyako/log"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/trading"
)

var (
	RxTheir = regexp.MustCompile(`(?i)g_DaysTheirEscrow\s*=\s*(\d+);`)
	RxMy    = regexp.MustCompile(`(?i)g_DaysMyEscrow\s*=\s*(\d+);`)

	ErrMaxRetriesReached    = errors.New("max retries reached")
	ErrCommunityNotReady    = errors.New("community client is not ready (bot not logged in)")
	ErrEscrowNotFound       = errors.New("escrow data not found on the page (Steam might be down or offer is invalid)")
	ErrCounterParamsMissing = errors.New("processor: counter params missing for counter action")
	ErrUnknownActionType    = errors.New("processor: unknown action type")
)

type Details struct {
	MyDays    int
	TheirDays int
}

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

type OfferHandler interface {
	ProcessOffer(ctx context.Context, offer *trading.TradeOffer) (trading.ActionDecision, error)
	OnActionFailed(ctx context.Context, offer *trading.TradeOffer, action trading.ActionType, reason string, err error)
}

type Option = generic.Option[*Processor]

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

func New(
	manager ManagerProvider,
	backpack BackpackProvider,
	handler OfferHandler,
	opts ...generic.Option[*Processor],
) *Processor {
	return &Processor{
		manager:  manager,
		handler:  handler,
		backpack: backpack,
		logger:   log.Discard,
		queue:    make(chan *trading.TradeOffer, 500),
	}
}

func (p *Processor) Start(ctx context.Context) {
	go p.worker(ctx)
}

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

func (p *Processor) CheckEscrow(ctx context.Context, offer *trading.TradeOffer) (bool, error) {
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

			time.AfterFunc(5*time.Second, func() {
				p.processing.Delete(off.ID)
			})
		}
	}
}

func (p *Processor) processSingleOffer(ctx context.Context, off *trading.TradeOffer) {
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
		return p.withRetry(ctx, 5, func() error {
			return p.manager.AcceptOffer(ctx, off.ID)
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

		backoffDuration := time.Duration(math.Pow(2, float64(attempt))) * time.Second
		p.logger.Warn("Action failed, retrying",
			log.Err(err),
			log.Int("attempt", attempt+1),
			log.Duration("backoff", backoffDuration),
		)

		timer := time.NewTimer(backoffDuration)
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}

			return ctx.Err()
		}
	}

	return err
}
