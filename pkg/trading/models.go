// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trading

import (
	"bytes"

	json "github.com/goccy/go-json"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
)

// OfferState represents the transactional lifecycle state of a trade offer.
type OfferState int32

const (
	// OfferStateInvalid represents a malformed or invalid offer state.
	OfferStateInvalid OfferState = 1
	// OfferStateActive represents an active offer awaiting response.
	OfferStateActive OfferState = 2
	// OfferStateAccepted represents an offer that has been accepted and finalized.
	OfferStateAccepted OfferState = 3
	// OfferStateCountered represents an offer that was countered with modified parameters.
	OfferStateCountered OfferState = 4
	// OfferStateExpired represents an offer that passed its expiration threshold.
	OfferStateExpired OfferState = 5
	// OfferStateCanceled represents an offer initiated by us that we withdrew.
	OfferStateCanceled OfferState = 6
	// OfferStateDeclined represents an offer that we declined.
	OfferStateDeclined OfferState = 7
	// OfferStateInvalidItems represents an offer that failed because items are no longer available.
	OfferStateInvalidItems OfferState = 8
	// OfferStateCreatedNeedsConfirmation represents an offer that is pending mobile app confirmation.
	OfferStateCreatedNeedsConfirmation OfferState = 9
	// OfferStateCanceledBySecondFactor represents an offer canceled due to a failed mobile confirmation.
	OfferStateCanceledBySecondFactor OfferState = 10
	// OfferStateInEscrow represents an offer accepted but currently held in Steam escrow.
	OfferStateInEscrow OfferState = 11
)

// OfferParams represents the input parameters required to construct and dispatch a new trade offer.
type OfferParams struct {
	// PartnerID is the 64-bit Steam ID of the trade partner.
	PartnerID id.ID
	// Token is the optional trade token required if we are not friends with the partner.
	Token string
	// Message is the optional text message attached to the outgoing offer.
	Message string
	// ItemsToGive is the list of items from our inventory we offer to trade.
	ItemsToGive []*Item
	// ItemsToReceive is the list of items from the partner's inventory we request.
	ItemsToReceive []*Item
	// CounteredID is the ID of the original incoming offer being responded to, if any.
	CounteredID uint64
}

// Attribute represents a game-specific item schema attribute (such as paint or effect).
type Attribute struct {
	// Defindex is the unique attribute definition index from the game schema.
	Defindex int `json:"defindex"`
	// Value is the string representation of the attribute value.
	Value string `json:"value"`
	// FloatValue is the floating-point representation of the attribute value.
	FloatValue float64 `json:"float_value"`
}

// Description represents a single descriptive text line within an item's tooltip.
type Description struct {
	// Value is the raw text string of the description line.
	Value string `json:"value"`
	// Color is the optional hex color string (e.g. "7a7a7a") for rendering the text.
	Color string `json:"color,omitempty"`
	// AppData holds game-specific metadata attached to this descriptive line.
	AppData *struct {
		// Defindex is the attribute definition index.
		Defindex int `json:"def_index,string"`
	} `json:"app_data,omitempty"`
}

// UnmarshalJSON implements custom JSON unmarshaling to handle inconsistent types for app_data (string vs object).
func (d *Description) UnmarshalJSON(data []byte) error {
	type plainDescription struct {
		Value string `json:"value"`
		Color string `json:"color,omitempty"`
	}

	var pd plainDescription
	if err := json.Unmarshal(data, &pd); err != nil {
		return err
	}

	d.Value = pd.Value
	d.Color = pd.Color

	// Fast check: avoid wrapper struct allocations if app_data is not present in payload
	if bytes.Contains(data, []byte(`"app_data"`)) {
		var appDataWrapper struct {
			AppData json.RawMessage `json:"app_data"`
		}
		if err := json.Unmarshal(
			data,
			&appDataWrapper,
		); err == nil && len(appDataWrapper.AppData) > 0 &&
			appDataWrapper.AppData[0] == '{' {
			var appData struct {
				Defindex int `json:"def_index,string"`
			}
			if err := json.Unmarshal(appDataWrapper.AppData, &appData); err == nil {
				d.AppData = &struct {
					Defindex int `json:"def_index,string"`
				}{Defindex: appData.Defindex}
			}
		}
	}

	return nil
}

// Tag represents classification metadata attached to an item (such as rarity or type).
type Tag struct {
	// Category is the category identifier of the tag.
	Category string `json:"category"`
	// InternalName is the unique internal programmatic name of the tag.
	InternalName string `json:"internal_name"`
	// Localized is the localized human-readable category name.
	Localized string `json:"localized_category_name"`
	// LocalizedName is the localized human-readable tag value name.
	LocalizedName string `json:"localized_tag_name"`
}

