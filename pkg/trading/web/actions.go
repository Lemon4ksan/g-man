// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package web

import (
	"context"
	"errors"
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
//
// Invariant: Steam Community POST "/tradeoffer/new/send" strictly expects form-encoded parameters:
// `serverid=1`, `partner` (64-bit SteamID), `tradeoffermessage`, `json_tradeoffer`, and `captcha=""`.
// When sending with a trade token, `trade_offer_create_params` must contain `{"trade_offer_access_token":"token"}`.
// Headers `Referer` and `Origin` must point to `https://steamcommunity.com`.
//
// Parity: matches node-steam-tradeoffer-manager (lib/classes/TradeOffer.js: send).
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

	partnerID, err := NormalizePartnerSteamID(p.PartnerID)
	if err != nil {
		return 0, fmt.Errorf("send offer: %w", err)
	}

	payload := sendNewReq{
		ServerID:     1,
		PartnerID:    partnerID,
		Captcha:      "",
		Message:      p.Message,
		JSON:         bytesconv.B2S(jsonBytes),
		CreateParams: paramsStr,
		CounteredID:  p.CounteredID,
	}

	referer := fmt.Sprintf("https://steamcommunity.com/tradeoffer/new/?partner=%d", partnerID.AccountID())
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

	if resp == nil {
		return 0, ErrEmptyResponse
	}

	if resp != nil && (resp.NeedsMobile || resp.NeedsEmail) {
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

// NormalizePartnerSteamID normalizes a partner SteamID into an individual 64-bit SteamID.
// If partnerID is a 32-bit AccountID (< id.FromAccountID(0)) and non-zero, it converts it via id.FromAccountID.
// If partnerID is 0, it fails fast with an error immediately to prevent sending "partner: 0" and HTTP 403 Forbidden.
//
// Invariant: Steam Community POST "/tradeoffer/{offerID}/accept" and "/tradeoffer/new/send" strictly
// require the partner's 64-bit individual SteamID string. Passing "0" triggers an immediate HTTP 403 Forbidden.
//
// Parity: matches node-steam-tradeoffer-manager (lib/classes/TradeOffer.js).
func NormalizePartnerSteamID(partnerID id.ID) (id.ID, error) {
	if partnerID == 0 {
		return 0, errors.New("trading: partner SteamID cannot be zero")
	}

	if partnerID < id.FromAccountID(0) {
		partnerID = id.FromAccountID(uint32(partnerID))
	}

	if partnerID == 0 {
		return 0, errors.New("trading: partner SteamID cannot be zero after normalization")
	}

	return partnerID, nil
}

// AcceptOffer accepts an incoming active trade offer, automatically discovering the partner ID.
func (m *Manager) AcceptOffer(ctx context.Context, offerID uint64) error {
	return m.AcceptOfferWithPartner(ctx, offerID, 0)
}

// AcceptOfferWithPartner accepts an incoming active trade offer with a known partner SteamID.
//
// Invariant: Steam Community POST "/tradeoffer/{offerID}/accept" endpoint requires
// the partner's 64-bit SteamID string (`partner`). Omitting this parameter or passing "0"
// causes Steam to reject the request with HTTP 403 Forbidden.
//
// Form payloads must also include the empty `captcha=` field.
//
// If Steam responds with an HTTP error (e.g. 403, 500, 502, or timeout), the offer may have
// already transitioned to OfferStateCreatedNeedsConfirmation (State 9) on Steam's servers.
// Verifying actual state via GetOffer prevents falsely failing trades that only require confirmation.
//
// Parity: matches node-steam-tradeoffer-manager (lib/classes/TradeOffer.js: accept).
func (m *Manager) AcceptOfferWithPartner(ctx context.Context, offerID uint64, partnerID id.ID) error {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return err
	}

	comm := m.Community()
	if comm == nil {
		return ErrCommunityNotReady
	}

	if partnerID == 0 {
		if offer, getErr := m.GetOffer(ctx, offerID); getErr == nil && offer != nil {
			partnerID = offer.OtherSteamID
		}
	}

	normPartnerID, err := NormalizePartnerSteamID(partnerID)
	if err != nil {
		return fmt.Errorf("accept offer %d: %w", offerID, err)
	}

	req := struct {
		ServerID     int    `query:"serverid"`
		TradeOfferID uint64 `query:"tradeofferid"`
		Partner      string `query:"partner"`
		Captcha      string `query:"captcha"`
	}{
		ServerID:     1,
		TradeOfferID: offerID,
		Partner:      strconv.FormatUint(normPartnerID.Uint64(), 10),
		Captcha:      "",
	}

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

			if offer.State == trading.OfferStateCreatedNeedsConfirmation {
				m.Logger.Info("Trade offer needs mobile confirmation despite HTTP error response",
					log.Uint64("offer_id", offerID),
				)
				m.Bus.Publish(&guard.ConfirmationRequiredEvent{
					TradeOfferID: strconv.FormatUint(offerID, 10),
					IsAppConfirm: true,
				})

				return nil
			}
		}

		return err
	}

	if resp == nil {
		return nil
	}

	if resp != nil && (resp.NeedsMobileConfirmation || resp.NeedsEmailConfirmation) {
		m.Bus.Publish(&guard.ConfirmationRequiredEvent{
			TradeOfferID: strconv.FormatUint(offerID, 10),
			IsAppConfirm: resp.NeedsMobileConfirmation,
			IsEmail:      resp.NeedsEmailConfirmation,
			EmailDomain:  resp.EmailDomain,
		})
	}

	return nil
}

