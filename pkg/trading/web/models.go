// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package web

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

	"github.com/lemon4ksan/aoni"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/trading"
)

type descKey struct {
	ClassID    uint64
	InstanceID uint64
}

func newDescKey(classID, instanceID string) descKey {
	cID, _ := strconv.ParseUint(classID, 10, 64)
	instID, _ := strconv.ParseUint(instanceID, 10, 64)
	return descKey{ClassID: cID, InstanceID: instID}
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
	Tradable       aoni.BoolInt          `json:"tradable"`
	Marketable     aoni.BoolInt          `json:"marketable"`
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

func unmarshalFlexibleArray[T any](data []byte) ([]T, error) {
	if len(data) == 0 {
		return nil, nil
	}

	switch data[0] {
	case '"':
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, err
		}

		return nil, nil

	case '[':
		var arr []T
		if err := json.Unmarshal(data, &arr); err != nil {
			return nil, err
		}

		return arr, nil

	case '{':
		var m map[string]T
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}

		type indexedItem struct {
			idx int
			val T
		}

		items := make([]indexedItem, 0, len(m))
		for k, v := range m {
			idx, err := strconv.Atoi(k)
			if err != nil {
				continue
			}

			items = append(items, indexedItem{idx: idx, val: v})
		}

		slices.SortFunc(items, func(a, b indexedItem) int {
			return a.idx - b.idx
		})

		res := make([]T, len(items))
		for i, item := range items {
			res[i] = item.val
		}

		return res, nil

	default:
		return nil, fmt.Errorf("failed to unmarshal flexible array: %s", string(data))
	}
}

type flexibleDescriptions []trading.Description

func (fd *flexibleDescriptions) UnmarshalJSON(data []byte) error {
	res, err := unmarshalFlexibleArray[trading.Description](data)
	if err != nil {
		return err
	}

	*fd = res

	return nil
}

type flexibleTags []assetClassTag

func (ft *flexibleTags) UnmarshalJSON(data []byte) error {
	res, err := unmarshalFlexibleArray[assetClassTag](data)
	if err != nil {
		return err
	}

	*ft = res

	return nil
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
	Tradable       aoni.BoolInt         `json:"tradable"`
	Marketable     aoni.BoolInt         `json:"marketable"`
}
