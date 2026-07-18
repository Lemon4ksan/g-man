// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package processor acts as the central orchestrator for the trade management subsystem.
package processor

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/lemon4ksan/miyako/bus"
	"github.com/lemon4ksan/miyako/generic"
	"github.com/lemon4ksan/miyako/log"
	"github.com/lemon4ksan/miyako/sync/keylock"

	"github.com/lemon4ksan/g-man/pkg/behavior"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/trading"
	"github.com/lemon4ksan/g-man/pkg/trading/engine"
	"github.com/lemon4ksan/g-man/pkg/trading/notifications"
	"github.com/lemon4ksan/g-man/pkg/trading/review"
	"github.com/lemon4ksan/g-man/pkg/trading/web"
)

// ProcessTrades registers the trade processing behavior with the orchestrator.
func ProcessTrades(client *steam.Client, eng *engine.Engine, n *notifications.Manager, r *review.Reviewer) {
	behavior.From(client).Register(New(web.From(client), eng, n, r, client.Bus(), client.Logger()))
}

// TradeExecutor defines the interface for executing final trade actions on Steam.
//
// This interface is typically implemented by the trade manager.
type TradeExecutor interface {
	// AcceptOffer approves and finalizes the specified trade offer ID.
	AcceptOffer(ctx context.Context, id uint64) error
	// DeclineOffer rejects and cancels the specified trade offer ID.
	DeclineOffer(ctx context.Context, id uint64) error
}

// Processor coordinates the sequential processing of trade offers.
//
// It manages an internal, sequential processing queue to avoid concurrency races
// in stock inventory, and maintains an active asset lock registry to prevent "double-spending"
// (re-using the same item in parallel trade processing cycles).
//
// Create new instances of Processor using the [New] constructor.
type Processor struct {
	executor TradeExecutor
	engine   *engine.Engine
	notif    *notifications.Manager
	reviewer *review.Reviewer
	logger   log.Logger
	bus      *bus.Bus

	// Queue for sequential processing (to avoid race conditions in inventory)
	queue chan *trading.TradeOffer

	// Tracking busy items using striped per-key locking
	itemLocks *keylock.KeyMutex[uint64]
	busyItems map[uint64]uint64 // assetID -> offerID

	// Deduplication: prevents the same offer from being enqueued twice
	processing sync.Map
}

// New creates a new Processor instance with the provided execution, decision,
// notification, and reporting dependencies.
func New(
	ex TradeExecutor,
	eng *engine.Engine,
	n *notifications.Manager,
	r *review.Reviewer,
	b *bus.Bus,
	l log.Logger,
) *Processor {
	if b == nil {
		b = bus.New()
	}

	if l == nil {
		l = log.Discard
	}

	return &Processor{
		executor:  ex,
		engine:    eng,
		notif:     n,
		reviewer:  r,
		bus:       b,
		logger:    l.With(log.Module("processor")),
		queue:     make(chan *trading.TradeOffer, 100),
		itemLocks: keylock.New[uint64](),
		busyItems: make(map[uint64]uint64),
	}
}

// Name returns the name of this behavior.
func (p *Processor) Name() string {
	return "trade_processor"
}

// Run launches the sequential background worker goroutine.
//
// This worker reads from the internal queue and processes queued trade offers
// sequentially to ensure inventory synchronization.
func (p *Processor) Run(ctx context.Context) error {
	sub := p.bus.Subscribe(&web.NewOfferEvent{})
	defer sub.Unsubscribe()

	// Run queue worker in a separate goroutine to ensure sequential processing
	go p.worker(ctx)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-sub.C():
			if !ok {
				return nil
			}

			switch ev := ev.(type) {
			case *web.NewOfferEvent:
				p.logger.Info("New active trade offer received from event bus",
					log.Uint64("offer_id", ev.Offer.ID),
					log.Uint64("partner_steam_id", uint64(ev.Offer.OtherSteamID)),
				)
				p.Enqueue(ev.Offer)

			default:
			}
		}
	}
}

func (p *Processor) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case offer := <-p.queue:
			p.handleOffer(ctx, offer)
		}
	}
}

// Enqueue adds the trade offer to the internal queue for sequential processing.
//
// If the internal queue buffer is full, writing to the queue blocks the caller.
// Duplicate offers (same ID) are silently ignored.
func (p *Processor) Enqueue(offer *trading.TradeOffer) {
	if _, loaded := p.processing.LoadOrStore(offer.ID, true); loaded {
		return
	}

	select {
	case p.queue <- offer:
		p.logger.Debug("Offer enqueued for processing", log.Uint64("offerID", offer.ID))
	default:
		p.logger.Warn("Offer queue full, dropping offer", log.Uint64("offerID", offer.ID))
		p.processing.Delete(offer.ID)
	}
}