type actionOfferResponse struct {
	TradeOfferID string `json:"tradeofferid"`
}

// DeclineOfferCommunity declines an incoming active trade offer via the Steam Community web endpoint.
//
// Invariant: Steam Community POST "/tradeoffer/{offerID}/decline" allows rejecting offers using
// web session cookies without requiring an IEconService WebAPI key.
//
// Parity: matches node-steam-tradeoffer-manager (lib/classes/TradeOffer.js: decline).
func (m *Manager) DeclineOfferCommunity(ctx context.Context, offerID uint64) error {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return err
	}

	comm := m.Community()
	if comm == nil {
		return ErrCommunityNotReady
	}

	req := struct{}{}
	_, err := community.PostFormTo[actionOfferResponse](
		ctx, comm, "tradeoffer/{offerID}/decline", req,
		mod.WithVar("offerID", offerID),
		mod.WithOrigin("https://steamcommunity.com"),
		mod.WithHeader("Referer", fmt.Sprintf("https://steamcommunity.com/tradeoffer/%d/", offerID)),
	)

	return err
}

// CancelOfferCommunity cancels an active sent trade offer via the Steam Community web endpoint.
//
// Invariant: Steam Community POST "/tradeoffer/{offerID}/cancel" allows canceling sent offers using
// web session cookies without requiring an IEconService WebAPI key.
//
// Parity: matches node-steam-tradeoffer-manager (lib/classes/TradeOffer.js: cancel).
func (m *Manager) CancelOfferCommunity(ctx context.Context, offerID uint64) error {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return err
	}

	comm := m.Community()
	if comm == nil {
		return ErrCommunityNotReady
	}

	req := struct{}{}
	_, err := community.PostFormTo[actionOfferResponse](
		ctx, comm, "tradeoffer/{offerID}/cancel", req,
		mod.WithVar("offerID", offerID),
		mod.WithOrigin("https://steamcommunity.com"),
		mod.WithHeader("Referer", fmt.Sprintf("https://steamcommunity.com/tradeoffer/%d/", offerID)),
	)

	return err
}

// DeclineOffer declines an incoming active trade offer. It attempts WebAPI first,
// and falls back to the Community web decline endpoint if WebAPI encounters an error.
func (m *Manager) DeclineOffer(ctx context.Context, offerID uint64) error {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return err
	}

	req := struct {
		TradeOfferID uint64 `query:"tradeofferid"`
	}{offerID}

	_, err := service.WebAPI[service.NoResponse](ctx, m.web, "POST", "IEconService", "DeclineTradeOffer", 1, req)
	if err != nil && m.Community() != nil {
		if commErr := m.DeclineOfferCommunity(ctx, offerID); commErr == nil {
			return nil
		}
	}

	return err
}

// CancelOffer cancels an active sent trade offer. It attempts WebAPI first,
// and falls back to the Community web cancel endpoint if WebAPI encounters an error.
func (m *Manager) CancelOffer(ctx context.Context, offerID uint64) error {
	if err := m.rateLimiter.Wait(ctx); err != nil {
		return err
	}

	req := struct {
		TradeOfferID uint64 `query:"tradeofferid"`
	}{offerID}

	_, err := service.WebAPI[service.NoResponse](ctx, m.web, "POST", "IEconService", "CancelTradeOffer", 1, req)
	if err != nil && m.Community() != nil {
		if commErr := m.CancelOfferCommunity(ctx, offerID); commErr == nil {
			return nil
		}
	}

	return err
}

// GetExchangeDetails fetches transaction assets and new asset IDs for a completed trade.
//
// Invariant: In EconService/GetTradeStatus, if an accepted offer expected items to be received
// but AssetsReceived is empty, Steam's backend inventory server is experiencing replication lag.
// Callers should treat an empty asset slice on expected transfers as transient ("Data temporarily unavailable").
//
// Parity: matches node-steam-tradeoffer-manager (lib/classes/TradeOffer.js: getExchangeDetails).
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

	if resp == nil || len(resp.Trades) == 0 {
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

	if resp == nil || resp.Offer == nil {
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

	if resp == nil {
		return []trading.TradeOffer{}, nil
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
