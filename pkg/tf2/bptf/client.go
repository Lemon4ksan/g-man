// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bptf

import (
	"context"
	"strconv"
	"strings"

	"github.com/lemon4ksan/g-man/pkg/rest"
)

type Client struct {
	restClient *rest.Client
	apiKey     string
	userToken  string
}

func New(httpClient rest.HTTPDoer, apiKey, userToken string) *Client {
	c := rest.NewClient(httpClient).
		WithBaseURL("https://backpack.tf/api").
		WithHeader("User-Agent", "G-man SDK/1.0")

	if apiKey != "" {
		c = c.WithHeader("X-Api-Key", apiKey)
	}
	if userToken != "" {
		c = c.WithHeader("X-Auth-Token", userToken)
	}

	return &Client{
		restClient: c,
		apiKey:     apiKey,
		userToken:  userToken,
	}
}

// REST returns a low-level REST client for specific tasks (e.g. scraping).
func (c *Client) REST() *rest.Client {
	return c.restClient
}

// GetPricesV4 returns the current pricing scheme (IGetPrices/v4).
func (c *Client) GetPricesV4(ctx context.Context, raw int, since int64) (*PricesResponseV4, error) {
	req := struct {
		Raw   int   `url:"raw,omitempty"`
		Since int64 `url:"since,omitempty"`
	}{raw, since}
	return rest.GetJSON[PricesResponseV4](ctx, c.restClient, "/IGetPrices/v4", req)
}

// GetCurrencies returns a list of currencies (IGetCurrencies/v1).
func (c *Client) GetCurrencies(ctx context.Context, raw int) (*CurrenciesResponseV1, error) {
	req := struct {
		Raw int `url:"raw,omitempty"`
	}{raw}
	return rest.GetJSON[CurrenciesResponseV1](ctx, c.restClient, "/IGetCurrencies/v1", req)
}

// CreateListing creates a buy or sell listing.
// For selling, the item's AssetID is passed; for buying, the item's attributes are passed.
func (c *Client) CreateListing(ctx context.Context, listing ListingResolvable) (*ListingResponse, error) {
	return rest.PostJSON[ListingResolvable, ListingResponse](
		ctx,
		c.restClient,
		"/v2/classifieds/listings",
		listing,
		nil,
	)
}

// BatchCreateListings allows you to create up to 100 listings in one request.
func (c *Client) BatchCreateListings(ctx context.Context, listings []ListingResolvable) ([]ListingBatchCreateResult, error) {
	resp, err := rest.PostJSON[[]ListingResolvable, []ListingBatchCreateResult](
		ctx,
		c.restClient,
		"/v2/classifieds/listings/batch",
		listings,
		nil,
	)
	return *resp, err
}

// GetInventoryStatus returns the status of a user's inventory on backpack.tf.
// It does not trigger a refresh, only returns the current cached state.
func (c *Client) GetInventoryStatus(ctx context.Context, steamID uint64) (InventoryStatus, error) {
	path := "/inventory/" + strconv.FormatInt(int64(steamID), 10) + "/status"
	resp, err := rest.GetJSON[InventoryStatus](ctx, c.restClient, path, nil)
	if err != nil {
		return InventoryStatus{}, err
	}
	return *resp, nil
}

// GetInventoryValues returns the total value of a user's inventory.
func (c *Client) GetInventoryValues(ctx context.Context, steamID uint64) (InventoryValues, error) {
	path := "/inventory/" + strconv.FormatInt(int64(steamID), 10) + "/values"
	resp, err := rest.GetJSON[InventoryValues](ctx, c.restClient, path, nil)
	if err != nil {
		return InventoryValues{}, err
	}
	return *resp, nil
}

// RefreshInventory requests backpack.tf to fetch the latest data from Steam.
// Note: This endpoint is non-blocking and heavily rate-limited.
func (c *Client) RefreshInventory(ctx context.Context, steamID uint64) (InventoryStatus, error) {
	path := "/inventory/" + strconv.FormatInt(int64(steamID), 10) + "/refresh"
	resp, err := rest.PostJSON[any, InventoryStatus](ctx, c.restClient, path, nil, nil)
	if err != nil {
		return InventoryStatus{}, err
	}
	return *resp, nil
}

// GetUsersInfo returns detailed information for a list of SteamIDs.
// bptf accepts a comma-separated list of IDs.
func (c *Client) GetUsersInfo(ctx context.Context, steamIDs []string) (V1UserResponse, error) {
	req := struct {
		SteamIDs string `url:"steamids"`
	}{SteamIDs: strings.Join(steamIDs, ",")}

	resp, err := rest.GetJSON[V1UserResponse](ctx, c.restClient, "/users/info/v1", req)
	if err != nil {
		return V1UserResponse{}, err
	}
	return *resp, nil
}

// GetAlerts returns a list of active listing alerts for the current user.
func (c *Client) GetAlerts(ctx context.Context, skip, limit int) (AlertsResponse, error) {
	req := struct {
		Skip  int `url:"skip,omitempty"`
		Limit int `url:"limit,omitempty"`
	}{skip, limit}

	resp, err := rest.GetJSON[AlertsResponse](ctx, c.restClient, "/classifieds/alerts", req)
	if err != nil {
		return AlertsResponse{}, err
	}
	return *resp, nil
}

// CreateAlert creates a new listing alert for a specific item.
func (c *Client) CreateAlert(ctx context.Context, itemName string, intent string, currency string, min, max int) (Alert, error) {
	req := struct {
		ItemName string `url:"item_name"`
		Intent   string `url:"intent"`
		Currency string `url:"currency,omitempty"`
		Min      int    `url:"min,omitempty"`
		Max      int    `url:"max,omitempty"`
	}{itemName, intent, currency, min, max}

	resp, err := rest.PostJSON[any, Alert](ctx, c.restClient, "/classifieds/alerts", nil, req)
	if err != nil {
		return Alert{}, err
	}
	return *resp, nil
}
