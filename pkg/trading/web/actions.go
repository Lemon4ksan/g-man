// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package web

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/lemon4ksan/aoni/mod"
	log "github.com/lemon4ksan/foundation/async/logkit"
	"github.com/lemon4ksan/foundation/codec/json"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"

	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/guard"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/trading"
)

// SendOffer sends a new trade offer to partner.
func (m *Manager) SendOffer(ctx context.Context, p trading.OfferParams) (uint64, error) {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return 0, err
	}

	comm := m.Community()
	if comm == nil {
		return 0, ErrUnauthenticatedTrade
	}

	tradeOfferObj := tradeOfferObj{
		NewVersion: true,
		Version:    2,
		Me: sideObject{
			Assets:   make([]steamObject, 0),
			Currency: make([]any, 0),
			Ready:    false,
		},
		Them: sideObject{
			Assets:   make([]steamObject, 0),
			Currency: make([]any, 0),
			Ready:    false,
		},
	}

	var giveAssetIDs []uint64
	for _, it := range p.ItemsToGive {
		giveAssetIDs = append(giveAssetIDs, it.AssetID)
		tradeOfferObj.Me.Assets = append(tradeOfferObj.Me.Assets, steamObject{
			AppID:     it.AppID,
			ContextID: strconv.FormatInt(it.ContextID, 10),
			AssetID:   strconv.FormatUint(it.AssetID, 10),
			Amount:    it.Amount,
		})
	}

	unlock, err := m.ReserveItems(giveAssetIDs...)
	if err != nil {
		return 0, err
	}
	defer unlock()

	for _, it := range p.ItemsToReceive {
		tradeOfferObj.Them.Assets = append(tradeOfferObj.Them.Assets, steamObject{
			AppID:     it.AppID,
			ContextID: strconv.FormatInt(it.ContextID, 10),
			AssetID:   strconv.FormatUint(it.AssetID, 10),
			Amount:    it.Amount,
		})
	}

	jsonBytes, _ := json.Marshal(tradeOfferObj)

	var paramsStr string
	if p.Token != "" {
		paramsObj := createParams{TradeOfferAccessToken: p.Token}
		paramsBytes, _ := json.Marshal(paramsObj)
		paramsStr = bytesconv.B2S(paramsBytes)
	}

	payload := sendNewReq{
		ServerID:     1,
		PartnerID:    p.PartnerID,
		Message:      p.Message,
		JSON:         bytesconv.B2S(jsonBytes),
		CreateParams: paramsStr,
		CounteredID:  p.CounteredID,
	}

	referer := fmt.Sprintf("https://steamcommunity.com/tradeoffer/new/?partner=%d", p.PartnerID.AccountID())
	if p.Token != "" {
		referer = fmt.Sprintf("%s&token=%s", referer, p.Token)
	}

	resp, err := community.PostFormTo[sendNewResponse](
		ctx, comm, "tradeoffer/new/send", payload,
		mod.WithHeader("Referer", referer),
		mod.WithHeader("Origin", "https://steamcommunity.com"),
	)
	if err != nil {
		return 0, err
	}

	if resp.NeedsMobile || resp.NeedsEmail {
		m.Bus.Publish(&guard.ConfirmationRequiredEvent{
			TradeOfferID: resp.TradeOfferID,
			IsAppConfirm: resp.NeedsMobile,
			IsEmail:      resp.NeedsEmail,
		})
	}

	idVal, err := strconv.ParseUint(resp.TradeOfferID, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid trade offer ID in response: %w", err)
	}

	return idVal, nil
}

// AcceptOffer accepts an incoming active trade offer.
func (m *Manager) AcceptOffer(ctx context.Context, offerID uint64) error {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return err
	}

	comm := m.Community()
	if comm == nil {
		return ErrCommunityNotReady
	}

	req := struct {
		ServerID     int    `query:"serverid"`
		TradeOfferID uint64 `query:"tradeofferid"`
	}{1, offerID}

	resp, err := community.PostFormTo[acceptResponse](
		ctx, comm, "tradeoffer/{offerID}/accept", req,
		mod.WithVar("offerID", offerID),
		mod.WithOrigin("https://steamcommunity.com"),
		mod.WithHeader("Referer", fmt.Sprintf("https://steamcommunity.com/tradeoffer/%d/", offerID)),
	)
	if err != nil {
		m.Logger.Warn("Accept trade offer HTTP call returned error, verifying actual offer status",
			log.Uint64("offer_id", offerID),
			log.Err(err),
		)

		if offer, getErr := m.GetOffer(ctx, offerID); getErr == nil && offer != nil {
			if offer.State == trading.OfferStateAccepted || offer.State == trading.OfferStateInEscrow {
				m.Logger.Info("Trade offer was accepted despite HTTP error response",
					log.Uint64("offer_id", offerID),
					log.Int32("state", int32(offer.State)),
				)

				return nil
			}
		}

		return err
	}

	if resp.NeedsMobileConfirmation || resp.NeedsEmailConfirmation {
		m.Bus.Publish(&guard.ConfirmationRequiredEvent{
			TradeOfferID: strconv.FormatUint(offerID, 10),
			IsAppConfirm: resp.NeedsMobileConfirmation,
			IsEmail:      resp.NeedsEmailConfirmation,
			EmailDomain:  resp.EmailDomain,
		})
	}

	return nil
}

