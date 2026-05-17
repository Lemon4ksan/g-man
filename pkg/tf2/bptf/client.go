// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bptf

import (
	"context"
	"strings"

	"github.com/lemon4ksan/g-man/pkg/rest"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
)

// Client is a client for backpack.tf API.
type Client struct {
	restClient *rest.Client
}

// New creates a new client for backpack.tf API.
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
func (c *Client) BatchCreateListings(
	ctx context.Context,
	listings []ListingResolvable,
) ([]ListingBatchCreateResult, error) {
	resp, err := rest.PostJSON[[]ListingResolvable, []ListingBatchCreateResult](
		ctx,
		c.restClient,
		"/v2/classifieds/listings/batch",
		listings,
		nil,
	)
	if err != nil {
		return nil, err
	}

	return *resp, nil
}

// GetInventoryStatus returns the status of a user's inventory on backpack.tf.
// It does not trigger a refresh, only returns the current cached state.
func (c *Client) GetInventoryStatus(ctx context.Context, steamID id.ID) (InventoryStatus, error) {
	path := "/inventory/" + steamID.String() + "/status"

	resp, err := rest.GetJSON[InventoryStatus](ctx, c.restClient, path, nil)
	if err != nil {
		return InventoryStatus{}, err
	}

	return *resp, nil
}

// GetInventoryValues returns the total value of a user's inventory.
func (c *Client) GetInventoryValues(ctx context.Context, steamID id.ID) (InventoryValues, error) {
	path := "/inventory/" + steamID.String() + "/values"

	resp, err := rest.GetJSON[InventoryValues](ctx, c.restClient, path, nil)
	if err != nil {
		return InventoryValues{}, err
	}

	return *resp, nil
}

// RefreshInventory requests backpack.tf to fetch the latest data from Steam.
// Note: This endpoint is non-blocking and heavily rate-limited.
func (c *Client) RefreshInventory(ctx context.Context, steamID id.ID) (InventoryStatus, error) {
	path := "/inventory/" + steamID.String() + "/refresh"

	resp, err := rest.PostJSON[any, InventoryStatus](ctx, c.restClient, path, nil, nil)
	if err != nil {
		return InventoryStatus{}, err
	}

	return *resp, nil
}

