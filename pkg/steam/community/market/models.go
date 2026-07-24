// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package market

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/lemon4ksan/aoni/codec/values"
)

// CurrencyCode is a steam currency code.
type CurrencyCode int

// Steam Currency codes
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

// Action is an action link for the item (for example, "Inspect in game").
type Action struct {
	// Link is the destination URL of the action.
	Link string `json:"link"`
	// Name is the display name of the action button.
	Name string `json:"name"`
}

// Description describes one line in the item description.
type Description struct {
	// Type is the format type of the description.
	Type string `json:"type"`
	// Value is the text value of the description.
	Value string `json:"value"`
	// Color is the localized hex color of the text.
	Color string `json:"color"`
	// Label is the custom label associated with this description.
	Label string `json:"label"`
}

// Asset is now the single source of truth.
// No more "AssetResponse" vs "Asset".
type Asset struct {
	// AppID is the Steam AppID of the application.
	AppID int `json:"appid"`
	// ContextID is the unique context identifier.
	ContextID values.Int64String `json:"contextid"`
	// ID is the unique asset identifier.
	ID values.Uint64String `json:"id"`
	// ClassID is the class identifier of the asset.
	ClassID values.Uint64String `json:"classid"`
	// InstanceID is the instance identifier of the asset.
	InstanceID values.Uint64String `json:"instanceid"`
	// Amount is the quantity of the asset.
	Amount values.Int64String `json:"amount"`
	// BackgroundColor is the background color of the asset icon.
	BackgroundColor string `json:"background_color"`
	// IconURL is the direct path suffix to the asset icon image.
	IconURL string `json:"icon_url"`
	// IconURLLarge is the direct path suffix to the large icon image.
	IconURLLarge string `json:"icon_url_large"`
	// Descriptions contains detailed item descriptions.
	Descriptions []Description `json:"descriptions"`
	// Tradable is true if the asset can be traded.
	Tradable values.BoolInt `json:"tradable"`
	// Actions contains interactive action links.
	Actions []Action `json:"actions"`
	// Name is the display name of the asset.
	Name string `json:"name"`
	// NameColor is the color of the asset display name.
	NameColor string `json:"name_color"`
	// Type is the localized classification type of the asset.
	Type string `json:"type"`
	// MarketName is the name of the asset on the Steam Market.
	MarketName string `json:"market_name"`
	// MarketHashName is the standard market identifier name.
	MarketHashName string `json:"market_hash_name"`
	// Commodity is true if the asset is a commodity (standard stackable item).
	Commodity values.BoolInt `json:"commodity"`
	// Marketable is true if the asset can be sold on the Steam Market.
	Marketable values.BoolInt `json:"marketable"`
}

// CreateSellOrderOptions contains parameters for creating a sell order.
type CreateSellOrderOptions struct {
	// AppID is the Steam AppID of the application.
	AppID uint32
	// AssetID is the unique asset identifier.
	AssetID uint64
	// ContextID is the unique context identifier.
	ContextID int64
	// Price is the target price in minimum currency units (for example, cents).
	Price int
	// Amount is the quantity to sell.
	Amount int
}

// CreateSellOrderResponse is the raw response from the API when creating a sell order.
type CreateSellOrderResponse struct {
	// Success is true if the request was successful.
	Success bool `json:"success"`
	// RequiresConfirmation is 1 if the order requires Steam Guard confirmation.
	RequiresConfirmation int `json:"requires_confirmation"`
	// NeedsMobileConfirmation is true if mobile authenticator approval is required.
	NeedsMobileConfirmation bool `json:"needs_mobile_confirmation"`
	// NeedsEmailConfirmation is true if email confirmation is required.
	NeedsEmailConfirmation bool `json:"needs_email_confirmation"`
	// EmailDomain is the target domain name where the email confirmation was sent.
	EmailDomain string `json:"email_domain"`
}

