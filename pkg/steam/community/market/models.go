// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package market

import (
	"strconv"
	"time"

	json "github.com/goccy/go-json"
	"github.com/lemon4ksan/aoni/codec/values"
)

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

type Action struct {
	Link string `json:"link"`
	Name string `json:"name"`
}

type Description struct {
	Type  string `json:"type"`
	Value string `json:"value"`
	Color string `json:"color"`
	Label string `json:"label"`
}

type Asset struct {
	AppID           int                 `json:"appid"`
	ContextID       values.Int64String  `json:"contextid"`
	ID              values.Uint64String `json:"id"`
	ClassID         values.Uint64String `json:"classid"`
	InstanceID      values.Uint64String `json:"instanceid"`
	Amount          values.Int64String  `json:"amount"`
	BackgroundColor string              `json:"background_color"`
	IconURL         string              `json:"icon_url"`
	IconURLLarge    string              `json:"icon_url_large"`
	Descriptions    []Description       `json:"descriptions"`
	Tradable        values.BoolInt      `json:"tradable"`
	Actions         []Action            `json:"actions"`
	Name            string              `json:"name"`
	NameColor       string              `json:"name_color"`
	Type            string              `json:"type"`
	MarketName      string              `json:"market_name"`
	MarketHashName  string              `json:"market_hash_name"`
	Commodity       values.BoolInt      `json:"commodity"`
	Marketable      values.BoolInt      `json:"marketable"`
}

type CreateSellOrderOptions struct {
	AppID     uint32
	AssetID   uint64
	ContextID int64
	Price     int
	Amount    int
}

type CreateSellOrderResponse struct {
	Success                 bool   `json:"success"`
	RequiresConfirmation    int    `json:"requires_confirmation"`
	NeedsMobileConfirmation bool   `json:"needs_mobile_confirmation"`
	NeedsEmailConfirmation  bool   `json:"needs_email_confirmation"`
	EmailDomain             string `json:"email_domain"`
}

type CreateSellOrder struct {
	Success                 bool
	RequiresConfirmation    bool
	NeedsMobileConfirmation bool
	NeedsEmailConfirmation  bool
	EmailDomain             string
}

type CreateBuyOrderOptions struct {
	AppID          uint32
	MarketHashName string
	Price          int
	Amount         int
}

type CreateBuyOrderResponse struct {
	Success    bool   `json:"success"`
	BuyOrderID uint64 `json:"buy_orderid,string"`
}

type ItemOrdersHistogramResponse struct {
	Success          int                  `json:"success"`
	SellOrderTable   string               `json:"sell_order_table"`
	SellOrderSummary string               `json:"sell_order_summary"`
	BuyOrderTable    string               `json:"buy_order_table"`
	BuyOrderSummary  string               `json:"buy_order_summary"`
	HighestBuyOrder  values.Float64String `json:"highest_buy_order"`
	LowestSellOrder  values.Float64String `json:"lowest_sell_order"`
	BuyOrderGraph    GraphPoints          `json:"buy_order_graph"`
	SellOrderGraph   GraphPoints          `json:"sell_order_graph"`
	GraphMaxY        float64              `json:"graph_max_y"`
	GraphMinX        float64              `json:"graph_min_x"`
	GraphMaxX        float64              `json:"graph_max_x"`
	PricePrefix      string               `json:"price_prefix"`
	PriceSuffix      string               `json:"price_suffix"`
}

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

type ItemOrdersHistogram struct {
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

type PriceHistoryResponse struct {
	Success     bool          `json:"success"`
	PricePrefix string        `json:"price_prefix"`
	PriceSuffix string        `json:"price_suffix"`
	Prices      []PriceSample `json:"prices"`
}

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

type PriceOverviewResponse struct {
	Success     bool   `json:"success"`
	LowestPrice string `json:"lowest_price"`
	Volume      string `json:"volume"`
	MedianPrice string `json:"median_price"`
}

type MyListingsResponse struct {
	Success           bool                                   `json:"success"`
	PageSize          int                                    `json:"pagesize"`
	TotalCount        int                                    `json:"total_count"`
	Assets            map[string]map[string]map[string]Asset `json:"assets"`
	Start             int                                    `json:"start"`
	NumActiveListings int                                    `json:"num_active_listings"`
	Listings          []ListingResponse                      `json:"listings"`
	ListingsOnHold    []ListingResponse                      `json:"listings_on_hold"`
	ListingsToConfirm []ListingResponse                      `json:"listings_to_confirm"`
	BuyOrders         []BuyOrderResponse                     `json:"buy_orders"`
}

type ListingResponse struct {
	ListingID           string `json:"listingid"`
	TimeCreated         int64  `json:"time_created"`
	Asset               Asset  `json:"asset"`
	SteamIDLister       string `json:"steamid_lister"`
	Price               int    `json:"price"`
	OriginalPrice       int    `json:"original_price"`
	Fee                 int    `json:"fee"`
	CurrencyID          string `json:"currencyid"`
	PublisherFeePercent string `json:"publisher_fee_percent"`
	PublisherFeeApp     int    `json:"publisher_fee_app"`
}

type BuyOrderResponse struct {
	AppID             int    `json:"appid"`
	HashName          string `json:"hash_name"`
	WalletCurrency    int    `json:"wallet_currency"`
	Price             string `json:"price"`
	Quantity          string `json:"quantity"`
	QuantityRemaining string `json:"quantity_remaining"`
	BuyOrderID        string `json:"buy_orderid"`
	Description       Asset  `json:"description"`
}

type SearchOptions struct {
	Query              string `url:"query"`
	Start              int    `url:"start"`
	Count              int    `url:"count"               default:"100"`
	SearchDescriptions bool   `url:"search_descriptions"`
	SortColumn         string `url:"sort_column"         default:"popular"`
	SortDir            string `url:"sort_dir"            default:"desc"`
}

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

type SearchResultResponse struct {
	Name             string `json:"name"`
	HashName         string `json:"hash_name"`
	SellListings     int    `json:"sell_listings"`
	SellPrice        int    `json:"sell_price"`
	SellPriceText    string `json:"sell_price_text"`
	AppIcon          string `json:"app_icon"`
	AppName          string `json:"app_name"`
	AssetDescription Asset  `json:"asset_description"`
	SalePriceText    string `json:"sale_price_text"`
}

type GemValue struct {
	PromptTitle string
	GemValue    int64
}

type GemsResult struct {
	GemsReceived int64
	TotalGems    int64
}

type BoosterCatalog struct {
	TotalGems      int
	TradableGems   int
	UntradableGems int
	Catalog        map[uint32]*BoosterPackInfo
}

type BoosterPackInfo struct {
	AppID           uint32 `json:"appid"`
	Name            string `json:"name"`
	Price           int    `json:"price"`
	Unavailable     bool   `json:"unavailable"`
	AvailableAtTime string `json:"available_at_time,omitempty"`
}

type BoosterResult struct {
	TotalGems      int64
	TradableGems   int64
	UntradableGems int64
	ResultItem     any
}

type GiftDetails struct {
	GiftName  string
	PackageID int64
	Owned     bool
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
	GooValueReceived values.Int64String `json:"goo_value_received "`
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
