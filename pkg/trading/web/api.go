// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package web

import (
	"context"

	"github.com/lemon4ksan/aoni"

	"github.com/lemon4ksan/g-man/pkg/trading"
)

// EconServiceAPI defines the Steam econ service API.
//
// @aoni:service casing=snake_case
// @base_url "https://api.steampowered.com/"
type EconServiceAPI interface {
	// @get "IEconService/GetTradeOffers/v1"
	GetTradeOffers(ctx context.Context, req GetOffersParams, mods ...aoni.RequestModifier) (*GetOffersResponse, error)

	// @get "IEconService/GetTradeOffer/v1"
	GetTradeOffer(ctx context.Context, req GetOfferParams, mods ...aoni.RequestModifier) (*GetOfferResponse, error)

	// @get "IEconService/GetTradeStatus/v1"
	GetTradeStatus(
		ctx context.Context,
		req TradeStatusParams,
		mods ...aoni.RequestModifier,
	) (*TradeStatusResponse, error)

	// @post "IEconService/DeclineTradeOffer/v1"
	// @form casing=flatcase
	DeclineTradeOffer(ctx context.Context, tradeOfferID uint64, mods ...aoni.RequestModifier) error

	// @post "IEconService/CancelTradeOffer/v1"
	// @form casing=flatcase
	CancelTradeOffer(ctx context.Context, tradeOfferID uint64, mods ...aoni.RequestModifier) error
}

// TradeCommunityAPI defines the Steam trade community API.
//
// @aoni:service casing=flatcase
// @base_url "https://steamcommunity.com/"
// @header "Origin: https://steamcommunity.com"
type TradeCommunityAPI interface {
	// @header "Referer: https://steamcommunity.com/tradeoffer/new/?partner={partnerID}"
	// @post "tradeoffer/new/send"
	// @form casing=flatcase
	SendOffer(
		ctx context.Context,
		partnerID uint32,
		req SendNewTradeOfferRequest,
		mods ...aoni.RequestModifier,
	) (*SendNewTradeOfferResponse, error)

	// @header "Referer: https://steamcommunity.com/tradeoffer/{offerID}/"
	// @post "tradeoffer/{offerID}/accept"
	// @form casing=flatcase
	AcceptOffer(
		ctx context.Context,
		offerID uint64,
		req AcceptTradeOfferRequest,
		mods ...aoni.RequestModifier,
	) (*AcceptTradeOfferResponse, error)
}

// GetOffersParams defines the parameters for getting trade offers.
//
// @aoni:dto casing=snake_case
type GetOffersParams struct {
	GetReceivedOffers    int   `json:"get_received_offers"    url:"get_received_offers"`
	GetSentOffers        int   `json:"get_sent_offers"        url:"get_sent_offers"`
	ActiveOnly           int   `json:"active_only"            url:"active_only"`
	GetDescriptions      int   `json:"get_descriptions"       url:"get_descriptions"`
	TimeHistoricalCutoff int64 `json:"time_historical_cutoff" url:"time_historical_cutoff"`
}

// GetOfferParams defines the parameters for getting a trade offer.
//
// @aoni:dto casing=snake_case
type GetOfferParams struct {
	TradeOfferID    uint64 `json:"tradeofferid"     url:"tradeofferid"`
	GetDescriptions bool   `json:"get_descriptions" url:"get_descriptions"`
	Language        string `json:"language"         url:"language"`
}

// TradeStatusParams defines the parameters for getting the status of a trade.
//
// @aoni:dto casing=snake_case
type TradeStatusParams struct {
	TradeID         uint64 `json:"tradeid"          url:"tradeid"`
	GetDescriptions bool   `json:"get_descriptions" url:"get_descriptions"`
	Language        string `json:"language"         url:"language"`
}

// TradeOfferActionParams defines the parameters for trading offer actions.
//
// @aoni:dto casing=snake_case
type TradeOfferActionParams struct {
	TradeOfferID uint64 `json:"tradeofferid" url:"tradeofferid"`
}

// SendNewTradeOfferRequest defines the request for sending a new trade offer.
//
// @aoni:dto casing=snake_case
type SendNewTradeOfferRequest struct {
	ServerID               int    `json:"serverid"                            url:"serverid"`
	Partner                uint64 `json:"partner"                             url:"partner"`
	TradeOfferMessage      string `json:"tradeoffermessage"                   url:"tradeoffermessage"`
	JSONTradeOffer         string `json:"json_tradeoffer"                     url:"json_tradeoffer"`
	TradeOfferCreateParams string `json:"trade_offer_create_params,omitempty" url:"trade_offer_create_params,omitempty"`
	TradeOfferIDCountered  uint64 `json:"tradeofferid_countered,omitempty"    url:"tradeofferid_countered,omitempty"`
}

// AcceptTradeOfferRequest defines the request for accepting a trade offer.
//
// Invariant: Steam Community POST "tradeoffer/{offerID}/accept" strictly requires
// the partner's 64-bit SteamID (`partner`) and an empty `captcha=` form field.
// Omitting `partner` or passing "0" causes Steam to return HTTP 403 Forbidden.
//
// Parity: matches node-steam-tradeoffer-manager (lib/classes/TradeOffer.js: accept).
//
// @aoni:dto casing=snake_case
type AcceptTradeOfferRequest struct {
	ServerID     int    `json:"serverid"     url:"serverid"`
	TradeOfferID uint64 `json:"tradeofferid" url:"tradeofferid"`
	Partner      uint64 `json:"partner"      url:"partner"`
	Captcha      string `json:"captcha"      url:"captcha"`
}

type GetOffersResponse struct {
	Sent         []*trading.TradeOffer `json:"trade_offers_sent"`
	Received     []*trading.TradeOffer `json:"trade_offers_received"`
	Descriptions []rawDescription      `json:"descriptions"`
}

type GetOfferResponse struct {
	Offer        *trading.TradeOffer `json:"offer"`
	Descriptions []rawDescription    `json:"descriptions"`
}

type TradeStatusResponse struct {
	Trades []struct {
		TradeID        uint64                  `json:"tradeid,string"`
		SteamIDOther   uint64                  `json:"steamid_other,string"`
		TimeInit       int64                   `json:"time_init"`
		Status         int                     `json:"status"`
		AssetsReceived []trading.ExchangeAsset `json:"assets_received"`
		AssetsGiven    []trading.ExchangeAsset `json:"assets_given"`
	} `json:"trades"`
}

type SendNewTradeOfferResponse struct {
	TradeOfferID string `json:"tradeofferid"`
	NeedsMobile  bool   `json:"needs_mobile_confirmation"`
	NeedsEmail   bool   `json:"needs_email_confirmation"`
}

type AcceptTradeOfferResponse struct {
	TradeID                 string `json:"tradeid"`
	NeedsMobileConfirmation bool   `json:"needs_mobile_confirmation"`
	NeedsEmailConfirmation  bool   `json:"needs_email_confirmation"`
	EmailDomain             string `json:"email_domain"`
}
