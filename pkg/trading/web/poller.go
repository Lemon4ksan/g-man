// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package web

import (
	"context"
	"maps"
	"time"

	"github.com/lemon4ksan/miyako/bus"
	"github.com/lemon4ksan/miyako/log"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/notifications"
	"github.com/lemon4ksan/g-man/pkg/trading"
)

// GetPollData snapshots active polling state for persistence.
func (m *Manager) GetPollData() trading.PollData {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sent := make(map[uint64]trading.OfferState, len(m.sentOffers))
	maps.Copy(sent, m.sentOffers)

	received := make(map[uint64]trading.OfferState, len(m.receivedOffers))
	maps.Copy(received, m.receivedOffers)

	return trading.PollData{
		OffersSince: m.offersSince,
		Sent:        sent,
		Received:    received,
	}
}

// SetPollData restores polling state from persistence.
func (m *Manager) SetPollData(data trading.PollData) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.offersSince = data.OffersSince

	m.sentOffers = make(map[uint64]trading.OfferState)
	maps.Copy(m.sentOffers, data.Sent)

	m.receivedOffers = make(map[uint64]trading.OfferState)
	maps.Copy(m.receivedOffers, data.Received)
}

func (m *Manager) pollingLoop(ctx context.Context) {
	ticker := time.NewTicker(m.config.PollInterval)
	defer ticker.Stop()

	for {
		if m.fsm.CurrentState() != StatePolling {
			return
		}

		select {
		case <-ctx.Done():
			return

		case <-m.trigger:
			time.Sleep(1 * time.Second)
			m.doPoll(ctx)

		case <-ticker.C:
			if m.fsm.CurrentState() != StatePolling {
				return
			}

			m.doPoll(ctx)
		}
	}
}

func (m *Manager) doPoll(ctx context.Context) {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return
	}

	m.Logger.Debug("Polling trade offers...")

	m.mu.RLock()

	cutoff := time.Now().Add(-24 * time.Hour).Unix()
	if m.offersSince > 0 {
		cutoff = m.offersSince - 1800
	}

	m.mu.RUnlock()

	req := getOffersReq{
		GetReceivedOffers:    1,
		GetSentOffers:        1,
		ActiveOnly:           1,
		GetDescriptions:      1,
		TimeHistoricalCutoff: cutoff,
	}

	resp, err := service.WebAPI[getOffersResp](ctx, m.web, "GET", "IEconService", "GetTradeOffers", 1, req)
	if err != nil {
		if ctx.Err() == nil {
			m.Logger.Warn("Trade poll failed", log.Err(err))
		}

		return
	}

	for _, o := range resp.Sent {
		if o != nil {
			mapDescriptionsToOffer(o, resp.Descriptions)
			_ = m.enricher.EnrichOffer(ctx, m.web, m.config.AppID, m.config.Language, o)
		}
	}

	for _, o := range resp.Received {
		if o != nil {
			mapDescriptionsToOffer(o, resp.Descriptions)
			_ = m.enricher.EnrichOffer(ctx, m.web, m.config.AppID, m.config.Language, o)
		}
	}

	m.mu.Lock()

	now := time.Now()
	allOffers := make([]*trading.TradeOffer, 0, len(resp.Sent)+len(resp.Received))

	for _, off := range resp.Sent {
		if off != nil {
			allOffers = append(allOffers, off)
		}
	}

	for _, off := range resp.Received {
		if off != nil {
			allOffers = append(allOffers, off)
		}
	}

	pollDataChanged := false

	for _, off := range allOffers {
		m.lastSeenOffers[off.ID] = now

		var (
			oldState trading.OfferState
			exists   bool
		)

		if off.IsOurOffer {
			oldState, exists = m.sentOffers[off.ID]
			m.sentOffers[off.ID] = off.State
		} else {
			oldState, exists = m.receivedOffers[off.ID]
			m.receivedOffers[off.ID] = off.State
		}

		if !exists && off.State == trading.OfferStateActive {
			pollDataChanged = true

			m.Bus.Publish(&NewOfferEvent{Offer: off})

			if m.processor != nil {
				m.processor.Enqueue(off)
			}
		} else if oldState != off.State {
			pollDataChanged = true

			m.Bus.Publish(&OfferChangedEvent{
				Offer:    off,
				OldState: oldState,
			})
		}

		if off.IsOurOffer && off.State == trading.OfferStateActive {
			m.canceller.PushActiveSent(off)
		}
	}

	latest := m.offersSince

	for _, off := range allOffers {
		if off.TimeUpdated > latest {
			latest = off.TimeUpdated
			pollDataChanged = true
		}
	}

	m.offersSince = latest
	m.gcKnownOffers(now)

	if pollDataChanged {
		newPollData := trading.PollData{
			OffersSince: m.offersSince,
			Sent:        make(map[uint64]trading.OfferState, len(m.sentOffers)),
			Received:    make(map[uint64]trading.OfferState, len(m.receivedOffers)),
		}

		maps.Copy(newPollData.Sent, m.sentOffers)
		maps.Copy(newPollData.Received, m.receivedOffers)

		m.Bus.Publish(&PollDataEvent{
			PollData: newPollData,
		})
	}

	m.mu.Unlock()

	m.canceller.HandleAutoCancellation(ctx, m.config, resp.Sent, m.sentOffers, &m.mu, m.CancelOffer)

	m.Logger.Debug("Trade poll completed",
		log.Int("sent_active", len(resp.Sent)),
		log.Int("received_active", len(resp.Received)),
	)
}

func (m *Manager) gcKnownOffers(now time.Time) {
	for id, lastSeen := range m.lastSeenOffers {
		if now.Sub(lastSeen) > 1*time.Hour {
			if state, ok := m.sentOffers[id]; ok && state != trading.OfferStateActive {
				delete(m.sentOffers, id)
				delete(m.lastSeenOffers, id)
			} else if state, ok := m.receivedOffers[id]; ok && state != trading.OfferStateActive {
				delete(m.receivedOffers, id)
				delete(m.lastSeenOffers, id)
			}
		}
	}
}

func (m *Manager) listenEvents(ctx context.Context, sub *bus.Subscription) {
	defer sub.Unsubscribe()

	for {
		select {
		case <-ctx.Done():
			return

		case ev, ok := <-sub.C():
			if !ok {
				return
			}

			if e, ok := ev.(*auth.StateEvent); ok {
				if e.New == auth.StateDisconnected {
					m.StopPolling()
				}
			}
		}
	}
}

func (m *Manager) listenNotifications(ctx context.Context, sub *bus.Subscription) {
	defer sub.Unsubscribe()

	for {
		select {
		case <-ctx.Done():
			return

		case ev, ok := <-sub.C():
			if !ok {
				return
			}

			switch e := ev.(type) {
			case *notifications.UserNotificationsEvent:
				if count, exists := e.Notifications[notifications.NotificationTradeOffer]; exists && count > 0 {
					m.Logger.Debug("Trade offer notification received, triggering poll", log.Uint32("count", count))
					m.TriggerPoll()
				}

			case *notifications.ReceivedEvent:
				hasTradeOffer := false

				for _, notif := range e.Notifications {
					if notif.GetNotificationType() == pb.ESteamNotificationType_k_ESteamNotificationType_TradeOffer {
						hasTradeOffer = true
						break
					}
				}

				if hasTradeOffer {
					m.Logger.Debug("Trade offer notification received, triggering poll")
					m.TriggerPoll()
				}
			}
		}
	}
}
