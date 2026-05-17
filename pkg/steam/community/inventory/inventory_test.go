// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inventory_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/community/inventory"
	"github.com/lemon4ksan/g-man/test/requester"
)

// Helper to create a JSON response body for the mock
func jsonResponse(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func TestGetUserInventoryContents(t *testing.T) {
	ctx := context.Background()
	userID := uint64(76561198000000000)
	appID := uint32(730)
	contextID := int64(2)

	t.Run("Success Single Page", func(t *testing.T) {
		mock := requester.New()

		resp := inventoryResponseMock{
			Success:    true,
			TotalCount: 2,
			Assets: []inventory.Asset{
				{AssetID: "1", ClassID: "100", InstanceID: "1", Amount: "1"},
				{AssetID: "2", ClassID: "200", InstanceID: "2", Amount: "1", CurrencyID: "500"},
			},
			Descriptions: []inventory.Description{
				{ClassID: "100", InstanceID: "1", Name: "Item 1", Tradable: 1},
				{ClassID: "200", InstanceID: "2", Name: "Currency 1", Tradable: 1},
			},
			MoreItems: false,
		}

		mock.OnRest = func(method, path string, body []byte) (*http.Response, error) {
			expectedPath := fmt.Sprintf("inventory/%d/%d/%d", userID, appID, contextID)
			assert.Contains(t, path, expectedPath)

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       requester.NewBuffer(jsonResponse(resp)),
			}, nil
		}

		inv, curr, total, err := inventory.GetUserInventoryContents(ctx, mock, userID, appID, contextID, false, "")

		require.NoError(t, err)
		assert.Equal(t, 2, total)
		assert.Len(t, inv, 1)
		assert.Len(t, curr, 1)
		assert.Equal(t, "Item 1", inv[0].Description.Name)
		assert.Equal(t, "Currency 1", curr[0].Description.Name)
		assert.Equal(t, 1, inv[0].Asset.Pos)
		assert.Equal(t, 2, curr[0].Asset.Pos)
	})

	t.Run("Success Pagination", func(t *testing.T) {
		mock := requester.New()
		callCount := 0

		mock.OnRest = func(method, path string, body []byte) (*http.Response, error) {
			callCount++

			var resp inventoryResponseMock
			if callCount == 1 {
				resp = inventoryResponseMock{
					Success:      true,
					TotalCount:   2,
					MoreItems:    true,
					LastAssetID:  "100",
					Assets:       []inventory.Asset{{AssetID: "1", ClassID: "A", InstanceID: "A"}},
					Descriptions: []inventory.Description{{ClassID: "A", InstanceID: "A"}},
				}
			} else {
				resp = inventoryResponseMock{
					Success:      true,
					TotalCount:   2,
					MoreItems:    false,
					Assets:       []inventory.Asset{{AssetID: "2", ClassID: "B", InstanceID: "B"}},
					Descriptions: []inventory.Description{{ClassID: "B", InstanceID: "B"}},
				}
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       requester.NewBuffer(jsonResponse(resp)),
			}, nil
		}

		inv, _, total, err := inventory.GetUserInventoryContents(ctx, mock, userID, appID, contextID, false, "russian")

		require.NoError(t, err)
		assert.Equal(t, 2, callCount)
		assert.Equal(t, 2, total)
		assert.Len(t, inv, 2)
	})

	t.Run("Empty Inventory", func(t *testing.T) {
		mock := requester.New()
		resp := inventoryResponseMock{Success: true, TotalCount: 0, Assets: nil}

		mock.OnRest = func(method, path string, body []byte) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       requester.NewBuffer(jsonResponse(resp)),
			}, nil
		}

		inv, curr, total, err := inventory.GetUserInventoryContents(ctx, mock, userID, appID, contextID, false, "")
		require.NoError(t, err)
		assert.Equal(t, 0, total)
		assert.Empty(t, inv)
		assert.Empty(t, curr)
	})

	t.Run("Tradable Only Filter", func(t *testing.T) {
		mock := requester.New()
		resp := inventoryResponseMock{
			Success:    true,
			TotalCount: 2,
			Assets: []inventory.Asset{
				{AssetID: "1", ClassID: "1", InstanceID: "1"}, // Tradable
				{AssetID: "2", ClassID: "2", InstanceID: "2"}, // Non-Tradable
				{AssetID: "3", ClassID: "3", InstanceID: "3"}, // Missing Description
			},
			Descriptions: []inventory.Description{
				{ClassID: "1", InstanceID: "1", Tradable: 1},
				{ClassID: "2", InstanceID: "2", Tradable: 0},
			},
		}

		mock.OnRest = func(method, path string, body []byte) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       requester.NewBuffer(jsonResponse(resp)),
			}, nil
		}

		inv, _, _, err := inventory.GetUserInventoryContents(ctx, mock, userID, appID, contextID, true, "")
		require.NoError(t, err)
		assert.Len(t, inv, 1)
		assert.Equal(t, "1", inv[0].Asset.AssetID)
	})

	t.Run("Steam Error Response", func(t *testing.T) {
		mock := requester.New()
		resp := inventoryResponseMock{
			Success: false,
			Error:   "Rate limit exceeded",
		}

		mock.OnRest = func(method, path string, body []byte) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       requester.NewBuffer(jsonResponse(resp)),
			}, nil
		}

		_, _, _, err := inventory.GetUserInventoryContents(ctx, mock, userID, appID, contextID, false, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "steam error: Rate limit exceeded")
	})

	t.Run("Requester Error", func(t *testing.T) {
		mock := requester.New()
		mock.OnRest = func(method, path string, body []byte) (*http.Response, error) {
			return nil, errors.New("network fail")
		}

		_, _, _, err := inventory.GetUserInventoryContents(ctx, mock, userID, appID, contextID, false, "")
		require.Error(t, err)
		assert.EqualError(t, err, "network fail")
	})
}

// inventoryResponseMock matches the internal inventoryResponse struct
// to allow unmarshaling in tests if needed.
type inventoryResponseMock struct {
	Success      bool                    `json:"success"`
	Error        string                  `json:"error"`
	Assets       []inventory.Asset       `json:"assets"`
	Descriptions []inventory.Description `json:"descriptions"`
	MoreItems    bool                    `json:"more_items"`
	LastAssetID  string                  `json:"last_assetid"`
	TotalCount   int                     `json:"total_inventory_count"`
}