// CreateSellOrder is a pure response structure when creating a sell order.
type CreateSellOrder struct {
	// Success is true if the request was successful.
	Success bool
	// RequiresConfirmation is true if the order requires Steam Guard confirmation.
	RequiresConfirmation bool
	// NeedsMobileConfirmation is true if mobile authenticator approval is required.
	NeedsMobileConfirmation bool
	// NeedsEmailConfirmation is true if email confirmation is required.
	NeedsEmailConfirmation bool
	// EmailDomain is the target domain name where the email confirmation was sent.
	EmailDomain string
}

// CreateBuyOrderOptions contains parameters for creating a buy order.
type CreateBuyOrderOptions struct {
	// AppID is the Steam AppID of the application.
	AppID uint32
	// MarketHashName is the standard market identifier name.
	MarketHashName string
	// Price is the purchase price limit in minimum currency units (for example, cents).
	Price int
	// Amount is the quantity to buy.
	Amount int
}

// CreateBuyOrderResponse response from the API when creating a buy order.
type CreateBuyOrderResponse struct {
	// Success is true if the request was successful.
	Success bool `json:"success"`
	// BuyOrderID is the unique string identifier of the created buy order.
	BuyOrderID uint64 `json:"buy_orderid,string"`
}

// ItemOrdersHistogramResponse is the raw API response with the orders histogram.
type ItemOrdersHistogramResponse struct {
	// Success is 1 if the request was successful.
	Success int `json:"success"`
	// SellOrderTable is the HTML table block representing sell orders.
	SellOrderTable string `json:"sell_order_table"`
	// SellOrderSummary is the HTML summary block representing sell orders.
	SellOrderSummary string `json:"sell_order_summary"`
	// BuyOrderTable is the HTML table block representing buy orders.
	BuyOrderTable string `json:"buy_order_table"`
	// BuyOrderSummary is the HTML summary block representing buy orders.
	BuyOrderSummary string `json:"buy_order_summary"`
	// HighestBuyOrder is the highest active buy order price as a decimal string.
	HighestBuyOrder values.Float64String `json:"highest_buy_order"`
	// LowestSellOrder is the lowest active sell order price as a decimal string.
	LowestSellOrder values.Float64String `json:"lowest_sell_order"`
	// BuyOrderGraph contains the coordinates for the buy order volume chart.
	BuyOrderGraph GraphPoints `json:"buy_order_graph"`
	// SellOrderGraph contains the coordinates for the sell order volume chart.
	SellOrderGraph GraphPoints `json:"sell_order_graph"`
	// GraphMaxY is the maximum Y-axis limit for the chart.
	GraphMaxY float64 `json:"graph_max_y"`
	// GraphMinX is the minimum X-axis limit for the chart.
	GraphMinX float64 `json:"graph_min_x"`
	// GraphMaxX is the maximum X-axis limit for the chart.
	GraphMaxX float64 `json:"graph_max_x"`
	// PricePrefix is the currency symbol prefix.
	PricePrefix string `json:"price_prefix"`
	// PriceSuffix is the currency symbol suffix.
	PriceSuffix string `json:"price_suffix"`
}

// GraphPoint represents a single point on the order chart (price, volume, description).
type GraphPoint struct {
	// Price is the cost coordinate value.
	Price float64
	// Volume is the cumulative quantity coordinate value.
	Volume int64
	// Description is the text label explaining the coordinate.
	Description string
}

// GraphPoints is a slice of GraphPoint.
type GraphPoints []GraphPoint