func (p *Processor) handleOffer(ctx context.Context, offer *trading.TradeOffer) {
	defer p.processing.Delete(offer.ID)

	start := time.Now()

	// Generate a unique CorrelationID for this trade offer reasoning execution
	corrID := fmt.Sprintf("offer-%d-%s", offer.ID, log.GenerateCorrelationID()[:8])
	ctx = log.WithCorrelationID(ctx, corrID)

	p.logger.InfoContext(ctx, "Processing offer", log.Uint64("id", offer.ID))

	if p.isAnyItemBusy(offer) {
		p.logger.WarnContext(ctx, "Offer skipped: items are busy in another trade", log.Uint64("id", offer.ID))
		return
	}

	p.lockItems(offer)
	defer p.unlockItems(offer)

	ctx = protocol.WithTransportType(ctx, protocol.TransportWebAPI)

	verdict, err := p.engine.Process(ctx, offer)
	if err != nil {
		p.logger.ErrorContext(ctx, "Engine failed to process offer", log.Err(err), log.Uint64("id", offer.ID))
		return
	}

	p.executeVerdict(ctx, offer, verdict, time.Since(start))
}

func (p *Processor) executeVerdict(
	ctx context.Context,
	offer *trading.TradeOffer,
	v *engine.Verdict,
	duration time.Duration,
) {
	switch v.Action {
	case trading.ActionAccept:
		if err := p.executor.AcceptOffer(ctx, offer.ID); err == nil {
			_ = p.notif.SendNotification(ctx, p.makeNotifInfo(offer, notifications.StateAccepted, v))
		}

	case trading.ActionDecline:
		if err := p.executor.DeclineOffer(ctx, offer.ID); err == nil {
			_ = p.notif.SendNotification(ctx, p.makeNotifInfo(offer, notifications.StateDeclined, v))
			_ = p.reviewer.SendDeclinedAlert(ctx, offer.ID, offer.OtherSteamID, p.makeReviewMeta(v, duration), nil)
		}

	case trading.ActionReview:
		p.logger.InfoContext(ctx, "Offer sent to manual review", log.Uint64("id", offer.ID))
		_ = p.notif.SendNotification(ctx, p.makeNotifInfo(offer, notifications.StateActive, v))
		_ = p.reviewer.SendReviewAlert(ctx, offer.ID, offer.OtherSteamID, p.makeReviewMeta(v, duration))

	case trading.ActionIgnore:
		p.logger.DebugContext(ctx, "Offer ignored by engine", log.Uint64("id", offer.ID))
	}
}

func (p *Processor) makeNotifInfo(
	offer *trading.TradeOffer,
	state notifications.TradeState,
	v *engine.Verdict,
) *notifications.TradeInfo {
	return &notifications.TradeInfo{
		OfferID:        offer.ID,
		PartnerSteamID: offer.OtherSteamID,
		OldState:       state,
		ReasonType:     v.Reason,
	}
}

func (p *Processor) makeReviewMeta(v *engine.Verdict, d time.Duration) *review.TradeMetadata {
	return &review.TradeMetadata{
		PrimaryReason: v.Reason,
		ProcessTimeMS: d.Milliseconds(),
	}
}

func (p *Processor) isAnyItemBusy(offer *trading.TradeOffer) bool {
	return generic.Any(append(offer.ItemsToGive, offer.ItemsToReceive...), func(item *trading.Item) bool {
		return p.itemLocks.IsLocked(item.AssetID)
	})
}

func (p *Processor) lockItems(offer *trading.TradeOffer) {
	allItems := make([]*trading.Item, len(offer.ItemsToGive)+len(offer.ItemsToReceive))
	copy(allItems, offer.ItemsToGive)
	copy(allItems[len(offer.ItemsToGive):], offer.ItemsToReceive)

	ids := make([]uint64, len(allItems))
	for i, item := range allItems {
		ids[i] = item.AssetID
	}

	slices.Sort(ids)

	for _, id := range ids {
		p.itemLocks.Lock(id)
	}

	for _, id := range ids {
		p.busyItems[id] = offer.ID
	}
}

func (p *Processor) unlockItems(offer *trading.TradeOffer) {
	allItems := make([]*trading.Item, len(offer.ItemsToGive)+len(offer.ItemsToReceive))
	copy(allItems, offer.ItemsToGive)
	copy(allItems[len(offer.ItemsToGive):], offer.ItemsToReceive)

	ids := make([]uint64, len(allItems))
	for i, item := range allItems {
		ids[i] = item.AssetID
	}

	slices.Sort(ids)

	for _, id := range ids {
		delete(p.busyItems, id)
	}

	for _, id := range ids {
		p.itemLocks.Unlock(id)
	}
}