// GetUsersInfo returns detailed information for a list of SteamIDs.
// bptf accepts a comma-separated list of IDs.
func (c *Client) GetUsersInfo(ctx context.Context, steamIDs []id.ID) (V1UserResponse, error) {
	ids := make([]string, len(steamIDs))
	for i, steamID := range steamIDs {
		ids[i] = steamID.String()
	}

	req := struct {
		SteamIDs string `url:"steamids"`
	}{SteamIDs: strings.Join(ids, ",")}

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
func (c *Client) CreateAlert(ctx context.Context, itemName, intent, currency string, min, max int) (Alert, error) {
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

// GetListings returns a list of active listings for the current account.
// It uses a scrollable cursor for pagination.
func (c *Client) GetListings(ctx context.Context, skip, limit int) (ListingsResponse, error) {
	req := struct {
		Skip  int `url:"skip,omitempty"`
		Limit int `url:"limit,omitempty"`
	}{skip, limit}

	resp, err := rest.GetJSON[ListingsResponse](ctx, c.restClient, "/v2/classifieds/listings", req)
	if err != nil {
		return ListingsResponse{}, err
	}

	return *resp, nil
}

// DeleteListing deletes a single listing by its ID.
func (c *Client) DeleteListing(ctx context.Context, id string) error {
	path := "/v2/classifieds/listings/" + id
	_, err := rest.DeleteJSON[any, any](ctx, c.restClient, path, nil, nil)
	return err
}

// BatchDeleteListings deletes multiple listings at once (up to 100).
func (c *Client) BatchDeleteListings(ctx context.Context, ids []string) error {
	req := struct {
		IDs []string `json:"listing_ids"`
	}{IDs: ids}

	_, err := rest.DeleteJSON[any, any](ctx, c.restClient, "/v2/classifieds/listings/batch", req, nil)

	return err
}

// Pulse sends a heartbeat to backpack.tf to keep the bot online and bump listings.
func (c *Client) Pulse(ctx context.Context) (UserAgentStatus, error) {
	resp, err := rest.PostJSON[any, UserAgentStatus](ctx, c.restClient, "/agent/pulse", nil, nil)
	if err != nil {
		return UserAgentStatus{}, err
	}

	return *resp, nil
}

// StopAgent declares the user as no longer under control of the agent.
func (c *Client) StopAgent(ctx context.Context) (UserAgentStatus, error) {
	resp, err := rest.PostJSON[any, UserAgentStatus](ctx, c.restClient, "/agent/stop", nil, nil)
	if err != nil {
		return UserAgentStatus{}, err
	}

	return *resp, nil
}

// GetAgentStatus returns the current status of the user agent.
func (c *Client) GetAgentStatus(ctx context.Context) (UserAgentStatus, error) {
	resp, err := rest.PostJSON[any, UserAgentStatus](ctx, c.restClient, "/agent/status", nil, nil)
	if err != nil {
		return UserAgentStatus{}, err
	}

	return *resp, nil
}

// GetNotifications returns user notifications.
func (c *Client) GetNotifications(ctx context.Context, skip, limit int, unread bool) (NotificationsResponse, error) {
	unreadInt := 0
	if unread {
		unreadInt = 1
	}

	req := struct {
		Skip   int `url:"skip,omitempty"`
		Limit  int `url:"limit,omitempty"`
		Unread int `url:"unread,omitempty"`
	}{skip, limit, unreadInt}

	resp, err := rest.GetJSON[NotificationsResponse](ctx, c.restClient, "/notifications", req)
	if err != nil {
		return NotificationsResponse{}, err
	}

	return *resp, nil
}

// MarkNotificationsRead marks all unread notifications as read.
func (c *Client) MarkNotificationsRead(ctx context.Context) (NotificationMarkResponse, error) {
	resp, err := rest.PostJSON[any, NotificationMarkResponse](ctx, c.restClient, "/notifications/mark", nil, nil)
	if err != nil {
		return NotificationMarkResponse{}, err
	}

	return *resp, nil
}

// DeleteNotification deletes a notification by ID.
func (c *Client) DeleteNotification(ctx context.Context, id string) error {
	path := "/notifications/" + id
	_, err := rest.DeleteJSON[any, any](ctx, c.restClient, path, nil, nil)
	return err
}

// GetPriceHistory returns price history for an item.
func (c *Client) GetPriceHistory(
	ctx context.Context,
	appid int,
	item, quality, tradable, craftable, priceindex string,
) (PriceHistoryResponse, error) {
	req := struct {
		AppID      int    `url:"appid"`
		Item       string `url:"item"`
		Quality    string `url:"quality"`
		Tradable   string `url:"tradable"`
		Craftable  string `url:"craftable"`
		PriceIndex string `url:"priceindex,omitempty"`
	}{appid, item, quality, tradable, craftable, priceindex}

	resp, err := rest.GetJSON[PriceHistoryResponse](ctx, c.restClient, "/IGetPriceHistory/v1", req)
	if err != nil {
		return PriceHistoryResponse{}, err
	}

	return *resp, nil
}

// DeleteAlertByID deletes an alert by its ID.
func (c *Client) DeleteAlertByID(ctx context.Context, id string) error {
	path := "/classifieds/alerts/" + id
	_, err := rest.DeleteJSON[any, any](ctx, c.restClient, path, nil, nil)
	return err
}

// DeleteAlertByItem deletes an alert by item name and intent.
func (c *Client) DeleteAlertByItem(ctx context.Context, itemName, intent string) error {
	req := struct {
		ItemName string `url:"item_name"`
		Intent   string `url:"intent"`
	}{itemName, intent}

	_, err := rest.DeleteJSON[any, any](ctx, c.restClient, "/classifieds/alerts", nil, req)

	return err
}

// GetArchiveListings returns archived listings for the current account.
func (c *Client) GetArchiveListings(ctx context.Context, skip, limit int) (ListingsResponse, error) {
	req := struct {
		Skip  int `url:"skip,omitempty"`
		Limit int `url:"limit,omitempty"`
	}{skip, limit}

	resp, err := rest.GetJSON[ListingsResponse](ctx, c.restClient, "/v2/classifieds/archive", req)
	if err != nil {
		return ListingsResponse{}, err
	}

	return *resp, nil
}

// SearchClassifieds fetches active classified listings for a specific item SKU.
func (c *Client) SearchClassifieds(ctx context.Context, sku, intent string) (*SnapshotResponse, error) {
	req := struct {
		SKU   string `url:"sku"`
		AppID int    `url:"appid"`
	}{
		SKU:   sku,
		AppID: 440,
	}

	resp, err := rest.GetJSON[SnapshotResponse](ctx, c.restClient, "/classifieds/listings/snapshot", req)
	if err != nil {
		return nil, err
	}

	// Filter listings by intent on the client side
	var filtered []ListingResponse
	for _, l := range resp.Listings {
		if l.Intent == intent {
			filtered = append(filtered, l)
		}
	}

	resp.Listings = filtered

	return resp, nil
}

// DeleteArchiveListings deletes all archived listings for the account.
// You may specify an optional intent filter inside req.
func (c *Client) DeleteArchiveListings(ctx context.Context, req ListingDropRequest) error {
	_, err := rest.DeleteJSON[ListingDropRequest, any](ctx, c.restClient, "/v2/classifieds/archive", req, nil)
	return err
}

// GetArchiveBatchLimit returns the batch operations limit for archived listings.
func (c *Client) GetArchiveBatchLimit(ctx context.Context) (map[string]any, error) {
	resp, err := rest.GetJSON[map[string]any](ctx, c.restClient, "/v2/classifieds/archive/batch", nil)
	if err != nil {
		return nil, err
	}

	return *resp, nil
}

// BatchDeleteArchiveListings performs a batch deletion of archived listings.
func (c *Client) BatchDeleteArchiveListings(ctx context.Context) (map[string]any, error) {
	resp, err := rest.DeleteJSON[any, map[string]any](ctx, c.restClient, "/v2/classifieds/archive/batch", nil, nil)
	if err != nil {
		return nil, err
	}

	return *resp, nil
}

// GetArchiveListing retrieves a single archived listing by its ID.
func (c *Client) GetArchiveListing(ctx context.Context, listingID string) (ListingResponse, error) {
	resp, err := rest.GetJSON[ListingResponse](ctx, c.restClient, "/v2/classifieds/archive/"+listingID, nil)
	if err != nil {
		return ListingResponse{}, err
	}

	return *resp, nil
}

// DeleteArchiveListing deletes a single archived listing by its ID.
func (c *Client) DeleteArchiveListing(ctx context.Context, listingID string) error {
	_, err := rest.DeleteJSON[any, any](ctx, c.restClient, "/v2/classifieds/archive/"+listingID, nil, nil)
	return err
}

// PatchArchiveListing updates properties of a single archived listing by its ID.
func (c *Client) PatchArchiveListing(
	ctx context.Context,
	listingID string,
	req ListingPatchRequest,
) (ListingResponse, error) {
	resp, err := rest.PatchJSON[ListingPatchRequest, ListingResponse](
		ctx,
		c.restClient,
		"/v2/classifieds/archive/"+listingID,
		req,
		nil,
	)
	if err != nil {
		return ListingResponse{}, err
	}

	return *resp, nil
}

// PublishArchiveListing publishes a single archived listing to the active pool.
func (c *Client) PublishArchiveListing(ctx context.Context, listingID string) (ListingResponse, error) {
	resp, err := rest.PostJSON[any, ListingResponse](
		ctx,
		c.restClient,
		"/v2/classifieds/archive/"+listingID+"/publish",
		nil,
		nil,
	)
	if err != nil {
		return ListingResponse{}, err
	}

	return *resp, nil
}

// DeleteAllListings deletes all active listings for the account.
// You may specify an optional intent filter inside req.
func (c *Client) DeleteAllListings(ctx context.Context, req ListingDropRequest) error {
	_, err := rest.DeleteJSON[ListingDropRequest, any](ctx, c.restClient, "/v2/classifieds/listings", req, nil)
	return err
}

// GetListingsBatchLimit returns the batch operations limit for active listings.
func (c *Client) GetListingsBatchLimit(ctx context.Context) (map[string]any, error) {
	resp, err := rest.GetJSON[map[string]any](ctx, c.restClient, "/v2/classifieds/listings/batch", nil)
	if err != nil {
		return nil, err
	}

	return *resp, nil
}

// GetListing retrieves a single active listing by its ID.
func (c *Client) GetListing(ctx context.Context, listingID string) (ListingResponse, error) {
	resp, err := rest.GetJSON[ListingResponse](ctx, c.restClient, "/v2/classifieds/listings/"+listingID, nil)
	if err != nil {
		return ListingResponse{}, err
	}

	return *resp, nil
}

// PatchListing updates properties of a single active listing by its ID.
func (c *Client) PatchListing(ctx context.Context, listingID string, req ListingPatchRequest) (ListingResponse, error) {
	resp, err := rest.PatchJSON[ListingPatchRequest, ListingResponse](
		ctx,
		c.restClient,
		"/v2/classifieds/listings/"+listingID,
		req,
		nil,
	)
	if err != nil {
		return ListingResponse{}, err
	}

	return *resp, nil
}

// ArchiveListing moves an active listing to the archive.
func (c *Client) ArchiveListing(ctx context.Context, listingID string) (ListingResponse, error) {
	resp, err := rest.PostJSON[any, ListingResponse](
		ctx,
		c.restClient,
		"/v2/classifieds/listings/"+listingID+"/archive",
		nil,
		nil,
	)
	if err != nil {
		return ListingResponse{}, err
	}

	return *resp, nil
}

// PromoteListing promotes an active listing.
func (c *Client) PromoteListing(ctx context.Context, listingID string) (ListingResponse, error) {
	resp, err := rest.PostJSON[any, ListingResponse](
		ctx,
		c.restClient,
		"/v2/classifieds/listings/"+listingID+"/promote",
		nil,
		nil,
	)
	if err != nil {
		return ListingResponse{}, err
	}

	return *resp, nil
}

// DemoteListing demotes an active listing.
func (c *Client) DemoteListing(ctx context.Context, listingID string) (ListingResponse, error) {
	resp, err := rest.PostJSON[any, ListingResponse](
		ctx,
		c.restClient,
		"/v2/classifieds/listings/"+listingID+"/demote",
		nil,
		nil,
	)
	if err != nil {
		return ListingResponse{}, err
	}

	return *resp, nil
}

// ArchiveAllListings moves all active listings to the archive.
func (c *Client) ArchiveAllListings(ctx context.Context) error {
	_, err := rest.PostJSON[any, any](ctx, c.restClient, "/v2/classifieds/listings/archiveAll", nil, nil)
	return err
}

// PublishAllListings publishes all archived listings back to the active pool.
func (c *Client) PublishAllListings(ctx context.Context) error {
	_, err := rest.PostJSON[any, any](ctx, c.restClient, "/v2/classifieds/listings/publishAll", nil, nil)
	return err
}
