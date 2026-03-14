// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package market

import (
	"encoding/json"
	"strconv"
	"time"
)

// CurrencyCode представляет коды валют Steam.
type CurrencyCode int

const (
	CurrencyCodeInvalid CurrencyCode = iota
	CurrencyCodeUSD
	CurrencyCodeGBP
	CurrencyCodeEUR
	CurrencyCodeCHF
	CurrencyCodeRUB
	CurrencyCodePLN
	CurrencyCodeBRL
	CurrencyCodeJPY
	CurrencyCodeNOK
	CurrencyCodeIDR
	CurrencyCodeMYR
	CurrencyCodePHP
	CurrencyCodeSGD
	CurrencyCodeTHB
	CurrencyCodeVND
	CurrencyCodeKRW
	CurrencyCodeTRY
	CurrencyCodeUAH
	CurrencyCodeMXN
	CurrencyCodeCAD
	CurrencyCodeAUD
	CurrencyCodeNZD
	CurrencyCodeCNY
	CurrencyCodeINR
	CurrencyCodeCLP
	CurrencyCodePEN
	CurrencyCodeCOP
	CurrencyCodeZAR
	CurrencyCodeHKD
	CurrencyCodeTWD
	CurrencyCodeSAR
	CurrencyCodeAED
	CurrencyMissing
	CurrencyCodeARS
	CurrencyCodeILS
	CurrencyCodeBYN
	CurrencyCodeKZT
	CurrencyCodeKWD
	CurrencyCodeQAR
	CurrencyCodeCRC
	CurrencyCodeUYU
)

// Action is an action link for the item (e.g. "Inspect in game").
type Action struct {
	Link string `json:"link"`
	Name string `json:"name"`
}

// Description describes one line in the item description.
type Description struct {
	Type  string `json:"type"`
	Value string `json:"value"`
	Color string `json:"color"`
	Label string `json:"label"`
}

// AssetResponse is the raw item structure as returned by the Steam API.
type AssetResponse struct {
	AppID           int           `json:"appid"`
	ContextID       string        `json:"contextid"`
	ID              string        `json:"id"`
	ClassID         string        `json:"classid"`
	InstanceID      string        `json:"instanceid"`
	Amount          string        `json:"amount"`
	BackgroundColor string        `json:"background_color"`
	IconURL         string        `json:"icon_url"`
	IconURLLarge    string        `json:"icon_url_large"`
	Descriptions    []Description `json:"descriptions"`
	Tradable        int           `json:"tradable"`
	Actions         []Action      `json:"actions"`
	Name            string        `json:"name"`
	NameColor       string        `json:"name_color"`
	Type            string        `json:"type"`
	MarketName      string        `json:"market_name"`
	MarketHashName  string        `json:"market_hash_name"`
	Commodity       int           `json:"commodity"`
	Marketable      int           `json:"marketable"`
}

// Asset is a clean representation of the [AssetResponse].
type Asset struct {
	AppID           int
	ContextID       int64
	ID              uint64
	ClassID         uint64
	InstanceID      uint64
	Amount          int64
	BackgroundColor string
	IconURL         string
	IconURLLarge    string
	Descriptions    []Description
	Tradable        bool
	Actions         []Action
	Name            string
	NameColor       string
	Type            string
	MarketName      string
	MarketHashName  string
	Commodity       bool
	Marketable      bool
}

// ToAsset converts AssetResponse to a pure Asset structure.
func (ar *AssetResponse) ToAsset() *Asset {
	contextID, _ := strconv.ParseInt(ar.ContextID, 10, 64)
	assetID, _ := strconv.ParseUint(ar.ID, 10, 64)
	classID, _ := strconv.ParseUint(ar.ClassID, 10, 64)
	instanceID, _ := strconv.ParseUint(ar.InstanceID, 10, 64)
	amount, _ := strconv.ParseInt(ar.Amount, 10, 64)

	return &Asset{
		AppID:           ar.AppID,
		ContextID:       contextID,
		ID:              assetID,
		ClassID:         classID,
		InstanceID:      instanceID,
		Amount:          amount,
		BackgroundColor: ar.BackgroundColor,
		IconURL:         ar.IconURL,
		IconURLLarge:    ar.IconURLLarge,
		Descriptions:    ar.Descriptions,
		Tradable:        ar.Tradable == 1,
		Actions:         ar.Actions,
		Name:            ar.Name,
		NameColor:       ar.NameColor,
		Type:            ar.Type,
		MarketName:      ar.MarketName,
		MarketHashName:  ar.MarketHashName,
		Commodity:       ar.Commodity == 1,
		Marketable:      ar.Marketable == 1,
	}
}

