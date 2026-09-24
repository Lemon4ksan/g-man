// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package web

import (
	"bytes"
	"fmt"
	"net/url"
	"strconv"

	"github.com/lemon4ksan/aoni/x/codec/values"
	"github.com/lemon4ksan/foundation/codec/json"
	"github.com/lemon4ksan/foundation/silicon/pool"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/trading"
)

type descKey = uint64

func packDescKey(classID, instanceID uint64) descKey {
	return descKey((classID << 32) | (instanceID & 0xFFFFFFFF))
}

func newDescKey(classID, instanceID string) descKey {
	cID, _ := strconv.ParseUint(classID, 10, 64)
	instID, _ := strconv.ParseUint(instanceID, 10, 64)

	return packDescKey(cID, instID)
}

type tradeOfferObj struct {
	NewVersion bool       `json:"newversion"`
	Version    int        `json:"version"`
	Me         sideObject `json:"me"`
	Them       sideObject `json:"them"`
}

type steamObject struct {
	AppID     uint32 `json:"appid"`
	ContextID string `json:"contextid"`
	Amount    int64  `json:"amount"`
	AssetID   string `json:"assetid"`
}

type sideObject struct {
	Assets   []steamObject `json:"assets"`
	Currency []any         `json:"currency"`
	Ready    bool          `json:"ready"`
}

type createParams struct {
	TradeOfferAccessToken string `json:"trade_offer_access_token"`
}

type sendNewReq struct {
	ServerID     int    `query:"serverid"`
	PartnerID    id.ID  `query:"partner"`
	Captcha      string `query:"captcha"`
	Message      string `query:"tradeoffermessage"`
	JSON         string `query:"json_tradeoffer"`
	CreateParams string `query:"trade_offer_create_params,omitempty"`
	CounteredID  uint64 `query:"tradeofferid_countered,omitempty"`
}

var formBufferPool = pool.NewPerPStorage(func() any {
	return new(bytes.Buffer)
})

// EncodeFormString serializes sendNewReq into application/x-www-form-urlencoded format.
//
// Invariant: Steam Community POST "/tradeoffer/new/send" strictly requires the parameters
// `serverid=1`, `partner` (64-bit SteamID string), `tradeoffermessage`, `json_tradeoffer`,
// and `captcha=""`.
//
// Parity: matches @tf2autobot/tradeoffer-manager (lib/classes/TradeOffer.js: send).
func (r sendNewReq) EncodeFormString() (string, error) {
	normPartnerID, err := NormalizePartnerSteamID(r.PartnerID)
	if err != nil {
		return "", fmt.Errorf("send offer form encode: %w", err)
	}

	buf := formBufferPool.Get().(*bytes.Buffer)
	buf.Reset()

	defer formBufferPool.Put(buf)

	var intBuf [20]byte

	buf.WriteString("serverid=")
	buf.Write(strconv.AppendInt(intBuf[:0], int64(r.ServerID), 10))

	buf.WriteString("&partner=")
	buf.Write(strconv.AppendUint(intBuf[:0], uint64(normPartnerID), 10))

	buf.WriteString("&tradeoffermessage=")
	buf.WriteString(url.QueryEscape(r.Message))

	buf.WriteString("&json_tradeoffer=")
	buf.WriteString(url.QueryEscape(r.JSON))

	buf.WriteString("&captcha=")

	if r.Captcha != "" {
		buf.WriteString(url.QueryEscape(r.Captcha))
	}

	if r.CreateParams != "" {
		buf.WriteString("&trade_offer_create_params=")
		buf.WriteString(url.QueryEscape(r.CreateParams))
	}

	if r.CounteredID > 0 {
		buf.WriteString("&tradeofferid_countered=")
		buf.Write(strconv.AppendUint(intBuf[:0], r.CounteredID, 10))
	}

	return buf.String(), nil
}

type sendNewResponse struct {
	TradeOfferID string `json:"tradeofferid"`
	NeedsMobile  bool   `json:"needs_mobile_confirmation"`
	NeedsEmail   bool   `json:"needs_email_confirmation"`
}

type acceptOfferReq struct {
	ServerID     int    `query:"serverid"`
	TradeOfferID uint64 `query:"tradeofferid"`
	Partner      string `query:"partner"`
	Captcha      string `query:"captcha"`
}

