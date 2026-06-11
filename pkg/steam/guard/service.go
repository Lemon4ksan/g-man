// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package guard

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/crypto"
	pbSteam "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
)

var rxTradeOfferID = regexp.MustCompile(`id="tradeofferid_(\d+)"`)

// TwoFactorService covers ITwoFactorService methods.
//
// Create new instances of the service using the [NewTwoFactorService] constructor.
type TwoFactorService struct {
	client service.Doer
}

// NewTwoFactorService creates a new wrapper around unified client.
func NewTwoFactorService(client service.Doer) *TwoFactorService {
	return &TwoFactorService{client: client}
}

// QueryTimeOffset calculates the drift between local computer time and Steam Server time.
// Crucial for generating valid TOTP codes if the local clock is out of sync.
func (s *TwoFactorService) QueryTimeOffset(ctx context.Context) (time.Duration, error) {
	type respStruct struct {
		ServerTime string `json:"server_time"`
	}

	start := time.Now()

	resp, err := service.WebAPI[respStruct](ctx, s.client, "POST", "ITwoFactorService", "QueryTime", 1, nil)
	if err != nil {
		return 0, err
	}

	rtt := time.Since(start)

	serverTime, err := strconv.ParseInt(resp.ServerTime, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid server time format from steam: %w", err)
	}

	// Adjust server time by adding half of the Round-Trip Time (RTT)
	adjustedServerTime := time.Unix(serverTime, 0).Add(rtt / 2)
	diff := time.Until(adjustedServerTime)

	return diff, nil
}

// MobileConf provides access to Steam's mobile verification endpoints.
//
// Create new instances of the service using the [NewMobileConf] constructor.
type MobileConf struct {
	client community.Requester
}

// NewMobileConf creates a new wrapper around unified client for responding to mobile confirmations.
func NewMobileConf(client community.Requester) *MobileConf {
	return &MobileConf{client: client}
}

// GetConfirmations fetches the list of pending mobile confirmations.
func (s *MobileConf) GetConfirmations(
	ctx context.Context,
	deviceID string,
	steamID id.ID,
	confKey string,
	timestamp int64,
) (*ConfirmationsList, error) {
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
func (s *MobileConf) GetConfirmationOfferID(
	ctx context.Context,
	confID uint64,
	deviceID string,
	steamID id.ID,
	confKey string,
	timestamp int64,
) (uint64, error) {
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
		return 0, errors.New("offer ID not found in confirmation details page")
	}

	return strconv.ParseUint(string(matches[1]), 10, 64)
}

// RespondToConfirmation accepts or denies a single confirmation.
func (s *MobileConf) RespondToConfirmation(
	ctx context.Context,
	conf *Confirmation,
	accept bool,
	deviceID string,
	steamID id.ID,
	confKey string,
	timestamp int64,
) error {
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
		return errors.New("steam rejected confirmation action")
	}

	return nil
}

