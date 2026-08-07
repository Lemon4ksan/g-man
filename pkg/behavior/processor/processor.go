// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package processor coordinates sequential trade offer processing, asset locking, and verdict execution.
package processor

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lemon4ksan/miyako/bus"
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

// ProcessTrades registers trade processing behavior with the client orchestrator.
func ProcessTrades(client *steam.Client, eng *engine.Engine, n *notifications.Manager, r *review.Reviewer) {
	behavior.From(client).Register(New(web.From(client), eng, n, r, client.Bus(), client.Logger()))
}

// TradeExecutor executes accepting and declining operations against Steam.
type TradeExecutor interface {
	AcceptOffer(ctx context.Context, id uint64) error
	DeclineOffer(ctx context.Context, id uint64) error
}

// Processor manages sequential processing of incoming trade offers and enforces asset locks to prevent concurrent trade conflicts.
type Processor struct {
	executor TradeExecutor
	engine   *engine.Engine
	notif    *notifications.Manager
	reviewer *review.Reviewer
	logger   log.Logger
	bus      *bus.Bus

	queue chan *trading.TradeOffer

	itemLocks *keylock.KeyMutex[uint64]

	processing sync.Map
}

// New constructs a trade Processor instance.
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
	}
}

// Name returns behavior name "trade_processor".
func (p *Processor) Name() string {
	return "trade_processor"
}

// Run launches event listeners and worker goroutines.
func (p *Processor) Run(ctx context.Context) error {
	sub := p.bus.Subscribe(&web.NewOfferEvent{})
	defer sub.Unsubscribe()

	go p.worker(ctx)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case ev, ok := <-sub.C():
			if !ok {
				return nil
			}

			if offerEv, ok := ev.(*web.NewOfferEvent); ok {
				p.logger.Info("New active trade offer received from event bus",
					log.Uint64("offer_id", offerEv.Offer.ID),
					log.Uint64("partner_steam_id", uint64(offerEv.Offer.OtherSteamID)),
				)
				p.Enqueue(offerEv.Offer)
			}
		}
	}
}

// Enqueue adds an offer to the processing queue if not currently handled.
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

var corrBufPool = sync.Pool{
	New: func() any { return new(strings.Builder) },
}

func generateCorrelationID(offerID uint64) string {
	sb := corrBufPool.Get().(*strings.Builder)

	sb.Reset()
	defer corrBufPool.Put(sb)

	sb.WriteString("offer-")

	var intBuf [20]byte
	sb.Write(strconv.AppendUint(intBuf[:0], offerID, 10))
	sb.WriteByte('-')

	corrSuffix := log.GenerateCorrelationID()
	if len(corrSuffix) > 8 {
		corrSuffix = corrSuffix[:8]
	}

	sb.WriteString(corrSuffix)

	return sb.String()
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

func (p *Processor) handleOffer(ctx context.Context, offer *trading.TradeOffer) {
	defer p.processing.Delete(offer.ID)

	start := time.Now()
	ctx = log.WithCorrelationID(ctx, generateCorrelationID(offer.ID))

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
	for _, item := range offer.ItemsToGive {
		if item != nil && p.itemLocks.IsLocked(item.AssetID) {
			return true
		}
	}

	for _, item := range offer.ItemsToReceive {
		if item != nil && p.itemLocks.IsLocked(item.AssetID) {
			return true
		}
	}

	return false
}

func (p *Processor) lockItems(offer *trading.TradeOffer) {
	totalLen := len(offer.ItemsToGive) + len(offer.ItemsToReceive)
	if totalLen == 0 {
		return
	}

	var (
		stackIDs [32]uint64
		ids      []uint64
	)

	if totalLen <= len(stackIDs) {
		ids = stackIDs[:0]
	} else {
		ids = make([]uint64, 0, totalLen)
	}

	for _, item := range offer.ItemsToGive {
		ids = append(ids, item.AssetID)
	}

	for _, item := range offer.ItemsToReceive {
		ids = append(ids, item.AssetID)
	}

	slices.Sort(ids)

	for _, id := range ids {
		p.itemLocks.Lock(id)
	}
}

func (p *Processor) unlockItems(offer *trading.TradeOffer) {
	totalLen := len(offer.ItemsToGive) + len(offer.ItemsToReceive)
	if totalLen == 0 {
		return
	}

	var (
		stackIDs [32]uint64
		ids      []uint64
	)

	if totalLen <= len(stackIDs) {
		ids = stackIDs[:0]
	} else {
		ids = make([]uint64, 0, totalLen)
	}

	for _, item := range offer.ItemsToGive {
		ids = append(ids, item.AssetID)
	}

	for _, item := range offer.ItemsToReceive {
		ids = append(ids, item.AssetID)
	}

	slices.Sort(ids)

	for _, id := range ids {
		p.itemLocks.Unlock(id)
	}
}
