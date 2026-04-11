// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package processor

import (
	"context"
	"sync"
	"time"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/trading/engine"
	"github.com/lemon4ksan/g-man/pkg/trading/notifications"
	"github.com/lemon4ksan/g-man/pkg/trading/review"
	trading "github.com/lemon4ksan/g-man/pkg/trading/web"
)

// TradeExecutor is an interface for executing actions in Steam (Accept/Decline).
// It is implemented by the trading.Manager module.
type TradeExecutor interface {
	AcceptOffer(ctx context.Context, id uint64) error
	DeclineOffer(ctx context.Context, id uint64) error
}

// Processor coordinates the processing of one trade offer.
type Processor struct {
	executor TradeExecutor
	engine   *engine.Engine
	notif    *notifications.Manager
	reviewer *review.Reviewer
	logger   log.Logger

	// Queue for sequential processing (to avoid race conditions in inventory)
	queue chan *trading.TradeOffer

	// Tracking busy items
	mu        busyItemsMu
	busyItems map[uint64]uint64 // assetID -> offerID
}

type busyItemsMu struct {
	sync.RWMutex
}

func New(ex TradeExecutor, eng *engine.Engine, n *notifications.Manager, r *review.Reviewer, l log.Logger) *Processor {
	return &Processor{
		executor:  ex,
		engine:    eng,
		notif:     n,
		reviewer:  r,
		logger:    l.With(log.Module("processor")),
		queue:     make(chan *trading.TradeOffer, 100),
		busyItems: make(map[uint64]uint64),
	}
}

// Start starts a worker that cleans up the offer queue.
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

// Enqueue adds the offer to the queue for processing.
func (p *Processor) Enqueue(offer *trading.TradeOffer) {
	p.queue <- offer
}

func (p *Processor) handleOffer(ctx context.Context, offer *trading.TradeOffer) {
	start := time.Now()

	p.logger.Info("Processing offer", log.Uint64("id", offer.ID))

	if p.isAnyItemBusy(offer) {
		p.logger.Warn("Offer skipped: items are busy in another trade", log.Uint64("id", offer.ID))
		return
	}

	p.lockItems(offer)
	defer p.unlockItems(offer)

	verdict, err := p.engine.Process(ctx, offer)
	if err != nil {
		p.logger.Error("Engine failed to process offer", log.Err(err), log.Uint64("id", offer.ID))
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
	case engine.ActionAccept:
		if err := p.executor.AcceptOffer(ctx, offer.ID); err == nil {
			_ = p.notif.SendNotification(ctx, p.makeNotifInfo(offer, notifications.StateAccepted, v))
		}

	case engine.ActionDecline:
		if err := p.executor.DeclineOffer(ctx, offer.ID); err == nil {
			_ = p.notif.SendNotification(ctx, p.makeNotifInfo(offer, notifications.StateDeclined, v))
			_ = p.reviewer.SendDeclinedAlert(ctx, offer.ID, offer.OtherSteamID, p.makeReviewMeta(v, duration), nil)
		}

	case engine.ActionReview:
		p.logger.Info("Offer sent to manual review", log.Uint64("id", offer.ID))
		_ = p.notif.SendNotification(ctx, p.makeNotifInfo(offer, notifications.StateActive, v))
		_ = p.reviewer.SendReviewAlert(ctx, offer.ID, offer.OtherSteamID, p.makeReviewMeta(v, duration))

	case engine.ActionIgnore:
		p.logger.Debug("Offer ignored by engine", log.Uint64("id", offer.ID))
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
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, item := range append(offer.ItemsToGive, offer.ItemsToReceive...) {
		if _, busy := p.busyItems[item.AssetID]; busy {
			return true
		}
	}

	return false
}

func (p *Processor) lockItems(offer *trading.TradeOffer) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, item := range append(offer.ItemsToGive, offer.ItemsToReceive...) {
		p.busyItems[item.AssetID] = offer.ID
	}
}

func (p *Processor) unlockItems(offer *trading.TradeOffer) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, item := range append(offer.ItemsToGive, offer.ItemsToReceive...) {
		delete(p.busyItems, item.AssetID)
	}
}