// EncodeFormString serializes acceptOfferReq into application/x-www-form-urlencoded format.
//
// Invariant: Steam Community POST "/tradeoffer/{id}/accept" strictly requires
// `serverid=1`, `tradeofferid`, `partner` (SteamID64), and `captcha=""`.
//
// Parity: matches @tf2autobot/tradeoffer-manager (lib/classes/TradeOffer.js: accept).
func (r acceptOfferReq) EncodeFormString() (string, error) {
	buf := formBufferPool.Get().(*bytes.Buffer)

	buf.Reset()
	defer formBufferPool.Put(buf)

	var intBuf [20]byte

	buf.WriteString("serverid=")
	buf.Write(strconv.AppendInt(intBuf[:0], int64(r.ServerID), 10))

	buf.WriteString("&tradeofferid=")
	buf.Write(strconv.AppendUint(intBuf[:0], r.TradeOfferID, 10))

	buf.WriteString("&partner=")
	buf.WriteString(url.QueryEscape(r.Partner))

	buf.WriteString("&captcha=")

	if r.Captcha != "" {
		buf.WriteString(url.QueryEscape(r.Captcha))
	}

	return buf.String(), nil
}

type acceptResponse struct {
	TradeID                 string `json:"tradeid"`
	NeedsMobileConfirmation bool   `json:"needs_mobile_confirmation"`
	NeedsEmailConfirmation  bool   `json:"needs_email_confirmation"`
	EmailDomain             string `json:"email_domain"`
}

type tradeStatusReq struct {
	TradeID         uint64 `query:"tradeid"`
	GetDescriptions bool   `query:"get_descriptions"`
	Language        string `query:"language"`
}

type tradeStatusResp struct {
	Trades []struct {
		TradeID        uint64                  `json:"tradeid,string"`
		SteamIDOther   uint64                  `json:"steamid_other,string"`
		TimeInit       int64                   `json:"time_init"`
		Status         int                     `json:"status"`
		AssetsReceived []trading.ExchangeAsset `json:"assets_received"`
		AssetsGiven    []trading.ExchangeAsset `json:"assets_given"`
	} `json:"trades"`
}

type getOfferReq struct {
	TradeOfferID    uint64 `query:"tradeofferid"`
	GetDescriptions bool   `query:"get_descriptions"`
	Language        string `query:"language"`
}

type getOffersReq struct {
	GetReceivedOffers    int   `query:"get_received_offers"`
	GetSentOffers        int   `query:"get_sent_offers"`
	ActiveOnly           int   `query:"active_only"`
	GetDescriptions      int   `query:"get_descriptions"`
	TimeHistoricalCutoff int64 `query:"time_historical_cutoff"`
}

type getOffersResp struct {
	Sent         []*trading.TradeOffer `json:"trade_offers_sent"`
	Received     []*trading.TradeOffer `json:"trade_offers_received"`
	Descriptions []rawDescription      `json:"descriptions"`
}

type getAssetClassInfoResponse struct {
	Result map[string]json.RawMessage `json:"result"`
}

type rawDescription struct {
	AppID          uint32                `json:"appid"`
	ClassID        string                `json:"classid"`
	InstanceID     string                `json:"instanceid"`
	Name           string                `json:"name"`
	NameColor      string                `json:"name_color"`
	Type           string                `json:"type"`
	MarketName     string                `json:"market_name"`
	MarketHashName string                `json:"market_hash_name"`
	IconURL        string                `json:"icon_url"`
	Tradable       values.BoolInt        `json:"tradable"`
	Marketable     values.BoolInt        `json:"marketable"`
	Descriptions   []trading.Description `json:"descriptions"`
	Tags           []trading.Tag         `json:"tags"`
	Actions        []trading.Action      `json:"actions"`
}

type assetClassTag struct {
	Category              string `json:"category"`
	InternalName          string `json:"internal_name"`
	LocalizedCategoryName string `json:"localized_category_name"`
	LocalizedTagName      string `json:"localized_tag_name"`
	Name                  string `json:"name"`
}

type rawAssetClassDescription struct {
	ClassID        string               `json:"classid"`
	InstanceID     string               `json:"instanceid"`
	Name           string               `json:"name"`
	MarketName     string               `json:"market_name"`
	Type           string               `json:"type"`
	MarketHashName string               `json:"market_hash_name"`
	IconURL        string               `json:"icon_url"`
	Descriptions   flexibleDescriptions `json:"descriptions"`
	Tags           flexibleTags         `json:"tags"`
	Tradable       values.BoolInt       `json:"tradable"`
	Marketable     values.BoolInt       `json:"marketable"`
}
