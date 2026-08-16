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

// API is an interface for the Steam Community Market endpoints.
//
// @aoni:service casing=snake_case
// @engine custom type="community.Requester" required
// @base_url "https://steamcommunity.com"
type API interface {
	// @post "market/sellitem"
	// @preset :xhr
	// @form casing=flatcase
	// @inject field="sessionid" from="SessionID"
	// @referer "profiles/{steamID}/inventory?modal=1&market=1"
	SellItem(
		ctx context.Context,
		appID uint32,
		contextID int64,
		assetID uint64,
		amount, price int,
		steamID id.ID,
		mods ...aoni.RequestModifier,
	) (*CreateSellOrderResponse, error)

	// @post "market/createbuyorder"
	// @preset :xhr
	// @form casing=snake_case
	// @inject field="sessionid" from="SessionID"
	// @referer "market/listings/{appID}/{marketHashName:escape}"
	CreateBuyOrder(
		ctx context.Context,
		appID uint32,
		currency CurrencyCode,
		marketHashName string,
		priceTotal string,
		quantity int,
		billingState string,
		saveMyAddress string,
		mods ...aoni.RequestModifier,
	) (*CreateBuyOrderResponse, error)

	// @post "market/cancelbuyorder"
	// @preset :xhr
	// @form
	// @inject field="sessionid" from="SessionID"
	// @referer :origin
	CancelBuyOrder(
		ctx context.Context,
		buyOrderID uint64, // @field "buy_orderid"
		mods ...aoni.RequestModifier,
	) (*basicMarketResponse, error)

	// @post "market/removelisting/{listingID}"
	// @preset :xhr
	// @form
	// @inject field="sessionid" from="SessionID"
	// @referer :origin
	RemoveListing(ctx context.Context, listingID uint64, mods ...aoni.RequestModifier) (*basicMarketResponse, error)

	// @get "market/search/render"
	// @preset :xhr
	// @referer "market/search?appid={appID}"
	Search(
		ctx context.Context,
		appID uint32,
		opts SearchOptions,
		mods ...aoni.RequestModifier,
	) (*SearchResponse, error)

	// @get "market/priceoverview"
	// @preset :xhr
	// @query casing=flatcase
	// @referer "market/listings/{appID}/{marketHashName:escape}"
	GetPriceOverview(
		ctx context.Context,
		appID uint32,
		currency CurrencyCode,
		marketHashName string, // @query "market_hash_name"
		mods ...aoni.RequestModifier,
	) (*PriceOverviewResponse, error)

	// @get "market/itemordershistogram"
	// @preset :xhr
	// @query casing=snake_case
	// @referer "market/listings/{appID}/{marketHashName:escape}"
	GetItemOrdersHistogram(
		ctx context.Context,
		appID uint32,
		marketHashName string, // @var
		country string,
		language string,
		currency CurrencyCode,
		itemNameID uint64, // @query "item_nameid"
		twoFactor int,
		mods ...aoni.RequestModifier,
	) (*ItemOrdersHistogramResponse, error)

	// @get "market/mylistings"
	// @preset :xhr
	// @query casing=snake_case
	// @referer :origin
	GetMyListings(ctx context.Context, start, count, norender int, mods ...aoni.RequestModifier) (*MyListingsResponse, error)

	// @get "market"
	// @referer :origin
	GetMarketPage(ctx context.Context, mods ...aoni.RequestModifier) (io.ReadCloser, error)

	// @get "ajaxgetgoovalue"
	// @preset :xhr
	// @query casing=flatcase
	// @referer :origin
	GetGooValue(ctx context.Context, appID uint32, contextID int64, assetID uint64, mods ...aoni.RequestModifier) (*gemValueResponse, error)

	// @post "ajaxgrindintogoo"
	// @preset :xhr
	// @form casing=flatcase
	// @inject field="sessionid" from="SessionID"
	// @referer :origin
	GrindIntoGoo(
		ctx context.Context,
		appID uint32,
		contextID int64,
		assetID uint64,
		gooValueExpected int, // @field "goo_value_expected"
		mods ...aoni.RequestModifier,
	) (*grindGooResponse, error)

	// @post "ajaxunpackbooster"
	// @preset :xhr
	// @form casing=flatcase
	// @inject field="sessionid" from="SessionID"
	// @referer :origin
	UnpackBooster(ctx context.Context, appID uint32, communityItemID uint64, mods ...aoni.RequestModifier) (*unpackBoosterResponse, error)

	// @get "tradingcards/boostercreator"
	// @referer :origin
	GetBoosterCreatorPage(ctx context.Context, mods ...aoni.RequestModifier) (io.ReadCloser, error)

	// @post "tradingcards/ajaxcreatebooster"
	// @preset :xhr
	// @form casing=snake_case
	// @inject field="sessionid" from="SessionID"
	// @referer "tradingcards/boostercreator"
	CreateBooster(
		ctx context.Context,
		appID uint32,
		series int,
		tradabilityPreference int,
		mods ...aoni.RequestModifier,
	) (*createBoosterResponse, error)

	// @post "gifts/{giftID}/validateunpack"
	// @preset :xhr
	// @form
	// @inject field="sessionid" from="SessionID"
	// @referer :origin
	ValidateUnpackGift(ctx context.Context, giftID uint64, mods ...aoni.RequestModifier) (*giftDetailsResponse, error)

	// @post "gifts/{giftID}/unpack"
	// @preset :xhr
	// @form
	// @inject field="sessionid" from="SessionID"
	// @referer :origin
	UnpackGift(ctx context.Context, giftID uint64, mods ...aoni.RequestModifier) (*redeemGiftResponse, error)

	// @post "ajaxexchangegoo"
	// @preset :xhr
	// @form casing=flatcase
	// @inject field="sessionid" from="SessionID"
	// @referer :origin
	ExchangeGoo(
		ctx context.Context,
		appID uint32,
		assetID uint64,
		gooDenomIn int, // @field "goo_denomination_in"
		gooAmountIn int, // @field "goo_amount_in"
		gooDenomOut int, // @field "goo_denomination_out"
		gooAmountOutExpected int, // @field "goo_amount_out_expected"
		mods ...aoni.RequestModifier,
	) (*gemExchangeResponse, error)
}
