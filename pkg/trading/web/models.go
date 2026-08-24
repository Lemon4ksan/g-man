// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package web

import (
	"bytes"
	"net/url"
	"strconv"
	"sync"

	"github.com/lemon4ksan/foundation/codec/json"
	"github.com/lemon4ksan/aoni/codec/values"

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
	ServerID     int    `url:"serverid"`
	PartnerID    id.ID  `url:"partner"`
	Message      string `url:"tradeoffermessage"`
	JSON         string `url:"json_tradeoffer"`
	CreateParams string `url:"trade_offer_create_params,omitempty"`
	CounteredID  uint64 `url:"tradeofferid_countered,omitempty"`
}

var formBufferPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

func (r sendNewReq) EncodeFormString() (string, error) {
	buf := formBufferPool.Get().(*bytes.Buffer)
	buf.Reset()

	defer formBufferPool.Put(buf)

	var intBuf [20]byte

	buf.WriteString("serverid=")
	buf.Write(strconv.AppendInt(intBuf[:0], int64(r.ServerID), 10))

	buf.WriteString("&partner=")
	buf.Write(strconv.AppendUint(intBuf[:0], uint64(r.PartnerID), 10))

	buf.WriteString("&tradeoffermessage=")
	buf.WriteString(url.QueryEscape(r.Message))

	buf.WriteString("&json_tradeoffer=")
	buf.WriteString(url.QueryEscape(r.JSON))

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

type acceptResponse struct {
	TradeID                 string `json:"tradeid"`
	NeedsMobileConfirmation bool   `json:"needs_mobile_confirmation"`
	NeedsEmailConfirmation  bool   `json:"needs_email_confirmation"`
	EmailDomain             string `json:"email_domain"`
}

type tradeStatusReq struct {
	TradeID         uint64 `url:"tradeid"`
	GetDescriptions bool   `url:"get_descriptions"`
	Language        string `url:"language"`
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
	TradeOfferID    uint64 `url:"tradeofferid"`
	GetDescriptions bool   `url:"get_descriptions"`
	Language        string `url:"language"`
}

type getOffersReq struct {
	GetReceivedOffers    int   `url:"get_received_offers"`
	GetSentOffers        int   `url:"get_sent_offers"`
	ActiveOnly           int   `url:"active_only"`
	GetDescriptions      int   `url:"get_descriptions"`
	TimeHistoricalCutoff int64 `url:"time_historical_cutoff"`
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
