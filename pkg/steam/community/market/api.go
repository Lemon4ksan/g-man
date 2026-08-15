// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package market

import (
	"context"
	"io"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
)

var _ = community.BaseURL

// SteamMarketAPI is an interface for the Steam Community Market endpoints.
//
// @aoni:service
// @engine custom type="community.Requester" required
// @base_url "https://steamcommunity.com"
type SteamMarketAPI interface {
	// @post "market/sellitem"
	// @form
	// @preset :xhr
	// @inject field="sessionid" from="SessionID"
	// @referer "profiles/{steamID}/inventory?modal=1&market=1"
	SellItem(
		ctx context.Context,
		// @field "appid"
		appID uint32,
		// @field "contextid"
		contextID int64,
		// @field "assetid"
		assetID uint64,
		// @field "amount"
		amount int,
		// @field "price"
		price int,
		steamID id.ID,
		mods ...aoni.RequestModifier,
	) (*CreateSellOrderResponse, error)

	// @post "market/createbuyorder"
	// @form
	// @preset :xhr
	// @inject field="sessionid" from="SessionID"
	// @referer "market/listings/{appID}/{marketHashName:escape}"
	CreateBuyOrder(
		ctx context.Context,
		// @field "appid"
		appID uint32,
		// @field "currency"
		currency CurrencyCode,
		// @field "market_hash_name"
		marketHashName string,
		// @field "price_total"
		priceTotal string,
		// @field "quantity"
		quantity int,
		// @field "billing_state"
		billingState string,
		// @field "save_my_address"
		saveMyAddress string,
		mods ...aoni.RequestModifier,
	) (*CreateBuyOrderResponse, error)

	// @post "market/cancelbuyorder"
	// @form
	// @preset :xhr
	// @inject field="sessionid" from="SessionID"
	// @referer :origin
	CancelBuyOrder(
		ctx context.Context,
		// @field "buy_orderid"
		buyOrderID uint64,
		mods ...aoni.RequestModifier,
	) (*basicMarketResponse, error)

	// @post "market/removelisting/{listingID}"
	// @form
	// @preset :xhr
	// @inject field="sessionid" from="SessionID"
	// @referer :origin
	RemoveListing(
		ctx context.Context,
		listingID uint64,
		mods ...aoni.RequestModifier,
	) (*basicMarketResponse, error)

	// @get "market/search/render"
	// @preset :xhr
	// @referer "market/search?appid={appID}"
	Search(
		ctx context.Context,
		// @query "appid"
		appID uint32,
		opts SearchOptions,
		mods ...aoni.RequestModifier,
	) (*SearchResponse, error)

	// @get "market/priceoverview"
	// @preset :xhr
	// @referer "market/listings/{appID}/{marketHashName:escape}"
	GetPriceOverview(
		ctx context.Context,
		// @query "appid"
		appID uint32,
		// @query "currency"
		currency CurrencyCode,
		// @query "market_hash_name"
		marketHashName string,
		mods ...aoni.RequestModifier,
	) (*PriceOverviewResponse, error)

	// @get "market/itemordershistogram"
	// @preset :xhr
	// @referer "market/listings/{appID}/{marketHashName:escape}"
	GetItemOrdersHistogram(
		ctx context.Context,
		appID uint32,
		marketHashName string,
		// @query "country"
		country string,
		// @query "language"
		language string,
		// @query "currency"
		currency CurrencyCode,
		// @query "item_nameid"
		itemNameID uint64,
		// @query "two_factor"
		twoFactor int,
		mods ...aoni.RequestModifier,
	) (*ItemOrdersHistogramResponse, error)

	// @get "market/mylistings"
	// @preset :xhr
	// @referer :origin
	GetMyListings(
		ctx context.Context,
		// @query "start"
		start int,
		// @query "count"
		count int,
		// @query "norender"
		norender int,
		mods ...aoni.RequestModifier,
	) (*MyListingsResponse, error)

	// @get "market"
	// @referer :origin
	GetMarketPage(ctx context.Context, mods ...aoni.RequestModifier) (io.ReadCloser, error)

	// @get "ajaxgetgoovalue"
	// @preset :xhr
	// @referer :origin
	GetGooValue(
		ctx context.Context,
		// @query "appid"
		appID uint32,
		// @query "contextid"
		contextID int64,
		// @query "assetid"
		assetID uint64,
		mods ...aoni.RequestModifier,
	) (*gemValueResponse, error)

	// @post "ajaxgrindintogoo"
	// @form
	// @preset :xhr
	// @inject field="sessionid" from="SessionID"
	// @referer :origin
	GrindIntoGoo(
		ctx context.Context,
		// @field "appid"
		appID uint32,
		// @field "contextid"
		contextID int64,
		// @field "assetid"
		assetID uint64,
		// @field "goo_value_expected"
		gooValueExpected int,
		mods ...aoni.RequestModifier,
	) (*grindGooResponse, error)

	// @post "ajaxunpackbooster"
	// @form
	// @preset :xhr
	// @inject field="sessionid" from="SessionID"
	// @referer :origin
	UnpackBooster(
		ctx context.Context,
		// @field "appid"
		appID uint32,
		// @field "communityitemid"
		communityItemID uint64,
		mods ...aoni.RequestModifier,
	) (*unpackBoosterResponse, error)

	// @get "tradingcards/boostercreator"
	// @referer :origin
	GetBoosterCreatorPage(ctx context.Context, mods ...aoni.RequestModifier) (io.ReadCloser, error)

	// @post "tradingcards/ajaxcreatebooster"
	// @form
	// @preset :xhr
	// @inject field="sessionid" from="SessionID"
	// @referer "tradingcards/boostercreator"
	CreateBooster(
		ctx context.Context,
		// @field "appid"
		appID uint32,
		// @field "series"
		series int,
		// @field "tradability_preference"
		tradabilityPreference int,
		mods ...aoni.RequestModifier,
	) (*createBoosterResponse, error)

	// @post "gifts/{giftID}/validateunpack"
	// @form
	// @preset :xhr
	// @inject field="sessionid" from="SessionID"
	// @referer :origin
	ValidateUnpackGift(
		ctx context.Context,
		giftID uint64,
		mods ...aoni.RequestModifier,
	) (*giftDetailsResponse, error)

	// @post "gifts/{giftID}/unpack"
	// @form
	// @preset :xhr
	// @inject field="sessionid" from="SessionID"
	// @referer :origin
	UnpackGift(
		ctx context.Context,
		giftID uint64,
		mods ...aoni.RequestModifier,
	) (*redeemGiftResponse, error)

	// @post "ajaxexchangegoo"
	// @form
	// @preset :xhr
	// @inject field="sessionid" from="SessionID"
	// @referer :origin
	ExchangeGoo(
		ctx context.Context,
		// @field "appid"
		appID uint32,
		// @field "assetid"
		assetID uint64,
		// @field "goo_denomination_in"
		gooDenomIn int,
		// @field "goo_amount_in"
		gooAmountIn int,
		// @field "goo_denomination_out"
		gooDenomOut int,
		// @field "goo_amount_out_expected"
		gooAmountOutExpected int,
		mods ...aoni.RequestModifier,
	) (*gemExchangeResponse, error)
}
