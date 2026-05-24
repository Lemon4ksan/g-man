// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trading

import "github.com/lemon4ksan/g-man/pkg/steam/id"

// OfferState represents the state of a trade offer.
type OfferState int32

// Offer state constants.
const (
	OfferStateInvalid                  OfferState = 1
	OfferStateActive                   OfferState = 2
	OfferStateAccepted                 OfferState = 3
	OfferStateCountered                OfferState = 4
	OfferStateExpired                  OfferState = 5
	OfferStateCanceled                 OfferState = 6
	OfferStateDeclined                 OfferState = 7
	OfferStateInvalidItems             OfferState = 8
	OfferStateCreatedNeedsConfirmation OfferState = 9
	OfferStateCanceledBySecondFactor   OfferState = 10
	OfferStateInEscrow                 OfferState = 11
)

// OfferParams represents the parameters for creating an offer.
type OfferParams struct {
	PartnerID      id.ID
	Token          string
	Message        string
	ItemsToGive    []*Item
	ItemsToReceive []*Item
	CounteredID    uint64
}

// Attribute represents an attribute of an item.
type Attribute struct {
	Defindex   int     `json:"defindex"`
	Value      string  `json:"value"`
	FloatValue float64 `json:"float_value"`
}

// Description represents a description for an item.
type Description struct {
	Value   string `json:"value"`
	Color   string `json:"color,omitempty"`
	AppData *struct {
		Defindex int `json:"def_index,string"`
	} `json:"app_data,omitempty"`
}

// Tag represents a tag for an item.
type Tag struct {
	Category      string `json:"category"`
	InternalName  string `json:"internal_name"`
	Localized     string `json:"localized_category_name"`
	LocalizedName string `json:"localized_tag_name"`
}

// Action represents a link and name for an action.
type Action struct {
	Link string `json:"link"`
	Name string `json:"name"`
}

// Item represents a Steam inventory item.
type Item struct {
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

	Name           string `json:"name"`
	NameColor      string `json:"name_color"`
	Type           string `json:"type"`
	MarketName     string `json:"market_name"`
	MarketHashName string `json:"market_hash_name"`
	IconURL        string `json:"icon_url"`
	Tradable       bool   `json:"tradable"`
	Marketable     bool   `json:"marketable"`

	SKU        string      `json:"sku,omitempty"`
	Attributes []Attribute `json:"attributes,omitempty"`
}

// PollData represents the polling state that can be saved and restored.
type PollData struct {
	OffersSince int64                 `json:"offers_since"`
	Sent        map[uint64]OfferState `json:"sent"`
	Received    map[uint64]OfferState `json:"received"`
}

// ExchangeDetails contains information about the details of a completed exchange (receipt).
type ExchangeDetails struct {
	Status         int             `json:"status"`
	TimeInit       int64           `json:"time_init"`
	AssetsReceived []ExchangeAsset `json:"assets_received"`
	AssetsGiven    []ExchangeAsset `json:"assets_given"`
}

// ExchangeAsset represents an item in a completed exchange, including new Asset IDs.
type ExchangeAsset struct {
	AppID        uint32 `json:"appid"`
	ContextID    int64  `json:"contextid,string"`
	AssetID      uint64 `json:"assetid,string"`
	Amount       int64  `json:"amount,string"`
	ClassID      uint64 `json:"classid,string"`
	InstanceID   uint64 `json:"instanceid,string"`
	NewAssetID   uint64 `json:"new_assetid,string"`
	NewContextID int64  `json:"new_contextid,string"`
}
