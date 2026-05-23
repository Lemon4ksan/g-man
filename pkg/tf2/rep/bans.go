// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package rep provides utilities for checking user bans against various ban lists.
package rep

import (
	"context"
	"fmt"

	"github.com/lemon4ksan/g-man/pkg/rest"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/tf2/bptf"
)

// BansManager handles checking users against various ban lists.
type BansManager struct {
	bptfClient *bptf.Client
	mptfAPIKey string
	httpClient rest.HTTPDoer
}

// NewBansManager creates a new bans manager.
func NewBansManager(bptfClient *bptf.Client, mptfAPIKey string) *BansManager {
	return &BansManager{
		bptfClient: bptfClient,
		mptfAPIKey: mptfAPIKey,
		httpClient: bptfClient.REST().HTTP(),
	}
}

// BanResult contains the results of ban checks from different sources.
type BanResult struct {
	IsBanned bool
	Details  map[string]string // Source -> Status/Reason
}

// CheckBans checks a user against multiple ban lists (Backpack.tf, SteamRep, Marketplace.tf).
func (m *BansManager) CheckBans(ctx context.Context, steamID id.ID) (*BanResult, error) {
	result := &BanResult{
		Details: make(map[string]string),
	}

	// Check Backpack.tf (which also includes SteamRep info)
	userResp, err := m.bptfClient.GetUsersInfo(ctx, []id.ID{steamID})
	if err == nil {
		if user, ok := userResp.Users[steamID]; ok {
			if user.Bans != nil {
				if user.Bans.All != "" || user.Bans.BPTF != "" {
					result.IsBanned = true
					result.Details["backpack.tf"] = "banned"
				}

				if user.Bans.SteamRepScammer == 1 {
					result.IsBanned = true
					result.Details["steamrep.com"] = "scammer"
				}
			}

			if user.Trust.Negative > 0 && user.Trust.Negative > user.Trust.Positive {
				result.Details["trust"] = fmt.Sprintf("negative (%d/%d)", user.Trust.Negative, user.Trust.Positive)
			}
		}
	}

	// Check Marketplace.tf (requires API key)
	if m.mptfAPIKey != "" {
		mptfBanned, err := m.checkMarketplaceTF(ctx, steamID)
		if err == nil && mptfBanned {
			result.IsBanned = true
			result.Details["marketplace.tf"] = "banned"
		}
	}

	return result, nil
}

func (m *BansManager) checkMarketplaceTF(ctx context.Context, steamID id.ID) (bool, error) {
	url := "https://marketplace.tf/api/Bans/GetUserBan/v2"

	req := struct {
		Key     string `url:"key"`
		SteamID string `url:"steamid"`
	}{
		Key:     m.mptfAPIKey,
		SteamID: steamID.String(),
	}

	type MPTFResponse struct {
		Status  string `json:"status"` // "success"
		Results []struct {
			SteamID string `json:"steamid"`
			Banned  bool   `json:"banned"`
		} `json:"results"`
	}

	resp, err := rest.PostJSON[any, MPTFResponse](ctx, rest.NewClient(m.httpClient), url, req, nil)
	if err != nil {
		return false, err
	}

	if resp.Status != "success" {
		return false, fmt.Errorf("mptf returned error status: %s", resp.Status)
	}

	for _, res := range resp.Results {
		if res.SteamID == steamID.String() {
			return res.Banned, nil
		}
	}

	return false, nil
}