// DeclineOffer declines an incoming active trade offer.
func (m *Manager) DeclineOffer(ctx context.Context, offerID uint64) error {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return err
	}

	req := struct {
		TradeOfferID uint64 `query:"tradeofferid"`
	}{offerID}

	_, err := service.WebAPI[service.NoResponse](ctx, m.web, "POST", "IEconService", "DeclineTradeOffer", 1, req)

	return err
}

// CancelOffer cancels an active sent trade offer.
func (m *Manager) CancelOffer(ctx context.Context, offerID uint64) error {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return err
	}

	req := struct {
		TradeOfferID uint64 `query:"tradeofferid"`
	}{offerID}

	_, err := service.WebAPI[service.NoResponse](ctx, m.web, "POST", "IEconService", "CancelTradeOffer", 1, req)

	return err
}

// GetExchangeDetails fetches transaction assets and new asset IDs for a completed trade.
func (m *Manager) GetExchangeDetails(ctx context.Context, tradeID uint64) (*trading.ExchangeDetails, error) {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	req := tradeStatusReq{
		TradeID:         tradeID,
		GetDescriptions: false,
		Language:        m.config.Language,
	}

	resp, err := service.WebAPI[tradeStatusResp](ctx, m.web, "GET", "IEconService", "GetTradeStatus", 1, req)
	if err != nil {
		return nil, err
	}

	if len(resp.Trades) == 0 {
		return nil, fmt.Errorf("%w: %d", ErrOfferNotFound, tradeID)
	}

	t := resp.Trades[0]

	return &trading.ExchangeDetails{
		Status:         t.Status,
		TimeInit:       t.TimeInit,
		AssetsReceived: t.AssetsReceived,
		AssetsGiven:    t.AssetsGiven,
	}, nil
}

// GetOffer fetches details for a single offer.
func (m *Manager) GetOffer(ctx context.Context, offerID uint64) (*trading.TradeOffer, error) {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	req := getOfferReq{
		TradeOfferID:    offerID,
		GetDescriptions: true,
		Language:        m.config.Language,
	}

	type respStruct struct {
		Offer        *trading.TradeOffer `json:"offer"`
		Descriptions []rawDescription    `json:"descriptions"`
	}

	resp, err := service.WebAPI[respStruct](ctx, m.web, "GET", "IEconService", "GetTradeOffer", 1, req)
	if err != nil {
		return nil, err
	}

	if resp.Offer == nil {
		return nil, fmt.Errorf("%w: %d", ErrOfferNotFound, offerID)
	}

	mapDescriptionsToOffer(resp.Offer, resp.Descriptions)
	_ = m.enricher.EnrichOffer(ctx, m.web, m.config.AppID, m.config.Language, resp.Offer)

	return resp.Offer, nil
}

// GetActiveSentOffers fetches all active offers sent by our account.
func (m *Manager) GetActiveSentOffers(ctx context.Context) ([]trading.TradeOffer, error) {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	req := getOffersReq{
		GetReceivedOffers:    0,
		GetSentOffers:        1,
		ActiveOnly:           1,
		GetDescriptions:      1,
		TimeHistoricalCutoff: time.Now().Add(-24 * time.Hour).Unix(),
	}

	type respStruct struct {
		Sent         []*trading.TradeOffer `json:"trade_offers_sent"`
		Descriptions []rawDescription      `json:"descriptions"`
	}

	resp, err := service.WebAPI[respStruct](ctx, m.web, "GET", "IEconService", "GetTradeOffers", 1, req)
	if err != nil {
		return nil, err
	}

	offers := make([]trading.TradeOffer, 0, len(resp.Sent))
	for _, o := range resp.Sent {
		if o != nil {
			mapDescriptionsToOffer(o, resp.Descriptions)
			_ = m.enricher.EnrichOffer(ctx, m.web, m.config.AppID, m.config.Language, o)
			offers = append(offers, *o)
		}
	}

	return offers, nil
}

// ParseTradeURL extracts partner ID and trade token from a trade URL.
func ParseTradeURL(tradeURL string) (id.ID, string, error) {
	u, err := url.Parse(tradeURL)
	if err != nil {
		return 0, "", fmt.Errorf("failed to parse trade URL: %w", err)
	}

	q := u.Query()
	partnerStr := q.Get("partner")

	if partnerStr == "" {
		return 0, "", ErrMissingPartnerParam
	}

	partnerAccountID, err := strconv.ParseUint(partnerStr, 10, 32)
	if err != nil {
		return 0, "", fmt.Errorf("invalid partner ID in trade URL: %w", err)
	}

	partnerID := id.FromAccountID(uint32(partnerAccountID))
	token := q.Get("token")

	return partnerID, token, nil
}
