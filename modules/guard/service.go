// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package guard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/lemon4ksan/g-man/steam/api"
	tr "github.com/lemon4ksan/g-man/steam/transport"
)

var rxTradeOfferID = regexp.MustCompile(`id="tradeofferid_(\d+)"`)

// MobileConf provides access to Steam's mobile verification endpoints.
type MobileConf struct {
	client *api.CommunityClient
}

func NewMobileConf(client *api.CommunityClient) *MobileConf {
	return &MobileConf{client: client}
}

func (s *MobileConf) GetConfirmations(ctx context.Context, deviceID string, steamID uint64, confKey string, timestamp int64) (*ConfirmationsList, error) {
	params := s.buildBaseParams(deviceID, steamID, confKey, timestamp, "conf")

	res := &ConfirmationsList{}
	err := s.client.GetJSON(ctx, "mobileconf/getlist", params, &res)
	return res, err
}

func (s *MobileConf) GetConfirmationOfferID(ctx context.Context, confID uint64, deviceID string, steamID uint64, confKey string, timestamp int64) (uint64, error) {
	params := s.buildBaseParams(deviceID, steamID, confKey, timestamp, "details")

	// Pass parameters using the RequestModifier pattern
	resp, err := s.client.Get(ctx, "mobileconf/detailspage/"+strconv.FormatUint(confID, 10), func(r *tr.Request) {
		r.WithParams(params)
	})
	if err != nil {
		return 0, err
	}

	matches := rxTradeOfferID.FindStringSubmatch(string(resp.Body))
	if len(matches) < 2 {
		return 0, fmt.Errorf("offer ID not found in confirmation details")
	}

	return strconv.ParseUint(matches[1], 10, 64)
}

func (s *MobileConf) RespondToConfirmation(ctx context.Context, conf *Confirmation, accept bool, deviceID string, steamID uint64, confKey string, timestamp int64) error {
	tag := "allow"
	if !accept {
		tag = "cancel"
	}

	params := s.buildBaseParams(deviceID, steamID, confKey, timestamp, tag)
	params.Set("op", tag)
	params.Set("cid", strconv.FormatUint(conf.ID, 10))
	params.Set("ck", strconv.FormatUint(conf.Nonce, 10))

	return s.executeAction(ctx, false, "mobileconf/ajaxop", params)
}

func (s *MobileConf) RespondToMultiple(ctx context.Context, confs []*Confirmation, accept bool, deviceID string, steamID uint64, confKey string, timestamp int64) error {
	if len(confs) == 0 {
		return nil
	}

	tag := "allow"
	if !accept {
		tag = "cancel"
	}

	params := s.buildBaseParams(deviceID, steamID, confKey, timestamp, tag)
	params.Set("op", tag)

	for _, conf := range confs {
		params.Add("cid[]", strconv.FormatUint(conf.ID, 10))
		params.Add("ck[]", strconv.FormatUint(conf.Nonce, 10))
	}

	return s.executeAction(ctx, true, "mobileconf/multiajaxop", params)
}

// executeAction dynamically fires GET or POST requests with parameters.
func (s *MobileConf) executeAction(ctx context.Context, isPost bool, path string, data url.Values) error {
	var resp *tr.Response
	var err error

	if isPost {
		resp, err = s.client.PostForm(ctx, path, data)
	} else {
		// FIXED: Added RequestModifier to inject the URL parameters into the GET request
		resp, err = s.client.Get(ctx, path, func(r *tr.Request) {
			r.WithParams(data)
		})
	}

	if err != nil {
		return err
	}

	var res struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err = json.Unmarshal(resp.Body, &res); err != nil {
		return fmt.Errorf("failed to parse steam response: %w", err)
	}

	if !res.Success {
		return fmt.Errorf("steam rejected action: %s", res.Message)
	}

	return nil
}

func (s *MobileConf) buildBaseParams(deviceID string, steamID uint64, confKey string, timestamp int64, tag string) url.Values {
	params := url.Values{}
	params.Set("p", deviceID)
	params.Set("a", strconv.FormatUint(steamID, 10))
	params.Set("k", confKey)
	params.Set("t", strconv.FormatInt(timestamp, 10))
	params.Set("m", "react")
	params.Set("tag", tag)
	return params
}

// TwoFactorService covers ITwoFactorService methods.
type TwoFactorService struct {
	client api.WebAPIRequester
}

func NewTwoFactorService(ur api.WebAPIRequester) *TwoFactorService {
	return &TwoFactorService{client: ur}
}

// QueryTimeOffset calculates the drift between local computer time and Steam Server time.
// Crucial for generating valid TOTP codes if the local clock is out of sync.
func (s *TwoFactorService) QueryTimeOffset(ctx context.Context) (time.Duration, error) {
	var resp struct {
		Response struct {
			ServerTime string `json:"server_time"`
		} `json:"response"`
	}

	err := s.client.CallWebAPI(ctx, "ITwoFactorService", "QueryTime", 1, &resp)
	if err != nil {
		return 0, err
	}

	serverTime, err := strconv.ParseInt(resp.Response.ServerTime, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid server time: %w", err)
	}

	diffSeconds := serverTime - time.Now().Unix()
	return time.Duration(diffSeconds) * time.Second, nil
}