// CreateSellOrderOptions contains parameters for creating a sell order.
type CreateSellOrderOptions struct {
	AssetID   uint64
	ContextID int64
	Price     int // Price in minimum currency units (kopecks, cents)
	Amount    int
}

// CreateSellOrderResponse is the raw response from the API when creating a sell order.
type CreateSellOrderResponse struct {
	Success                 bool   `json:"success"`
	RequiresConfirmation    int    `json:"requires_confirmation"`
	NeedsMobileConfirmation bool   `json:"needs_mobile_confirmation"`
	NeedsEmailConfirmation  bool   `json:"needs_email_confirmation"`
	EmailDomain             string `json:"email_domain"`
}

// CreateSellOrder is a pure response structure when creating a sell order.
type CreateSellOrder struct {
	Success                 bool
	RequiresConfirmation    bool
	NeedsMobileConfirmation bool
	NeedsEmailConfirmation  bool
	EmailDomain             string
}

// CreateBuyOrderOptions contains parameters for creating a buy order.
type CreateBuyOrderOptions struct {
	MarketHashName string
	Price          int // Price in minimum currency units
	Amount         int
}

// CreateBuyOrderResponse response from the API when creating a buy order.
type CreateBuyOrderResponse struct {
	Success    bool   `json:"success"`
	BuyOrderID uint64 `json:"buy_orderid,string"`
}

// ItemOrdersHistogramResponse is the raw API response with the orders histogram.
type ItemOrdersHistogramResponse struct {
	Success          int         `json:"success"`
	SellOrderTable   string      `json:"sell_order_table"`
	SellOrderSummary string      `json:"sell_order_summary"`
	BuyOrderTable    string      `json:"buy_order_table"`
	BuyOrderSummary  string      `json:"buy_order_summary"`
	HighestBuyOrder  string      `json:"highest_buy_order"`
	LowestSellOrder  string      `json:"lowest_sell_order"`
	BuyOrderGraph    GraphPoints `json:"buy_order_graph"`
	SellOrderGraph   GraphPoints `json:"sell_order_graph"`
	GraphMaxY        float64     `json:"graph_max_y"`
	GraphMinX        float64     `json:"graph_min_x"`
	GraphMaxX        float64     `json:"graph_max_x"`
	PricePrefix      string      `json:"price_prefix"`
	PriceSuffix      string      `json:"price_suffix"`
}

// GraphPoint represents a single point on the order chart (price, volume, description).
type GraphPoint struct {
	Price       float64
	Volume      int64
	Description string
}

type GraphPoints []GraphPoint

func (g *GraphPoints) UnmarshalJSON(data []byte) error {
	var rawGraph [][]json.RawMessage
	if err := json.Unmarshal(data, &rawGraph); err != nil {
		return err
	}

	points := make([]GraphPoint, len(rawGraph))
	for i, rawPoint := range rawGraph {
		if len(rawPoint) != 3 {
			continue
		}

		var p GraphPoint
		_ = json.Unmarshal(rawPoint[0], &p.Price)
		_ = json.Unmarshal(rawPoint[1], &p.Volume)
		_ = json.Unmarshal(rawPoint[2], &p.Description)
		points[i] = p
	}

	*g = points
	return nil
}

// ItemOrdersHistogram is a pure structure with a histogram of orders.
type ItemOrdersHistogram struct {
	Success          bool
	SellOrderTable   string
	SellOrderSummary string
	BuyOrderTable    string
	BuyOrderSummary  string
	HighestBuyOrder  float64
	LowestSellOrder  float64
	BuyOrderGraph    GraphPoints
	SellOrderGraph   GraphPoints
	GraphMaxY        float64
	GraphMinX        float64
	GraphMaxX        float64
	PricePrefix      string
	PriceSuffix      string
}

// PriceHistoryResponse is the raw API response with the price history.
type PriceHistoryResponse struct {
	Success     bool          `json:"success"`
	PricePrefix string        `json:"price_prefix"`
	PriceSuffix string        `json:"price_suffix"`
	Prices      []PriceSample `json:"prices"`
}

