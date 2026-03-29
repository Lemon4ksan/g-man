// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package econ

type TradeOfferState int32

const (
	TradeOfferStateInvalid                  TradeOfferState = 1
	TradeOfferStateActive                   TradeOfferState = 2
	TradeOfferStateAccepted                 TradeOfferState = 3
	TradeOfferStateCountered                TradeOfferState = 4
	TradeOfferStateExpired                  TradeOfferState = 5
	TradeOfferStateCanceled                 TradeOfferState = 6
	TradeOfferStateDeclined                 TradeOfferState = 7
	TradeOfferStateInvalidItems             TradeOfferState = 8
	TradeOfferStateCreatedNeedsConfirmation TradeOfferState = 9
	TradeOfferStateCanceledBySecondFactor   TradeOfferState = 10
	TradeOfferStateInEscrow                 TradeOfferState = 11
)

type Attribute struct {
	Defindex   int     `json:"defindex"`
	Value      string  `json:"value"`
	FloatValue float64 `json:"float_value"`
}

type Description struct {
	Value   string `json:"value"`
	Color   string `json:"color,omitempty"`
	AppData *struct {
		Defindex int `json:"def_index,string"`
	} `json:"app_data,omitempty"`
}

type Tag struct {
	Category     string `json:"category"`
	InternalName string `json:"internal_name"`
	Localized    string `json:"localized_category_name"`
}

type Action struct {
	Link string `json:"link"`
	Name string `json:"name"`
}

type Item struct {
	// Identity
	AppID        uint32        `json:"appid"`
	ContextID    int64         `json:"contextid,string"`
	AssetID      uint64        `json:"assetid,string"`
	ClassID      uint64        `json:"classid,string"`
	InstanceID   uint64        `json:"instanceid,string"`
	Amount       int64         `json:"amount,string"`
	Missing      bool          `json:"missing"`
	Descriptions []Description `json:"descriptions"`
	Tags         []Tag         `json:"tags"`
	Actions      []Action      `json:"actions"`

	// Description (Filled by Asset Cache or API)
	Name           string `json:"name"`
	NameColor      string `json:"name_color"`
	Type           string `json:"type"`
	MarketName     string `json:"market_name"`
	MarketHashName string `json:"market_hash_name"`
	IconURL        string `json:"icon_url"`
	Tradable       bool   `json:"tradable"`
	Marketable     bool   `json:"marketable"`

	// External fields
	SKU        string      `json:"sku,omitempty"`
	Attributes []Attribute `json:"attributes,omitempty"`
}