// UnmarshalJSON implements json.Unmarshaler.
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
	// SellOrderTable is the HTML table block representing sell orders.
	SellOrderTable string
	// SellOrderSummary is the HTML summary block representing sell orders.
	SellOrderSummary string
	// BuyOrderTable is the HTML table block representing buy orders.
	BuyOrderTable string
	// BuyOrderSummary is the HTML summary block representing buy orders.
	BuyOrderSummary string
	// HighestBuyOrder is the highest active buy order price limit.
	HighestBuyOrder float64
	// LowestSellOrder is the lowest active sell order price limit.
	LowestSellOrder float64
	// BuyOrderGraph contains the coordinates for the buy order volume chart.
	BuyOrderGraph GraphPoints
	// SellOrderGraph contains the coordinates for the sell order volume chart.
	SellOrderGraph GraphPoints
	// GraphMaxY is the maximum Y-axis limit for the chart.
	GraphMaxY float64
	// GraphMinX is the minimum X-axis limit for the chart.
	GraphMinX float64
	// GraphMaxX is the maximum X-axis limit for the chart.
	GraphMaxX float64
	// PricePrefix is the currency symbol prefix.
	PricePrefix string
	// PriceSuffix is the currency symbol suffix.
	PriceSuffix string
}

// PriceHistoryResponse is the raw API response with the price history.
type PriceHistoryResponse struct {
	// Success is true if the request was successful.
	Success bool `json:"success"`
	// PricePrefix is the currency symbol prefix.
	PricePrefix string `json:"price_prefix"`
	// PriceSuffix is the currency symbol suffix.
	PriceSuffix string `json:"price_suffix"`
	// Prices contains chronological price samples.
	Prices []PriceSample `json:"prices"`
}

// PriceSample represents a single point in price history (time, price, volume).
type PriceSample struct {
	// Timestamp is the exact time of the transaction sample.
	Timestamp time.Time
	// Price is the cost coordinate value.
	Price float64
	// Volume is the quantity of items traded at this price coordinate.
	Volume int64
}

// UnmarshalJSON implements json.Unmarshaler.
func (ps *PriceSample) UnmarshalJSON(data []byte) error {
	var rawPriceSample [3]json.RawMessage
	if err := json.Unmarshal(data, &rawPriceSample); err != nil {
		return err
	}

	var (
		timeStr   string
		volumeStr string
	)

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
	// Success is true if the request was successful.
	Success bool `json:"success"`
	// LowestPrice is the current lowest active price of the item.
	LowestPrice string `json:"lowest_price"`
	// Volume is the total quantity of items sold in the last 24 hours.
	Volume string `json:"volume"`
	// MedianPrice is the median transaction cost of the item.
	MedianPrice string `json:"median_price"`
}

// MyListingsResponse is the raw API response with the user's listings.
type MyListingsResponse struct {
	// Success is true if the request was successful.
	Success bool `json:"success"`
	// PageSize is the number of lots returned per page.
	PageSize int `json:"pagesize"`
	// TotalCount is the total number of active lots on the account.
	TotalCount int `json:"total_count"`
	// Assets is the nested catalog map containing detailed item descriptions.
	Assets map[string]map[string]map[string]Asset `json:"assets"`
	// Start is the starting offset of pagination.
	Start int `json:"start"`
	// NumActiveListings is the count of active selling lots.
	NumActiveListings int `json:"num_active_listings"`
	// Listings is the list of active selling lots.
	Listings []ListingResponse `json:"listings"`
	// ListingsOnHold is the list of selling lots currently on trade hold.
	ListingsOnHold []ListingResponse `json:"listings_on_hold"`
	// ListingsToConfirm is the list of selling lots awaiting mobile approval.
	ListingsToConfirm []ListingResponse `json:"listings_to_confirm"`
	// BuyOrders is the list of active buy orders.
	BuyOrders []BuyOrderResponse `json:"buy_orders"`
}

