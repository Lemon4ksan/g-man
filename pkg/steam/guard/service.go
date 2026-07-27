// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package guard

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/miyako/generic"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/internal/bytesconv"
	"github.com/lemon4ksan/g-man/internal/crypto"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/community"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
)

var rxTradeOfferID = regexp.MustCompile(`id="tradeofferid_(\d+)"`)

var (
	// ErrOfferIDNotFound indicates trade offer ID was missing from confirmation HTML details.
	ErrOfferIDNotFound = errors.New("offer ID not found in confirmation details page")
	// ErrConfirmationRejected indicates Steam rejected mobile confirmation operation.
	ErrConfirmationRejected = errors.New("steam rejected confirmation action")
)

// TwoFactorService wraps ITwoFactorService WebAPI endpoints.
type TwoFactorService struct {
	client service.Doer
}

// NewTwoFactorService constructs a TwoFactorService wrapper.
func NewTwoFactorService(client service.Doer) *TwoFactorService {
	return &TwoFactorService{client: client}
}

// QueryTimeOffset computes time drift between local machine clock and Steam Server time.
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

	adjustedServerTime := time.Unix(serverTime, 0).Add(rtt / 2)

	return time.Until(adjustedServerTime), nil
}

// MobileConf executes mobile confirmation operations against mobileconf endpoints.
type MobileConf struct {
	client community.Requester
}

// NewMobileConf constructs a MobileConf instance.
func NewMobileConf(client community.Requester) *MobileConf {
	return &MobileConf{client: client}
}

// GetConfirmations fetches active mobile confirmations.
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

	return community.GetTo[ConfirmationsList](
		ctx, s.client, "mobileconf/getlist",
		mod.WithQuery(params),
	)
}

// GetConfirmationOfferID parses the trade offer ID associated with a confirmation details page.
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

	respBytes, err := community.GetTo[[]byte](
		ctx, s.client, path,
		mod.WithQuery(params),
		decode.WithRaw(),
	)
	if err != nil {
		return 0, err
	}

	matches := rxTradeOfferID.FindSubmatch(*respBytes)
	if len(matches) < 2 {
		return 0, ErrOfferIDNotFound
	}

	return strconv.ParseUint(string(matches[1]), 10, 64)
}

// RespondToConfirmation accepts or rejects a single confirmation.
func (s *MobileConf) RespondToConfirmation(
	ctx context.Context,
	conf *Confirmation,
	accept bool,
	deviceID string,
	steamID id.ID,
	confKey string,
	timestamp int64,
) error {
	params := baseParams{
		DeviceID:  deviceID,
		SteamID:   steamID,
		ConfKey:   confKey,
		Timestamp: timestamp,
		Mode:      "react",
		ActionTag: generic.Ternary(accept, "accept", "reject"),
		Op:        generic.Ternary(accept, "allow", "cancel"),
		ConfID:    conf.ID,
		Nonce:     conf.Nonce,
	}

	type respStruct struct {
		Success bool `json:"success"`
	}

	resp, err := community.GetTo[respStruct](
		ctx, s.client, "mobileconf/ajaxop",
		mod.WithQuery(params),
	)
	if err != nil {
		return err
	}

	if !resp.Success {
		return ErrConfirmationRejected
	}

	return nil
}

type multiRequest struct {
	baseParams
	ConfIDs []uint64 `url:"cid[]"`
	Nonces  []uint64 `url:"ck[]"`
}

func (r multiRequest) EncodeFormString() (string, error) {
	var sb strings.Builder
	sb.Grow(256 + len(r.ConfIDs)*24 + len(r.Nonces)*24)

	baseStr, err := r.baseParams.EncodeFormString()
	if err != nil {
		return "", err
	}

	sb.WriteString(baseStr)

	for _, cid := range r.ConfIDs {
		sb.WriteString("&cid[]=")
		sb.WriteString(strconv.FormatUint(cid, 10))
	}

	for _, nonce := range r.Nonces {
		sb.WriteString("&ck[]=")
		sb.WriteString(strconv.FormatUint(nonce, 10))
	}

	return sb.String(), nil
}

// RespondToMultiple accepts or rejects multiple confirmations in a single batch request.
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

	req := multiRequest{
		baseParams: baseParams{
			DeviceID:  deviceID,
			SteamID:   steamID,
			ConfKey:   confKey,
			Timestamp: timestamp,
			Mode:      "react",
			ActionTag: generic.Ternary(accept, "accept", "reject"),
			Op:        generic.Ternary(accept, "allow", "cancel"),
		},
		ConfIDs: make([]uint64, len(confs)),
		Nonces:  make([]uint64, len(confs)),
	}

	for i, c := range confs {
		req.ConfIDs[i] = c.ID
		req.Nonces[i] = c.Nonce
	}

	type respStruct struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}

	resp, err := community.PostFormTo[respStruct](ctx, s.client, "mobileconf/multiajaxop", req)
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

