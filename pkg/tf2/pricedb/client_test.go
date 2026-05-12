// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pricedb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetItem(t *testing.T) {
	sku := "5021;6"

	// Create a mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/item/5021;6", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		resp := Price{
			SKU:  sku,
			Name: "Mann Co. Supply Crate Key",
			Buy:  Currencies{Keys: 0, Metal: 75},
			Sell: Currencies{Keys: 0, Metal: 75.11},
			Time: 1600000000,
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Initialize the client and override the REST client's base URL
	client := NewClient(nil)
	client.restClient = client.restClient.WithBaseURL(server.URL)

	price, err := client.GetItem(context.Background(), sku)

	require.NoError(t, err)
	assert.Equal(t, sku, price.SKU)
	assert.Equal(t, 75.0, price.Buy.Metal)
	assert.Equal(t, 75.11, price.Sell.Metal)
}

func TestClient_GetItemsBulk(t *testing.T) {
	skus := []string{"5021;6", "5002;6"}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/items-bulk", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)

		var req bulkRequest

		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.ElementsMatch(t, skus, req.SKUs)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		resp := []*Price{
			{SKU: "5021;6", Name: "Key", Buy: Currencies{Metal: 75}},
			{SKU: "5002;6", Name: "Refined", Buy: Currencies{Metal: 1}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(nil)
	client.restClient = client.restClient.WithBaseURL(server.URL)

	prices, err := client.GetItemsBulk(context.Background(), skus)

	require.NoError(t, err)
	assert.Len(t, prices, 2)
	assert.Equal(t, "5021;6", prices[0].SKU)
}

func TestClient_TriggerPriceCheck(t *testing.T) {
	sku := "5021;6"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/autob/items/5021;6", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := NewClient(nil)
	client.restClient = client.restClient.WithBaseURL(server.URL)

	err := client.TriggerPriceCheck(context.Background(), sku)
	require.NoError(t, err)
}

func TestClient_ResolveSKU(t *testing.T) {
	sku := "1;6"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/sku/1;6", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		resp := map[string]any{
			"defindex": 1,
			"quality":  6,
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(nil)
	// Note: ResolveSKU uses skuClient, not restClient!
	client.skuClient = client.skuClient.WithBaseURL(server.URL)

	info, err := client.ResolveSKU(context.Background(), sku)

	require.NoError(t, err)
	assert.Equal(t, float64(1), info["defindex"]) // JSON unmarshals numbers to float64
	assert.Equal(t, float64(6), info["quality"])
}

func TestClient_Search(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/search", r.URL.Path)
		assert.Equal(t, "key", r.URL.Query().Get("q"))
		assert.Equal(t, "10", r.URL.Query().Get("limit"))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(SearchResult{Total: 1})
	}))
	defer server.Close()

	client := NewClient(nil)
	client.restClient = client.restClient.WithBaseURL(server.URL)

	res, err := client.Search(context.Background(), "key", 10)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Total)
}

func TestClient_GetHistory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/item-history/5021;6", r.URL.Path)
		assert.Equal(t, "1000", r.URL.Query().Get("start"))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]*Price{{SKU: "5021;6"}})
	}))
	defer server.Close()

	client := NewClient(nil)
	client.restClient = client.restClient.WithBaseURL(server.URL)

	history, err := client.GetHistory(context.Background(), "5021;6", 1000, 0)
	require.NoError(t, err)
	assert.Len(t, history, 1)
}

func TestClient_Compare(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/compare/1;6/2;6", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")

		resp := CompareResult{}
		resp.Comparison.BuyDifference.Metal = 10.5
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(nil)
	client.restClient = client.restClient.WithBaseURL(server.URL)

	res, err := client.Compare(context.Background(), "1;6", "2;6")
	require.NoError(t, err)
	assert.Equal(t, 10.5, res.Comparison.BuyDifference.Metal)
}
