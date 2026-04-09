// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package guard

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
)

var rxTradeOfferID = regexp.MustCompile(`id="tradeofferid_(\d+)"`)

// TwoFactorService covers ITwoFactorService methods.
type TwoFactorService struct {
	client service.Doer
}

func NewTwoFactorService(client service.Doer) *TwoFactorService {
	return &TwoFactorService{client: client}
}

// QueryTimeOffset calculates the drift between local computer time and Steam Server time.
// Crucial for generating valid TOTP codes if the local clock is out of sync.
func (s *TwoFactorService) QueryTimeOffset(ctx context.Context) (time.Duration, error) {
	type respStruct struct {
		ServerTime string `json:"server_time"`
	}
	resp, err := service.WebAPI[respStruct](ctx, s.client, "POST", "ITwoFactorService", "QueryTime", 1, nil)
	if err != nil {
		return 0, err
	}

	serverTime, err := strconv.ParseInt(resp.ServerTime, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid server time format from steam: %w", err)
	}

	diffSeconds := serverTime - time.Now().Unix()
	return time.Duration(diffSeconds) * time.Second, nil
}

// MobileConf provides access to Steam's mobile verification endpoints.
type MobileConf struct {
	client community.Requester
}

func NewMobileConf(client community.Requester) *MobileConf {
	return &MobileConf{client: client}
}

// GetConfirmations fetches the list of pending mobile confirmations.
func (s *MobileConf) GetConfirmations(ctx context.Context, deviceID string, steamID id.ID, confKey string, timestamp int64) (*ConfirmationsList, error) {
	params := baseParams{
		DeviceID:  deviceID,
		SteamID:   steamID,
		ConfKey:   confKey,
		Timestamp: timestamp,
		Mode:      "react",
		ActionTag: "conf",
	}
	return community.Get[ConfirmationsList](ctx, s.client, "mobileconf/getlist", params)
}

// GetConfirmationOfferID retrieves the trade offer ID associated with a market listing confirmation.
func (s *MobileConf) GetConfirmationOfferID(ctx context.Context, confID uint64, deviceID string, steamID id.ID, confKey string, timestamp int64) (uint64, error) {
	params := baseParams{
		DeviceID:  deviceID,
		SteamID:   steamID,
		ConfKey:   confKey,
		Timestamp: timestamp,
		Mode:      "react",
		ActionTag: "details",
	}

	path := "mobileconf/detailspage/" + strconv.FormatUint(confID, 10)

	respBytes, err := community.Get[[]byte](ctx, s.client, path, params, func(r *tr.Request, c *api.CallConfig) {
		c.Format = api.FormatRaw
	})
	if err != nil {
		return 0, err
	}

	matches := rxTradeOfferID.FindSubmatch(*respBytes)
	if len(matches) < 2 {
		return 0, fmt.Errorf("offer ID not found in confirmation details page")
	}

	return strconv.ParseUint(string(matches[1]), 10, 64)
}

// RespondToConfirmation accepts or denies a single confirmation.
func (s *MobileConf) RespondToConfirmation(ctx context.Context, conf *Confirmation, accept bool, deviceID string, steamID id.ID, confKey string, timestamp int64) error {
	op := "cancel"
	if accept {
		op = "allow"
	}

	params := baseParams{
		DeviceID:  deviceID,
		SteamID:   steamID,
		ConfKey:   confKey,
		Timestamp: timestamp,
		Mode:      "react",
		ActionTag: "conf",
		Op:        op,
		ConfID:    conf.ID,
		Nonce:     conf.Nonce,
	}

	type resStruct struct {
		Success bool `json:"success"`
	}

	resp, err := community.Get[resStruct](ctx, s.client, "mobileconf/ajaxop", params)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("steam rejected confirmation action")
	}

	return nil
}

// RespondToMultiple accepts or denies multiple confirmations in a single request.
func (s *MobileConf) RespondToMultiple(ctx context.Context, confs []*Confirmation, accept bool, deviceID string, steamID id.ID, confKey string, timestamp int64) error {
	if len(confs) == 0 {
		return nil
	}

	op := "cancel"
	if accept {
		op = "allow"
	}

	type multiRequest struct {
		baseParams
		ConfIDs []uint64 `url:"cid[]"`
		Nonces  []uint64 `url:"ck[]"`
	}

	req := multiRequest{
		baseParams: baseParams{
			DeviceID:  deviceID,
			SteamID:   steamID,
			ConfKey:   confKey,
			Timestamp: timestamp,
			Mode:      "react",
			ActionTag: "multiajaxop",
			Op:        op,
		},
		ConfIDs: make([]uint64, len(confs)),
		Nonces:  make([]uint64, len(confs)),
	}

	for i, c := range confs {
		req.ConfIDs[i] = c.ID
		req.Nonces[i] = c.Nonce
	}

	type resStruct struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}

	resp, err := community.PostForm[resStruct](ctx, s.client, "mobileconf/multiajaxop", req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("steam rejected multi-confirmation action: %s", resp.Message)
	}

	return nil
}

type baseParams struct {
	DeviceID  string `url:"p"`
	SteamID   id.ID  `url:"a"`
	ConfKey   string `url:"k"`
	Timestamp int64  `url:"t"`
	Mode      string `url:"m"`
	ActionTag string `url:"tag"`
	Op        string `url:"op,omitempty"`
	ConfID    uint64 `url:"cid,omitempty"`
	Nonce     uint64 `url:"ck,omitempty"`
}
