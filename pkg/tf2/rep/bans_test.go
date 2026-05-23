// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rep

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/tf2/bptf"
)

type mockDoer struct {
	fn func(req *http.Request) (*http.Response, error)
}

func (m *mockDoer) Do(req *http.Request) (*http.Response, error) {
	return m.fn(req)
}

func TestBansManager_CheckBans(t *testing.T) {
	cleanSteamID := id.New(76561198033830321)
	bannedSteamID := id.New(76561198000000001)
	scammerSteamID := id.New(76561198000000002)
	badTrustSteamID := id.New(76561198000000003)
	mptfBannedSteamID := id.New(76561198000000004)

	httpMock := &mockDoer{
		fn: func(req *http.Request) (*http.Response, error) {
			// 1. Intercept Backpack.tf User Info call
			if strings.HasSuffix(req.URL.Path, "/users/info/v1") {
				steamidsQuery := req.URL.Query().Get("steamids")

				resp := bptf.V1UserResponse{
					Users: make(map[id.ID]bptf.V1User),
				}

				// Populate mock info based on which steam ID is queried
				switch {
				case steamidsQuery == cleanSteamID.String():
					resp.Users[cleanSteamID] = bptf.V1User{
						Name: "Clean User",
						Trust: bptf.UserTrust{
							Positive: 10,
							Negative: 0,
						},
					}

				case steamidsQuery == bannedSteamID.String():
					resp.Users[bannedSteamID] = bptf.V1User{
						Name: "Banned User",
						Bans: &bptf.UserBans{
							All:  "banned everywhere",
							BPTF: "banned on bptf",
						},
					}

				case steamidsQuery == scammerSteamID.String():
					resp.Users[scammerSteamID] = bptf.V1User{
						Name: "SteamRep Scammer",
						Bans: &bptf.UserBans{
							SteamRepScammer: 1,
						},
					}

				case steamidsQuery == badTrustSteamID.String():
					resp.Users[badTrustSteamID] = bptf.V1User{
						Name: "Negative Trust User",
						Trust: bptf.UserTrust{
							Positive: 1,
							Negative: 5,
						},
					}

				case steamidsQuery == mptfBannedSteamID.String():
					resp.Users[mptfBannedSteamID] = bptf.V1User{
						Name: "Marketplace Banned User",
						Trust: bptf.UserTrust{
							Positive: 2,
							Negative: 0,
						},
					}
				}

				bodyBytes, _ := json.Marshal(resp)

				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewReader(bodyBytes)),
				}, nil
			}

			// 2. Intercept Marketplace.tf call
			if strings.HasSuffix(req.URL.Path, "/api/Bans/GetUserBan/v2") {
				// We need to read the body because PostJSON posts as JSON or URL encoded body.
				// But wait, PostJSON posts a struct.
				// Let's return isBanned = true only if it's the mptfBannedSteamID.
				isBanned := false

				// Read body or URL parameters.
				// PostJSON actually sends the payload in body.
				bodyBytes, _ := io.ReadAll(req.Body)
				bodyStr := string(bodyBytes)

				if bytes.Contains(bodyBytes, []byte(mptfBannedSteamID.String())) ||
					req.URL.Query().Get("steamid") == mptfBannedSteamID.String() ||
					bodyStr != "" {
					// We'll mock it dynamically
					if bytes.Contains(bodyBytes, []byte(mptfBannedSteamID.String())) {
						isBanned = true
					}
				}

				resp := struct {
					Status  string `json:"status"`
					Results []struct {
						SteamID string `json:"steamid"`
						Banned  bool   `json:"banned"`
					} `json:"results"`
				}{
					Status: "success",
					Results: []struct {
						SteamID string `json:"steamid"`
						Banned  bool   `json:"banned"`
					}{
						{
							SteamID: mptfBannedSteamID.String(),
							Banned:  isBanned,
						},
					},
				}

				respBytes, _ := json.Marshal(resp)

				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewReader(respBytes)),
				}, nil
			}

			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{"status":"error"}`))),
			}, nil
		},
	}

	bptfClient := bptf.New(httpMock, "mock-api-key", "mock-token")
	// Since bptf Client constructor sets the URL to https://backpack.tf/api, we should keep it
	// and our mock doer matches the path.

	t.Run("Clean User", func(t *testing.T) {
		manager := NewBansManager(bptfClient, "mock-mptf-key")
		res, err := manager.CheckBans(context.Background(), cleanSteamID)
		require.NoError(t, err)
		assert.False(t, res.IsBanned)
		assert.Len(t, res.Details, 0)
	})

	t.Run("Banned on Backpack.tf", func(t *testing.T) {
		manager := NewBansManager(bptfClient, "")
		res, err := manager.CheckBans(context.Background(), bannedSteamID)
		require.NoError(t, err)
		assert.True(t, res.IsBanned)
		assert.Equal(t, "banned", res.Details["backpack.tf"])
	})

	t.Run("SteamRep Scammer", func(t *testing.T) {
		manager := NewBansManager(bptfClient, "")
		res, err := manager.CheckBans(context.Background(), scammerSteamID)
		require.NoError(t, err)
		assert.True(t, res.IsBanned)
		assert.Equal(t, "scammer", res.Details["steamrep.com"])
	})

	t.Run("Negative Trust User", func(t *testing.T) {
		manager := NewBansManager(bptfClient, "")
		res, err := manager.CheckBans(context.Background(), badTrustSteamID)
		require.NoError(t, err)
		assert.False(t, res.IsBanned) // trust doesn't auto-ban, but logs details
		assert.Contains(t, res.Details["trust"], "negative")
	})

	t.Run("Marketplace.tf Banned", func(t *testing.T) {
		manager := NewBansManager(bptfClient, "mock-mptf-key")
		res, err := manager.CheckBans(context.Background(), mptfBannedSteamID)
		require.NoError(t, err)
		assert.True(t, res.IsBanned)
		assert.Equal(t, "banned", res.Details["marketplace.tf"])
	})

	t.Run("Marketplace.tf Checked without API key", func(t *testing.T) {
		// When API key is empty, Marketplace check is skipped
		manager := NewBansManager(bptfClient, "")
		res, err := manager.CheckBans(context.Background(), mptfBannedSteamID)
		require.NoError(t, err)
		assert.False(t, res.IsBanned)
		assert.NotContains(t, res.Details, "marketplace.tf")
	})
}
