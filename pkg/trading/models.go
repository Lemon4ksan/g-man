// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trading

import (
	"bytes"

	"github.com/lemon4ksan/foundation/codec/json"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
)

type OfferState int32

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

type OfferParams struct {
	PartnerID      id.ID   `json:"partner_id"`
	Token          string  `json:"token,omitempty"`
	Message        string  `json:"message,omitempty"`
	ItemsToGive    []*Item `json:"items_to_give,omitempty"`
	ItemsToReceive []*Item `json:"items_to_receive,omitempty"`
	CounteredID    uint64  `json:"countered_id,omitempty"`
}

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

func (d *Description) UnmarshalJSON(data []byte) error {
	type plainDescription struct {
		Value string `json:"value"`
		Color string `json:"color,omitempty"`
	}

	var pd plainDescription
	if err := json.UnmarshalNoCopy(data, &pd); err != nil {
		return err
	}

	d.Value = pd.Value
	d.Color = pd.Color

	if bytes.Contains(data, []byte(`"app_data"`)) {
		var appDataWrapper struct {
			AppData json.RawMessage `json:"app_data"`
		}

		if err := json.UnmarshalNoCopy(
			data,
			&appDataWrapper,
		); err == nil && len(appDataWrapper.AppData) > 0 &&
			appDataWrapper.AppData[0] == '{' {
			var appData struct {
				Defindex int `json:"def_index,string"`
			}

			if err := json.UnmarshalNoCopy(appDataWrapper.AppData, &appData); err == nil {
				d.AppData = &struct {
					Defindex int `json:"def_index,string"`
				}{Defindex: appData.Defindex}
			}
		}
	}

	return nil
}

type Tag struct {
	Category      string `json:"category"`
	InternalName  string `json:"internal_name"`
	Localized     string `json:"localized_category_name"`
	LocalizedName string `json:"localized_tag_name"`
}

type Action struct {
	Link string `json:"link"`
	Name string `json:"name"`
}

type Item struct {
	AppID          uint32        `json:"appid"`
	ContextID      int64         `json:"contextid,string"`
	AssetID        uint64        `json:"assetid,string"`
	ClassID        uint64        `json:"classid,string"`
	InstanceID     uint64        `json:"instanceid,string"`
	Amount         int64         `json:"amount,string"`
	Missing        bool          `json:"missing"`
	Descriptions   []Description `json:"descriptions"`
	Tags           []Tag         `json:"tags"`
	Actions        []Action      `json:"actions"`
	Name           string        `json:"name"`
	NameColor      string        `json:"name_color"`
	Type           string        `json:"type"`
	MarketName     string        `json:"market_name"`
	MarketHashName string        `json:"market_hash_name"`
	IconURL        string        `json:"icon_url"`
	Tradable       bool          `json:"tradable"`
	Marketable     bool          `json:"marketable"`
	SKU            string        `json:"sku,omitempty"`
	Attributes     []Attribute   `json:"attributes,omitempty"`
}

type PollData struct {
	OffersSince int64                 `json:"offers_since"`
	Sent        map[uint64]OfferState `json:"sent"`
	Received    map[uint64]OfferState `json:"received"`
}

type ExchangeDetails struct {
	Status         int             `json:"status"`
	TimeInit       int64           `json:"time_init"`
	AssetsReceived []ExchangeAsset `json:"assets_received"`
	AssetsGiven    []ExchangeAsset `json:"assets_given"`
}

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

// GetNewAssetID looks up the new asset ID assigned to a given original asset ID.
func (d *ExchangeDetails) GetNewAssetID(oldAssetID uint64) (uint64, bool) {
	if d == nil {
		return 0, false
	}

	for _, a := range d.AssetsReceived {
		if a.AssetID == oldAssetID && a.NewAssetID != 0 {
			return a.NewAssetID, true
		}
	}

	for _, a := range d.AssetsGiven {
		if a.AssetID == oldAssetID && a.NewAssetID != 0 {
			return a.NewAssetID, true
		}
	}

	return 0, false
}

// NewAssetIDs returns a mapping of original asset IDs to their new post-trade asset IDs.
func (d *ExchangeDetails) NewAssetIDs() map[uint64]uint64 {
	if d == nil {
		return nil
	}

	res := make(map[uint64]uint64, len(d.AssetsReceived)+len(d.AssetsGiven))
	for _, a := range d.AssetsReceived {
		if a.NewAssetID != 0 {
			res[a.AssetID] = a.NewAssetID
		}
	}

	for _, a := range d.AssetsGiven {
		if a.NewAssetID != 0 {
			res[a.AssetID] = a.NewAssetID
		}
	}

	return res
}

// ReceivedAssetIDs returns a slice containing the newly assigned asset IDs for all received items.
func (d *ExchangeDetails) ReceivedAssetIDs() []uint64 {
	if d == nil {
		return nil
	}

	res := make([]uint64, 0, len(d.AssetsReceived))
	for _, a := range d.AssetsReceived {
		if a.NewAssetID != 0 {
			res = append(res, a.NewAssetID)
		} else {
			res = append(res, a.AssetID)
		}
	}

	return res
}