// ListingResponse is the raw structure of the lot.
type ListingResponse struct {
	// ListingID is the unique string identifier of the listing.
	ListingID string `json:"listingid"`
	// TimeCreated is the Unix timestamp when the listing was created.
	TimeCreated int64 `json:"time_created"`
	// Asset is the underlying listing asset details.
	Asset Asset `json:"asset"`
	// SteamIDLister is the 64-bit Steam ID of the lister.
	SteamIDLister string `json:"steamid_lister"`
	// Price is the selling price in minimum currency units.
	Price int `json:"price"`
	// OriginalPrice is the original selling price in minimum currency units.
	OriginalPrice int `json:"original_price"`
	// Fee is the transaction fee associated with the listing.
	Fee int `json:"fee"`
	// CurrencyID is the currency identifier of the listing.
	CurrencyID string `json:"currencyid"`
	// PublisherFeePercent is the custom publisher transaction fee percentage.
	PublisherFeePercent string `json:"publisher_fee_percent"`
	// PublisherFeeApp is the Steam AppID of the application receiving the publisher fee.
	PublisherFeeApp int `json:"publisher_fee_app"`
}

// BuyOrderResponse is the raw structure of the buy order.
type BuyOrderResponse struct {
	// AppID is the Steam AppID of the application.
	AppID int `json:"appid"`
	// HashName is the standard market identifier name of the target.
	HashName string `json:"hash_name"`
	// WalletCurrency is the wallet currency identifier.
	WalletCurrency int `json:"wallet_currency"`
	// Price is the buy order price limit.
	Price string `json:"price"`
	// Quantity is the requested quantity of the buy order.
	Quantity string `json:"quantity"`
	// QuantityRemaining is the remaining unfulfilled quantity.
	QuantityRemaining string `json:"quantity_remaining"`
	// BuyOrderID is the unique string identifier of the buy order.
	BuyOrderID string `json:"buy_orderid"`
	// Description contains the detailed target asset descriptions.
	Description Asset `json:"description"`
}

// SearchOptions contains parameters for determining the TP.
type SearchOptions struct {
	// Query is the search keywords filter.
	Query string `url:"query"`
	// Start is the starting offset of pagination.
	Start int `url:"start"`
	// Count is the maximum number of items returned.
	Count int `url:"count" default:"100"`
	// SearchDescriptions is true if keywords should be matched against descriptions.
	SearchDescriptions bool `url:"search_descriptions"`
	// SortColumn specifies the column to sort by (such as "popular", "price", "quantity", or "name").
	SortColumn string `url:"sort_column" default:"popular"`
	// SortDir specifies the chronological sorting direction (such as "asc" or "desc").
	SortDir string `url:"sort_dir" default:"desc"`
}

// SearchResponse is the raw structure of the search.
type SearchResponse struct {
	// Success is true if the request was successful.
	Success bool `json:"success"`
	// Start is the starting offset of pagination.
	Start int `json:"start"`
	// Pagesize is the number of results returned per page.
	Pagesize int `json:"pagesize"`
	// TotalCount is the total number of matching items.
	TotalCount int `json:"total_count"`
	// SearchData holds metadata regarding the current search query context.
	SearchData struct {
		Query              string `json:"query"`
		SearchDescriptions bool   `json:"search_descriptions"`
		TotalCount         int    `json:"total_count"`
		PageSize           int    `json:"page_size"`
		Prefix             string `json:"prefix"`
		ClassPrefix        string `json:"class_prefix"`
	} `json:"search_data"`
	// Results is the list of matched items.
	Results []SearchResultResponse `json:"results"`
}

// SearchResultResponse is the raw search result structure.
type SearchResultResponse struct {
	// Name is the display name of the item.
	Name string `json:"name"`
	// HashName is the standard market identifier name.
	HashName string `json:"hash_name"`
	// SellListings is the count of active selling lots for this item.
	SellListings int `json:"sell_listings"`
	// SellPrice is the lowest active price in minimum currency units.
	SellPrice int `json:"sell_price"`
	// SellPriceText is the formatted lowest price text.
	SellPriceText string `json:"sell_price_text"`
	// AppIcon is the direct URL to the application icon.
	AppIcon string `json:"app_icon"`
	// AppName is the name of the application.
	AppName string `json:"app_name"`
	// AssetDescription is the detailed asset descriptions.
	AssetDescription Asset `json:"asset_description"`
	// SalePriceText is the formatted lowest price text including tax.
	SalePriceText string `json:"sale_price_text"`
}