// RespondToMultiple accepts or denies multiple confirmations in a single request.
func (s *MobileConf) RespondToMultiple(
	ctx context.Context,
	confs []*Confirmation,
	accept bool,
	deviceID string,
	steamID id.ID,
	confKey string,
	timestamp int64,
) error {
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

// AddAuthenticator registers a new authenticator to the account.
func (s *TwoFactorService) AddAuthenticator(
	ctx context.Context,
	steamID id.ID,
	deviceID string,
) (*pbSteam.CTwoFactor_AddAuthenticator_Response, error) {
	req := &pbSteam.CTwoFactor_AddAuthenticator_Request{
		AuthenticatorType: proto.Uint32(1),
		Steamid:           proto.Uint64(steamID.Uint64()),
		DeviceIdentifier:  proto.String(deviceID),
		Version:           proto.Uint32(2),
	}

	return service.Unified[pbSteam.CTwoFactor_AddAuthenticator_Response](ctx, s.client, req)
}

// FinalizeAuthenticator finalizes linking the authenticator using the verification SMS/email code and generated TOTP.
func (s *TwoFactorService) FinalizeAuthenticator(
	ctx context.Context,
	steamID id.ID,
	sharedSecret string,
	serverTime uint64,
	smsCode string,
) (*pbSteam.CTwoFactor_FinalizeAddAuthenticator_Response, error) {
	totpCode, err := crypto.GenerateAuthCode(sharedSecret, int64(serverTime))
	if err != nil {
		return nil, fmt.Errorf("failed to generate verification totp code: %w", err)
	}

	req := &pbSteam.CTwoFactor_FinalizeAddAuthenticator_Request{
		Steamid:           proto.Uint64(steamID.Uint64()),
		AuthenticatorCode: proto.String(totpCode),
		AuthenticatorTime: proto.Uint64(serverTime),
		ActivationCode:    proto.String(smsCode),
		ValidateSmsCode:   proto.Bool(true),
	}

	return service.Unified[pbSteam.CTwoFactor_FinalizeAddAuthenticator_Response](ctx, s.client, req)
}

// QueryStatus queries the current two-factor status for the account.
func (s *TwoFactorService) QueryStatus(
	ctx context.Context,
	steamID id.ID,
) (*pbSteam.CTwoFactor_Status_Response, error) {
	req := &pbSteam.CTwoFactor_Status_Request{
		Steamid: proto.Uint64(steamID.Uint64()),
	}

	return service.Unified[pbSteam.CTwoFactor_Status_Response](ctx, s.client, req)
}

// RemoveAuthenticator removes/revokes the authenticator using a revocation code.
func (s *TwoFactorService) RemoveAuthenticator(
	ctx context.Context,
	revocationCode string,
) (*pbSteam.CTwoFactor_RemoveAuthenticator_Response, error) {
	req := &pbSteam.CTwoFactor_RemoveAuthenticator_Request{
		RevocationCode: proto.String(revocationCode),
	}

	return service.Unified[pbSteam.CTwoFactor_RemoveAuthenticator_Response](ctx, s.client, req)
}

// RemoveAuthenticatorViaChallengeStart begins the authenticator transfer.
func (s *TwoFactorService) RemoveAuthenticatorViaChallengeStart(
	ctx context.Context,
) (*pbSteam.CTwoFactor_RemoveAuthenticatorViaChallengeStart_Response, error) {
	req := &pbSteam.CTwoFactor_RemoveAuthenticatorViaChallengeStart_Request{}

	return service.Unified[pbSteam.CTwoFactor_RemoveAuthenticatorViaChallengeStart_Response](ctx, s.client, req)
}

// RemoveAuthenticatorViaChallengeContinue finishes the authenticator transfer using the SMS code.
func (s *TwoFactorService) RemoveAuthenticatorViaChallengeContinue(
	ctx context.Context,
	steamID id.ID,
	smsCode string,
) (*pbSteam.CTwoFactor_RemoveAuthenticatorViaChallengeContinue_Response, error) {
	req := &pbSteam.CTwoFactor_RemoveAuthenticatorViaChallengeContinue_Request{
		SmsCode:          proto.String(smsCode),
		GenerateNewToken: proto.Bool(true),
		Version:          proto.Uint32(2),
	}

	return service.Unified[pbSteam.CTwoFactor_RemoveAuthenticatorViaChallengeContinue_Response](ctx, s.client, req)
}

// IsFinalizeWantMore parses the unknown fields of CTwoFactor_FinalizeAddAuthenticator_Response
// to determine if Steam expects more authentication codes.
func IsFinalizeWantMore(resp *pbSteam.CTwoFactor_FinalizeAddAuthenticator_Response) bool {
	if resp == nil {
		return false
	}

	unknown := resp.ProtoReflect().GetUnknown()
	for len(unknown) > 0 {
		num, typ, length := protowire.ConsumeTag(unknown)
		if num == 2 && typ == protowire.VarintType {
			val, _ := protowire.ConsumeVarint(unknown[length:])
			return val != 0
		}

		n := protowire.ConsumeFieldValue(num, typ, unknown[length:])
		if n < 0 {
			break
		}

		unknown = unknown[length+n:]
	}

	return false
}
