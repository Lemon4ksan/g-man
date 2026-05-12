// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bptf

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/log"
)

func TestPriceManager_Update(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		// Mock GetPricesV4 response
		resp := PricesResponseV4{
			Success: 1,
			Items: map[string]BaseItemDoc{
				"Mann Co. Supply Crate Key": {
					Defindexes: []string{"5021"},
					Prices: map[string]map[string]map[string]map[string]PriceEntry{
						"6": { // Unique
							"Tradable": {
								"Craftable": {
									"0": { // No priceindex
										Value: 75,
									},
								},
							},
						},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(nil, "", "")
	client.restClient = client.restClient.WithBaseURL(server.URL)

	manager := NewPriceManager(client, log.Discard, Config{})

	err := manager.Update(context.Background())
	require.NoError(t, err)

	// Key SKU should be "5021;6"
	price, ok := manager.GetPrice("5021;6")
	assert.True(t, ok)
	assert.Equal(t, float64(75), price.Value)

	// Unusual SKU with effect 19 (Burning Flames) -> "5021;5;u19"
	// Wait, key can't be unusual but for test logic it's fine if mocked.
	// Let's use a real unusual item defindex like 1.
}

func TestPriceManager_ComplexSKUs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		resp := PricesResponseV4{
			Success: 1,
			Items: map[string]BaseItemDoc{
				"Item": {
					Defindexes: []string{"1"},
					Prices: map[string]map[string]map[string]map[string]PriceEntry{
						"5": { // Unusual
							"Tradable": {
								"Craftable": {
									"19": {Value: 100}, // Effect 19
								},
							},
						},
						"6": { // Unique
							"Tradable": {
								"Craftable": {
									"82": {Value: 10}, // Crate series 82
								},
							},
						},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(nil, "", "")
	client.restClient = client.restClient.WithBaseURL(server.URL)
	manager := NewPriceManager(client, log.Discard, Config{})

	err := manager.Update(context.Background())
	require.NoError(t, err)

	// Unusual: 1;5;u19
	p1, ok := manager.GetPrice("1;5;u19")
	assert.True(t, ok)
	assert.Equal(t, float64(100), p1.Value)

	// Crate: 1;6;c82
	p2, ok := manager.GetPrice("1;6;c82")
	assert.True(t, ok)
	assert.Equal(t, float64(10), p2.Value)
}
