// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package web

import (
	"context"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/g-man/pkg/trading"
)

// @aoni:service
// @base_url "https://api.steampowered.com/"
type EconServiceAPI interface {
	// @get "IEconService/GetTradeOffers/v1"
	GetTradeOffers(ctx context.Context, req GetOffersParams, mods ...aoni.RequestModifier) (*GetOffersResponse, error)

	// @get "IEconService/GetTradeOffer/v1"
	GetTradeOffer(ctx context.Context, req GetOfferParams, mods ...aoni.RequestModifier) (*GetOfferResponse, error)

	// @get "IEconService/GetTradeStatus/v1"
	GetTradeStatus(ctx context.Context, req TradeStatusParams, mods ...aoni.RequestModifier) (*TradeStatusResponse, error)

	// @post "IEconService/DeclineTradeOffer/v1"
	// @form
	DeclineTradeOffer(ctx context.Context, req TradeOfferActionParams, mods ...aoni.RequestModifier) error

	// @post "IEconService/CancelTradeOffer/v1"
	// @form
	CancelTradeOffer(ctx context.Context, req TradeOfferActionParams, mods ...aoni.RequestModifier) error
}

// @aoni:service
// @base_url "https://steamcommunity.com/"
// @header "Origin: https://steamcommunity.com"
type TradeCommunityAPI interface {
	// @post "tradeoffer/new/send"
	// @form
	// @header "Referer: https://steamcommunity.com/tradeoffer/new/?partner={partnerID}"
	SendOffer(ctx context.Context, partnerID uint32, req SendNewTradeOfferRequest, mods ...aoni.RequestModifier) (*SendNewTradeOfferResponse, error)

	// @post "tradeoffer/{offerID}/accept"
	// @form
	// @header "Referer: https://steamcommunity.com/tradeoffer/{offerID}/"
	AcceptOffer(ctx context.Context, offerID uint64, req AcceptTradeOfferRequest, mods ...aoni.RequestModifier) (*AcceptTradeOfferResponse, error)
}

// @aoni:dto casing=snake_case
type GetOffersParams struct {
	GetReceivedOffers    int   `json:"get_received_offers" url:"get_received_offers"`
	GetSentOffers        int   `json:"get_sent_offers" url:"get_sent_offers"`
	ActiveOnly           int   `json:"active_only" url:"active_only"`
	GetDescriptions      int   `json:"get_descriptions" url:"get_descriptions"`
	TimeHistoricalCutoff int64 `json:"time_historical_cutoff" url:"time_historical_cutoff"`
}

// @aoni:dto casing=snake_case
type GetOfferParams struct {
	TradeOfferID    uint64 `json:"tradeofferid" url:"tradeofferid"`
	GetDescriptions bool   `json:"get_descriptions" url:"get_descriptions"`
	Language        string `json:"language" url:"language"`
}

// @aoni:dto casing=snake_case
type TradeStatusParams struct {
	TradeID         uint64 `json:"tradeid" url:"tradeid"`
	GetDescriptions bool   `json:"get_descriptions" url:"get_descriptions"`
	Language        string `json:"language" url:"language"`
}

// @aoni:dto casing=snake_case
type TradeOfferActionParams struct {
	TradeOfferID uint64 `json:"tradeofferid" url:"tradeofferid"`
}

// @aoni:dto casing=snake_case
type SendNewTradeOfferRequest struct {
	ServerID               int    `json:"serverid" url:"serverid"`
	Partner                uint64 `json:"partner" url:"partner"`
	TradeOfferMessage      string `json:"tradeoffermessage" url:"tradeoffermessage"`
	JSONTradeOffer         string `json:"json_tradeoffer" url:"json_tradeoffer"`
	TradeOfferCreateParams string `json:"trade_offer_create_params,omitempty" url:"trade_offer_create_params,omitempty"`
	TradeOfferIDCountered  uint64 `json:"tradeofferid_countered,omitempty" url:"tradeofferid_countered,omitempty"`
}

// @aoni:dto casing=snake_case
type AcceptTradeOfferRequest struct {
	ServerID     int    `json:"serverid" url:"serverid"`
	TradeOfferID uint64 `json:"tradeofferid" url:"tradeofferid"`
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
