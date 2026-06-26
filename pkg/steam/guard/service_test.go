// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package guard

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/test/mock"
)

func TestTwoFactorService_QueryTimeOffset(t *testing.T) {
	ctx := context.Background()
	mock := mock.NewServiceMock()
	svc := NewTwoFactorService(mock)

	t.Run("Success", func(t *testing.T) {
		// Steam returns time as a string in this specific API
		serverTime := time.Now().Add(30 * time.Second).Unix()

		mock.SetJSONResponse("ITwoFactorService", "QueryTime", map[string]any{
			"response": map[string]string{
				"server_time": strconv.FormatInt(serverTime, 10),
			},
		})

		offset, err := svc.QueryTimeOffset(ctx)
		assert.NoError(t, err)
		// Expect roughly 30s offset
		assert.InDelta(t, 30*time.Second, offset, float64(2*time.Second))
	})

	t.Run("Invalid Format", func(t *testing.T) {
		mock.SetJSONResponse("ITwoFactorService", "QueryTime", map[string]any{
			"response": map[string]string{
				"server_time": "not-a-number",
			},
		})

		_, err := svc.QueryTimeOffset(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid server time format")
	})

	t.Run("Network Error", func(t *testing.T) {
		mock.ResponseErrs["ITwoFactorService.QueryTime"] = errors.New("timeout")

		_, err := svc.QueryTimeOffset(ctx)
		assert.Error(t, err)
	})
}

func TestMobileConf_GetConfirmations(t *testing.T) {
	ctx := context.Background()
	mockComm := mock.NewHTTPStub()
	svc := NewMobileConf(mockComm)
	sid := id.ID(76561198000000001)

	t.Run("Success", func(t *testing.T) {
		expected := &ConfirmationsList{
			Success: true,
			Confirmations: []*Confirmation{
				{ID: 123, Title: "Trade with Bot"},
			},
		}
		mockComm.SetJSONResponse("mobileconf/getlist", 200, expected)

		resp, err := svc.GetConfirmations(ctx, "android:1", sid, "key", 12345)
		assert.NoError(t, err)
		assert.True(t, resp.Success)
		assert.Len(t, resp.Confirmations, 1)
		assert.Equal(t, uint64(123), resp.Confirmations[0].ID)
	})
}

func TestMobileConf_GetConfirmationOfferID(t *testing.T) {
	ctx := context.Background()
	mockComm := mock.NewHTTPStub()
	svc := NewMobileConf(mockComm)
	sid := id.ID(76561198000000001)

	t.Run("Found", func(t *testing.T) {
		html := `<div>Some content <div id="tradeofferid_987654321"></div></div>`
		mockComm.SetRawResponse("mobileconf/detailspage/123", 200, []byte(html))

		offerID, err := svc.GetConfirmationOfferID(ctx, 123, "dev", sid, "key", 0)
		assert.NoError(t, err)
		assert.Equal(t, uint64(987654321), offerID)
	})

	t.Run("Not Found", func(t *testing.T) {
		mockComm.SetRawResponse("mobileconf/detailspage/123", 200, []byte("no offer here"))

		_, err := svc.GetConfirmationOfferID(ctx, 123, "dev", sid, "key", 0)
		assert.Error(t, err)
		assert.Equal(t, "offer ID not found in confirmation details page", err.Error())
	})
}

func TestMobileConf_RespondToConfirmation(t *testing.T) {
	ctx := context.Background()
	mockComm := mock.NewHTTPStub()
	svc := NewMobileConf(mockComm)
	sid := id.ID(76561198000000001)
	conf := &Confirmation{ID: 111, Nonce: 222}

	t.Run("Accept Success", func(t *testing.T) {
		mockComm.SetJSONResponse("mobileconf/ajaxop", 200, map[string]bool{"success": true})

		err := svc.RespondToConfirmation(ctx, conf, true, "dev", sid, "key", 0)
		assert.NoError(t, err)

		// Verify parameters in last call
		params := mockComm.GetLastCallParams()
		assert.Equal(t, "allow", params.Get("op"))
		assert.Equal(t, "111", params.Get("cid"))
	})

	t.Run("Steam Rejection", func(t *testing.T) {
		mockComm.SetJSONResponse("mobileconf/ajaxop", 400, map[string]bool{"success": false})

		err := svc.RespondToConfirmation(ctx, conf, false, "dev", sid, "key", 0)
		assert.Error(t, err)
		assert.Equal(t, "steam rejected confirmation action", err.Error())
	})
}

func TestMobileConf_RespondToMultiple(t *testing.T) {
	ctx := context.Background()
	mockComm := mock.NewHTTPStub()
	svc := NewMobileConf(mockComm)
	sid := id.ID(76561198000000001)

	confs := []*Confirmation{
		{ID: 1, Nonce: 10},
		{ID: 2, Nonce: 20},
	}

	t.Run("Empty List", func(t *testing.T) {
		err := svc.RespondToMultiple(ctx, nil, true, "dev", sid, "key", 0)
		assert.NoError(t, err)
		assert.Empty(t, mockComm.Calls)
	})

	t.Run("Success", func(t *testing.T) {
		mockComm.SetJSONResponse("mobileconf/multiajaxop", 200, map[string]any{
			"success": true,
		})

		err := svc.RespondToMultiple(ctx, confs, true, "dev", sid, "key", 0)
		assert.NoError(t, err)

		params := mockComm.GetLastCallParams()
		assert.Equal(t, "allow", params.Get("op"))
		// Verify array params (cid[] and ck[])
		assert.ElementsMatch(t, []string{"1", "2"}, params["cid[]"])
		assert.ElementsMatch(t, []string{"10", "20"}, params["ck[]"])
	})

	t.Run("Failure with Message", func(t *testing.T) {
		mockComm.SetJSONResponse("mobileconf/multiajaxop", 200, map[string]any{
			"success": false,
			"message": "failed manually",
		})

		err := svc.RespondToMultiple(ctx, confs, false, "dev", sid, "key", 0)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed manually")
	})
}
