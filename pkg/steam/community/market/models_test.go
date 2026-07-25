// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package market

import (
	"fmt"
	"testing"
	"time"

	json "github.com/goccy/go-json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAsset_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	jsonData := `{
		"appid": 730,
		"contextid": "2",
		"id": "123456789",
		"amount": "1",
		"tradable": 1,
		"commodity": 0,
		"marketable": "1",
		"name": "AK-47 | Redline"
	}`

	var asset Asset

	err := json.Unmarshal([]byte(jsonData), &asset)
	require.NoError(t, err)

	assert.Equal(t, 730, asset.AppID)
	assert.Equal(t, int64(2), int64(asset.ContextID))
	assert.Equal(t, uint64(123456789), uint64(asset.ID))
	assert.True(t, bool(asset.Tradable))
	assert.False(t, bool(asset.Commodity))
	assert.True(t, bool(asset.Marketable))
	assert.Equal(t, "AK-47 | Redline", asset.Name)
}

func TestGraphPoints_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("valid_data", func(t *testing.T) {
		t.Parallel()

		data := []byte(`[[10.5, 5, "5 sold at $10.50"], [11.0, 2, "2 sold at $11.00"]]`)

		var gp GraphPoints

		err := json.Unmarshal(data, &gp)
		require.NoError(t, err)
		require.Len(t, gp, 2)

		assert.Equal(t, 10.5, gp[0].Price)
		assert.Equal(t, int64(5), gp[0].Volume)
		assert.Equal(t, "5 sold at $10.50", gp[0].Description)
	})

	t.Run("invalid_nested_length", func(t *testing.T) {
		t.Parallel()

		data := []byte(`[[10.5, 5, "desc"], [11.0, 2]]`)

		var gp GraphPoints

		err := json.Unmarshal(data, &gp)
		require.NoError(t, err)
		assert.Len(t, gp, 2)
		assert.Equal(t, 0.0, gp[1].Price)
	})

	t.Run("malformed_json", func(t *testing.T) {
		t.Parallel()

		var gp GraphPoints

		err := json.Unmarshal([]byte(`{ "not": "an array" }`), &gp)
		assert.Error(t, err)
	})
}

func TestPriceSample_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("valid_steam_format", func(t *testing.T) {
		t.Parallel()

		timePart := "Jan 02 2026 15:04:05 GMT+0000"
		padding := " +0000"
		input := timePart + padding

		data := fmt.Appendf(nil, `["%s", 12.34, "500"]`, input)

		var ps PriceSample

		err := json.Unmarshal(data, &ps)
		require.NoError(t, err)

		assert.Equal(t, 12.34, ps.Price)
		assert.Equal(t, int64(500), ps.Volume)

		expected := time.Date(2026, time.January, 2, 15, 4, 5, 0, time.UTC)
		assert.True(t, ps.Timestamp.Equal(expected))
	})

	t.Run("unmarshal_errors", func(t *testing.T) {
		t.Parallel()

		const validTimeStr = "Jan 02 2026 15:04:05 GMT+0000 +0000"

		tests := []struct {
			name string
			data []byte
		}{
			{
				name: "array_unmarshal_error",
				data: []byte(`{"not": "an array"}`),
			},
			{
				name: "timestamp_type_error",
				data: []byte(`[123, 1.0, "1"]`),
			},
			{
				name: "price_type_error",
				data: fmt.Appendf(nil, `["%s", "not-a-float", "1"]`, validTimeStr),
			},
			{
				name: "volume_type_error",
				data: fmt.Appendf(nil, `["%s", 1.0, 123]`, validTimeStr),
			},
			{
				name: "invalid_time_format",
				data: fmt.Appendf(nil, `["%s", 1.0, "1"]`, "Invalid Date String Length 35!!!!"),
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				var ps PriceSample

				err := json.Unmarshal(tt.data, &ps)
				assert.Error(t, err)
			})
		}
	})
}

func TestBuyOrderResponse_Unmarshal(t *testing.T) {
	t.Parallel()

	data := []byte(`{"success": true, "buy_orderid": "987654321"}`)

	var resp CreateBuyOrderResponse

	err := json.Unmarshal(data, &resp)
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, uint64(987654321), resp.BuyOrderID)
}

func TestItemOrdersHistogramResponse_Unmarshal(t *testing.T) {
	t.Parallel()

	data := []byte(`{
		"success": 1,
		"sell_order_graph": [[100.0, 1, "1 unit"]]
	}`)

	var resp ItemOrdersHistogramResponse

	err := json.Unmarshal(data, &resp)
	require.NoError(t, err)
	assert.Equal(t, 1, resp.Success)
	require.Len(t, resp.SellOrderGraph, 1)
	assert.Equal(t, 100.0, resp.SellOrderGraph[0].Price)
}
