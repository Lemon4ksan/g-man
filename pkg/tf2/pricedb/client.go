// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pricedb

import (
	"context"
	"net/url"

	"github.com/lemon4ksan/g-man/pkg/rest"
)

const (
	BaseURL = "https://api.pricedb.io"
	SKUURL  = "https://sku.pricedb.io"
)

// Client is a thread-safe HTTP client for interacting with PriceDB.
type Client struct {
	restClient *rest.Client
	skuClient  *rest.Client
}

// NewClient creates a new PriceDB API client.
// If httpClient is nil, a default robust client is created.
func NewClient(httpClient rest.HTTPDoer) *Client {
	return &Client{
		restClient: rest.NewClient(httpClient).WithBaseURL(BaseURL).WithHeader("User-Agent", "G-man Bot/1.0"),
		skuClient:  rest.NewClient(httpClient).WithBaseURL(SKUURL).WithHeader("User-Agent", "G-man Bot/1.0"),
	}
}

// GetItem fetches the latest price for a specific item SKU.
func (c *Client) GetItem(ctx context.Context, sku string) (*Price, error) {
	path := "/api/item/" + url.PathEscape(sku)
	return rest.GetJSON[Price](ctx, c.restClient, path, nil)
}

// GetItemsBulk fetches the latest prices for an array of SKUs in a single request.
func (c *Client) GetItemsBulk(ctx context.Context, skus []string) ([]*Price, error) {
	req := bulkRequest{SKUs: skus}
	resp, err := rest.PostJSON[bulkRequest, []*Price](ctx, c.restClient, "/api/items-bulk", req, nil)

	return *resp, err
}

// Search performs a fuzzy search for items by name.
func (c *Client) Search(ctx context.Context, query string, limit int) (*SearchResult, error) {
	req := struct {
		Q     string `url:"q"`
		Limit int    `url:"limit,omitempty"`
	}{query, limit}

	return rest.GetJSON[SearchResult](ctx, c.restClient, "/api/search", req)
}

// GetHistory returns the price history for a specific SKU.
// start and end are optional Unix timestamps (use 0 to ignore).
func (c *Client) GetHistory(ctx context.Context, sku string, start, end int64) ([]*Price, error) {
	path := "/api/item-history/" + url.PathEscape(sku)
	req := struct {
		Start int64 `url:"start,omitempty"`
		End   int64 `url:"end,omitempty"`
	}{start, end}
	resp, err := rest.GetJSON[[]*Price](ctx, c.restClient, path, req)

	return *resp, err
}

// GetStats returns statistics (min, max, avg) for an item's price history.
func (c *Client) GetStats(ctx context.Context, sku string) (*ItemStats, error) {
	path := "/api/item-stats/" + url.PathEscape(sku)
	return rest.GetJSON[ItemStats](ctx, c.restClient, path, nil)
}

// Compare compares two items side by side, returning the price differences.
func (c *Client) Compare(ctx context.Context, sku1, sku2 string) (*CompareResult, error) {
	path := "/api/compare/" + url.PathEscape(sku1) + "/" + url.PathEscape(sku2)
	return rest.GetJSON[CompareResult](ctx, c.restClient, path, nil)
}

// TriggerPriceCheck requests PriceDB to update the price for a specific SKU.
// This hits the Autobot integration endpoint.
func (c *Client) TriggerPriceCheck(ctx context.Context, sku string) error {
	path := "/api/autob/items/" + url.PathEscape(sku)
	// We don't care about the response body, just the HTTP status code
	_, err := rest.PostJSON[any, any](ctx, c.restClient, path, nil, nil)

	return err
}

// HealthCheck returns the current system statistics and health of the API.
func (c *Client) HealthCheck(ctx context.Context) (*CacheStats, error) {
	return rest.GetJSON[CacheStats](ctx, c.restClient, "/api/cache-stats", nil)
}

// ResolveName looks up an item by name using the SKU Service.
func (c *Client) ResolveName(ctx context.Context, name string) (map[string]any, error) {
	path := "/api/name/" + url.PathEscape(name)
	resp, err := rest.GetJSON[map[string]any](ctx, c.skuClient, path, nil)

	return *resp, err
}

// ResolveSKU looks up item properties by its SKU using the SKU Service.
func (c *Client) ResolveSKU(ctx context.Context, sku string) (map[string]any, error) {
	path := "/api/sku/" + url.PathEscape(sku)
	resp, err := rest.GetJSON[map[string]any](ctx, c.skuClient, path, nil)

	return *resp, err
}