func (p baseParams) EncodeFormString() (string, error) {
	var sb strings.Builder
	sb.Grow(128)

	sb.WriteString("p=")
	sb.WriteString(url.QueryEscape(p.DeviceID))
	sb.WriteString("&a=")
	sb.WriteString(p.SteamID.String())
	sb.WriteString("&k=")
	sb.WriteString(url.QueryEscape(p.ConfKey))
	sb.WriteString("&t=")
	sb.WriteString(strconv.FormatInt(p.Timestamp, 10))
	sb.WriteString("&m=")
	sb.WriteString(p.Mode)
	sb.WriteString("&tag=")
	sb.WriteString(p.ActionTag)

	if p.Op != "" {
		sb.WriteString("&op=")
		sb.WriteString(p.Op)
	}

	if p.ConfID > 0 {
		sb.WriteString("&cid=")
		sb.WriteString(strconv.FormatUint(p.ConfID, 10))
	}

	if p.Nonce > 0 {
		sb.WriteString("&ck=")
		sb.WriteString(strconv.FormatUint(p.Nonce, 10))
	}

	return sb.String(), nil
}

// AddAuthenticator registers a new authenticator to the account.
func (s *TwoFactorService) AddAuthenticator(
	ctx context.Context,
	steamID id.ID,
	deviceID string,
) (*pb.CTwoFactor_AddAuthenticator_Response, error) {
	req := &pb.CTwoFactor_AddAuthenticator_Request{
		AuthenticatorType: proto.Uint32(1),
		Steamid:           proto.Uint64(steamID.Uint64()),
		DeviceIdentifier:  proto.String(deviceID),
		Version:           proto.Uint32(2),
	}

	return service.Unified[pb.CTwoFactor_AddAuthenticator_Response](ctx, s.client, req)
}

// FinalizeAuthenticator links the authenticator using SMS code and active TOTP code.
func (s *TwoFactorService) FinalizeAuthenticator(
	ctx context.Context,
	steamID id.ID,
	sharedSecret string,
	serverTime uint64,
	smsCode string,
) (*pb.CTwoFactor_FinalizeAddAuthenticator_Response, error) {
	totpCode := crypto.GenerateAuthCode(bytesconv.S2B(sharedSecret), int64(serverTime))

	req := &pb.CTwoFactor_FinalizeAddAuthenticator_Request{
		Steamid:           proto.Uint64(steamID.Uint64()),
		AuthenticatorCode: proto.String(bytesconv.B2S(totpCode[:])),
		AuthenticatorTime: proto.Uint64(serverTime),
		ActivationCode:    proto.String(smsCode),
		ValidateSmsCode:   proto.Bool(true),
	}

	return service.Unified[pb.CTwoFactor_FinalizeAddAuthenticator_Response](ctx, s.client, req)
}

// QueryStatus retrieves current two-factor settings status.
func (s *TwoFactorService) QueryStatus(
	ctx context.Context,
	steamID id.ID,
) (*pb.CTwoFactor_Status_Response, error) {
	req := &pb.CTwoFactor_Status_Request{
		Steamid: proto.Uint64(steamID.Uint64()),
	}

	return service.Unified[pb.CTwoFactor_Status_Response](ctx, s.client, req)
}

// RemoveAuthenticator revokes two-factor authenticator protection using a revocation code.
func (s *TwoFactorService) RemoveAuthenticator(
	ctx context.Context,
	revocationCode string,
) (*pb.CTwoFactor_RemoveAuthenticator_Response, error) {
	req := &pb.CTwoFactor_RemoveAuthenticator_Request{
		RevocationCode: proto.String(revocationCode),
	}

	return service.Unified[pb.CTwoFactor_RemoveAuthenticator_Response](ctx, s.client, req)
}

// RemoveAuthenticatorViaChallengeStart initiates authenticator transfer.
func (s *TwoFactorService) RemoveAuthenticatorViaChallengeStart(
	ctx context.Context,
) (*pb.CTwoFactor_RemoveAuthenticatorViaChallengeStart_Response, error) {
	req := &pb.CTwoFactor_RemoveAuthenticatorViaChallengeStart_Request{}

	return service.Unified[pb.CTwoFactor_RemoveAuthenticatorViaChallengeStart_Response](ctx, s.client, req)
}

// RemoveAuthenticatorViaChallengeContinue completes authenticator transfer using SMS code.
func (s *TwoFactorService) RemoveAuthenticatorViaChallengeContinue(
	ctx context.Context,
	steamID id.ID,
	smsCode string,
) (*pb.CTwoFactor_RemoveAuthenticatorViaChallengeContinue_Response, error) {
	req := &pb.CTwoFactor_RemoveAuthenticatorViaChallengeContinue_Request{
		SmsCode:          proto.String(smsCode),
		GenerateNewToken: proto.Bool(true),
		Version:          proto.Uint32(2),
	}

	return service.Unified[pb.CTwoFactor_RemoveAuthenticatorViaChallengeContinue_Response](ctx, s.client, req)
}

// IsFinalizeWantMore inspects unknown fields in CTwoFactor_FinalizeAddAuthenticator_Response to check if Steam expects additional authentication codes.
func IsFinalizeWantMore(resp *pb.CTwoFactor_FinalizeAddAuthenticator_Response) bool {
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