// Action represents an interactive link attached to an item (such as an in-game inspect link).
type Action struct {
	// Link is the destination URL of the action.
	Link string `json:"link"`
	// Name is the human-readable label for the action button.
	Name string `json:"name"`
}

// Item represents a Steam inventory item with its associated metadata and schema descriptors.
type Item struct {
	// AppID is the Steam AppID of the game this item belongs to.
	AppID uint32 `json:"appid"`
	// ContextID is the Steam inventory context identifier (e.g., 2).
	ContextID int64 `json:"contextid,string"`
	// AssetID is the unique instance identifier of this item in the owner's inventory.
	AssetID uint64 `json:"assetid,string"`
	// ClassID is the immutable class identifier of the item type.
	ClassID uint64 `json:"classid,string"`
	// InstanceID is the optional instance variation identifier.
	InstanceID uint64 `json:"instanceid,string"`
	// Amount is the quantity of the item (always 1 for non-stackable items).
	Amount int64 `json:"amount,string"`
	// Missing is true if Steam reports the item was removed or is unavailable.
	Missing bool `json:"missing"`
	// Descriptions contains descriptive text lines for the item tooltip.
	Descriptions []Description `json:"descriptions"`
	// Tags holds classification descriptors of the item.
	Tags []Tag `json:"tags"`
	// Actions contains interactive links associated with the item.
	Actions []Action `json:"actions"`
	// Name is the basic display name of the item.
	Name string `json:"name"`
	// NameColor is the optional hex color string for rendering the item's name.
	NameColor string `json:"name_color"`
	// Type is the localized item classification text.
	Type string `json:"type"`
	// MarketName is the localized name of the item on the Steam Community Market.
	MarketName string `json:"market_name"`
	// MarketHashName is the unique English identifier of the item on the Market.
	MarketHashName string `json:"market_hash_name"`
	// IconURL is the path to the item's display image hosted on Steam CDN.
	IconURL string `json:"icon_url"`
	// Tradable is true if the item can be exchanged via Steam Trade.
	Tradable bool `json:"tradable"`
	// Marketable is true if the item can be listed on the Steam Community Market.
	Marketable bool `json:"marketable"`
	// SKU is a custom game-specific stock-keeping unit string used by trading bots.
	SKU string `json:"sku,omitempty"`
	// Attributes contains custom game-specific item attributes.
	Attributes []Attribute `json:"attributes,omitempty"`
}

// PollData represents the polling state used by the trade manager to detect new offers.
type PollData struct {
	// OffersSince is the Unix timestamp threshold of the last processed offer.
	OffersSince int64 `json:"offers_since"`
	// Sent is a map tracking the states of offers sent by our account.
	Sent map[uint64]OfferState `json:"sent"`
	// Received is a map tracking the states of offers received by our account.
	Received map[uint64]OfferState `json:"received"`
}

// ExchangeDetails contains receipts and asset conversion details for a finalized trade.
type ExchangeDetails struct {
	// Status is the transactional completion status.
	Status int `json:"status"`
	// TimeInit is the Unix timestamp indicating when the trade was initialized.
	TimeInit int64 `json:"time_init"`
	// AssetsReceived is the list of assets acquired in the exchange, including new asset IDs.
	AssetsReceived []ExchangeAsset `json:"assets_received"`
	// AssetsGiven is the list of assets transferred away in the exchange, including new asset IDs.
	AssetsGiven []ExchangeAsset `json:"assets_given"`
}

// ExchangeAsset represents an item involved in a finalized trade, containing old and new identifiers.
type ExchangeAsset struct {
	// AppID is the Steam AppID of the game this asset belongs to.
	AppID uint32 `json:"appid"`
	// ContextID is the Steam inventory context identifier.
	ContextID int64 `json:"contextid,string"`
	// AssetID is the original unique identifier before the exchange.
	AssetID uint64 `json:"assetid,string"`
	// Amount is the quantity of the asset.
	Amount int64 `json:"amount,string"`
	// ClassID is the class identifier of the asset.
	ClassID uint64 `json:"classid,string"`
	// InstanceID is the instance variation identifier.
	InstanceID uint64 `json:"instanceid,string"`
	// NewAssetID is the new unique identifier assigned to the asset in the recipient's inventory.
	NewAssetID uint64 `json:"new_assetid,string"`
	// NewContextID is the new inventory context identifier.
	NewContextID int64 `json:"new_contextid,string"`
}
