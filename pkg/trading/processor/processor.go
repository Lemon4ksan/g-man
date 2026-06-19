// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package processor

import (
	"context"
	"fmt"
	"time"

	"github.com/lemon4ksan/miyako/generic"
	"github.com/lemon4ksan/miyako/sync/keylock"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/trading"
	"github.com/lemon4ksan/g-man/pkg/trading/engine"
	"github.com/lemon4ksan/g-man/pkg/trading/notifications"
	"github.com/lemon4ksan/g-man/pkg/trading/review"
)

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

	// Queue for sequential processing (to avoid race conditions in inventory)
	queue chan *trading.TradeOffer

	// Tracking busy items using striped per-key locking
	itemLocks *keylock.KeyMutex[uint64]
	busyItems map[uint64]uint64 // assetID -> offerID
}

// New creates a new Processor instance with the provided execution, decision,
// notification, and reporting dependencies.
func New(ex TradeExecutor, eng *engine.Engine, n *notifications.Manager, r *review.Reviewer, l log.Logger) *Processor {
	return &Processor{
		executor:  ex,
		engine:    eng,
		notif:     n,
		reviewer:  r,
		logger:    l.With(log.Module("processor")),
		queue:     make(chan *trading.TradeOffer, 100),
		itemLocks: keylock.New[uint64](),
		busyItems: make(map[uint64]uint64),
	}
}

// Start launches the sequential background worker goroutine.
//
// This worker reads from the internal queue and processes queued trade offers
// sequentially to ensure inventory synchronization.
func (p *Processor) Start(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case offer := <-p.queue:
				p.handleOffer(ctx, offer)
			}
		}
	}()
}

// Enqueue adds the trade offer to the internal queue for sequential processing.
//
// If the internal queue buffer is full, writing to the queue blocks the caller.
func (p *Processor) Enqueue(offer *trading.TradeOffer) {
	p.queue <- offer
}

func (p *Processor) handleOffer(ctx context.Context, offer *trading.TradeOffer) {
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
	allItems := append(offer.ItemsToGive, offer.ItemsToReceive...) //nolint:gocritic

	for _, item := range allItems {
		p.itemLocks.Lock(item.AssetID)
	}

	for _, item := range allItems {
		p.busyItems[item.AssetID] = offer.ID
	}
}

func (p *Processor) unlockItems(offer *trading.TradeOffer) {
	for _, item := range append(offer.ItemsToGive, offer.ItemsToReceive...) {
		delete(p.busyItems, item.AssetID)
		p.itemLocks.Unlock(item.AssetID)
	}
}