// PriceSample represents a single point in price history (time, price, volume).
type PriceSample struct {
	Timestamp time.Time
	Price     float64
	Volume    int64
}

func (ps *PriceSample) UnmarshalJSON(data []byte) error {
	var rawPriceSample [3]json.RawMessage
	if err := json.Unmarshal(data, &rawPriceSample); err != nil {
		return err
	}

	var timeStr string
	var volumeStr string
	if err := json.Unmarshal(rawPriceSample[0], &timeStr); err != nil {
		return err
	}
	if err := json.Unmarshal(rawPriceSample[1], &ps.Price); err != nil {
		return err
	}
	if err := json.Unmarshal(rawPriceSample[2], &volumeStr); err != nil {
		return err
	}

	t, err := time.Parse("Jan 02 2006 15:04:05 GMT-0700", timeStr[:len(timeStr)-6])
	if err != nil {
		return err
	}
	ps.Timestamp = t
	ps.Volume, _ = strconv.ParseInt(volumeStr, 10, 64)

	return nil
}

// PriceOverviewResponse is the raw API response with the price overview.
type PriceOverviewResponse struct {
	Success     bool   `json:"success"`
	LowestPrice string `json:"lowest_price"`
	Volume      string `json:"volume"`
	MedianPrice string `json:"median_price"`
}

// MyListingsResponse is the raw API response with the user's listings.
type MyListingsResponse struct {
	Success           bool                                           `json:"success"`
	PageSize          int                                            `json:"pagesize"`
	TotalCount        int                                            `json:"total_count"`
	Assets            map[string]map[string]map[string]AssetResponse `json:"assets"`
	Start             int                                            `json:"start"`
	NumActiveListings int                                            `json:"num_active_listings"`
	Listings          []ListingResponse                              `json:"listings"`
	ListingsOnHold    []ListingResponse                              `json:"listings_on_hold"`
	ListingsToConfirm []ListingResponse                              `json:"listings_to_confirm"`
	BuyOrders         []BuyOrderResponse                             `json:"buy_orders"`
}

// ListingResponse is the raw structure of the lot.
type ListingResponse struct {
	ListingID           string        `json:"listingid"`
	TimeCreated         int64         `json:"time_created"`
	Asset               AssetResponse `json:"asset"`
	SteamIDLister       string        `json:"steamid_lister"`
	Price               int           `json:"price"`
	OriginalPrice       int           `json:"original_price"`
	Fee                 int           `json:"fee"`
	CurrencyID          string        `json:"currencyid"`
	PublisherFeePercent string        `json:"publisher_fee_percent"`
	PublisherFeeApp     int           `json:"publisher_fee_app"`
}

// BuyOrderResponse is the raw structure of the buy order.
type BuyOrderResponse struct {
	AppID             int           `json:"appid"`
	HashName          string        `json:"hash_name"`
	WalletCurrency    int           `json:"wallet_currency"`
	Price             string        `json:"price"`
	Quantity          string        `json:"quantity"`
	QuantityRemaining string        `json:"quantity_remaining"`
	BuyOrderID        string        `json:"buy_orderid"`
	Description       AssetResponse `json:"description"`
}

// SearchResponse is the raw structure of the search.
type SearchResponse struct {
	Success    bool `json:"success"`
	Start      int  `json:"start"`
	Pagesize   int  `json:"pagesize"`
	TotalCount int  `json:"total_count"`
	SearchData struct {
		Query              string `json:"query"`
		SearchDescriptions bool   `json:"search_descriptions"`
		TotalCount         int    `json:"total_count"`
		PageSize           int    `json:"page_size"`
		Prefix             string `json:"prefix"`
		ClassPrefix        string `json:"class_prefix"`
	} `json:"search_data"`
	Results []SearchResultResponse `json:"results"`
}

// SearchResultResponse is the raw search result structure.
type SearchResultResponse struct {
	Name             string        `json:"name"`
	HashName         string        `json:"hash_name"`
	SellListings     int           `json:"sell_listings"`
	SellPrice        int           `json:"sell_price"`
	SellPriceText    string        `json:"sell_price_text"`
	AppIcon          string        `json:"app_icon"`
	AppName          string        `json:"app_name"`
	AssetDescription AssetResponse `json:"asset_description"`
	SalePriceText    string        `json:"sale_price_text"`
}