// GemValue represents the gem value info of an item.
type GemValue struct {
	// PromptTitle is the title displayed in the gem grinding confirmation dialog.
	PromptTitle string
	// GemValue is the total quantity of gems received on grinding.
	GemValue int64
}

// GemsResult represents the result of grinding an item into gems.
type GemsResult struct {
	// GemsReceived is the quantity of gems received on grinding.
	GemsReceived int64
	// TotalGems is the total quantity of gems on the account.
	TotalGems int64
}

// BoosterCatalog represents the booster pack creator catalog details.
type BoosterCatalog struct {
	// TotalGems is the total quantity of gems on the account.
	TotalGems int
	// TradableGems is the quantity of tradable gems on the account.
	TradableGems int
	// UntradableGems is the quantity of untradable gems on the account.
	UntradableGems int
	// Catalog is the map containing available booster packs indexed by AppID.
	Catalog map[uint32]*BoosterPackInfo
}

// BoosterPackInfo represents a single card/game booster pack cost details.
type BoosterPackInfo struct {
	// AppID is the Steam AppID of the application.
	AppID uint32 `json:"appid"`
	// Name is the display name of the booster pack.
	Name string `json:"name"`
	// Price is the cost of the booster pack in gems.
	Price int `json:"price"`
	// Unavailable is true if the booster pack is currently unavailable to craft.
	Unavailable bool `json:"unavailable"`
	// AvailableAtTime specifies the timestamp when the booster pack becomes available.
	AvailableAtTime string `json:"available_at_time,omitempty"`
}

// BoosterResult represents the result of creating or opening a booster pack.
type BoosterResult struct {
	// TotalGems is the total quantity of gems remaining on the account.
	TotalGems int64
	// TradableGems is the quantity of tradable gems remaining on the account.
	TradableGems int64
	// UntradableGems is the quantity of untradable gems remaining on the account.
	UntradableGems int64
	// ResultItem is the raw item description details of the crafted/unpacked asset.
	ResultItem any
}

// GiftDetails represents the details of a Steam Inventory gift pack.
type GiftDetails struct {
	// GiftName is the display name of the gift.
	GiftName string
	// PackageID is the package identifier of the gift.
	PackageID int64
	// Owned is true if the user already owns the target package.
	Owned bool
}

type gemValueResponse struct {
	Success  int                `json:"success"`
	Message  string             `json:"message"`
	GooValue values.Int64String `json:"goo_value"`
	StrTitle string             `json:"strTitle"`
}

type grindGooResponse struct {
	Success          int                `json:"success"`
	Message          string             `json:"message"`
	GooValueReceived values.Int64String `json:"goo_value_received "` // lol valve
	GooValueTotal    values.Int64String `json:"goo_value_total"`
}

type unpackBoosterResponse struct {
	Success int    `json:"success"`
	Message string `json:"message"`
	RgItems []any  `json:"rgItems"`
}

type createBoosterResponse struct {
	PurchaseEResult     int                `json:"purchase_eresult"`
	GooAmount           values.Int64String `json:"goo_amount"`
	TradableGooAmount   values.Int64String `json:"tradable_goo_amount"`
	UntradableGooAmount values.Int64String `json:"untradable_goo_amount"`
	PurchaseResult      any                `json:"purchase_result"`
}

type giftDetailsResponse struct {
	Success   int                `json:"success"`
	Message   string             `json:"message"`
	PackageID values.Int64String `json:"packageid"`
	GiftName  string             `json:"gift_name"`
	Owned     bool               `json:"owned"`
}

type redeemGiftResponse struct {
	Success int    `json:"success"`
	Message string `json:"message"`
}

type gemExchangeResponse struct {
	Success int    `json:"success"`
	Message string `json:"message"`
}
