// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"time"

	"github.com/lemon4ksan/g-man/pkg/crypto"
	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"

	"google.golang.org/protobuf/proto"
)

// handleChannelEncryptRequest processes the initial TCP handshake from Steam CM.
// It generates a symmetric session key and sends it back encrypted with Steam's public key.
func (a *Authenticator) handleChannelEncryptRequest(packet *protocol.Packet) {
	a.logger.Debug("Received ChannelEncryptRequest", log.Int("size", len(packet.Payload)))

	r := bytes.NewReader(packet.Payload)
	var protocolVer, universe uint32
	if err := binary.Read(r, binary.LittleEndian, &protocolVer); err != nil {
		a.failLogin(fmt.Errorf("encrypt_request: failed to read protocol version: %w", err))
		return
	}
	if err := binary.Read(r, binary.LittleEndian, &universe); err != nil {
		a.failLogin(fmt.Errorf("encrypt_request: failed to read universe: %w", err))
		return
	}

	nonce := make([]byte, 16)
	if _, err := io.ReadFull(r, nonce); err != nil {
		a.failLogin(fmt.Errorf("encrypt_request: failed to read nonce: %w", err))
		return
	}

	// Generate symmetric key for this session
	plainKey, encryptedKey, err := crypto.GenerateSessionKey(nonce)
	if err != nil {
		a.failLogin(fmt.Errorf("encrypt_request: failed to generate session key: %w", err))
		return
	}

	// Store temporarily until CM confirms it
	a.tempKey.Store(&plainKey)

	// Structure: [ProtocolVersion] [KeySize] [EncryptedKey] [CRC32] [Trailer(0)]
	resp := new(bytes.Buffer)
	binary.Write(resp, binary.LittleEndian, protocolVer)
	binary.Write(resp, binary.LittleEndian, uint32(len(encryptedKey)))
	resp.Write(encryptedKey)
	binary.Write(resp, binary.LittleEndian, crc32.ChecksumIEEE(encryptedKey))
	binary.Write(resp, binary.LittleEndian, uint32(0))

	a.logger.Debug("Sending ChannelEncryptResponse", log.Int("key_size", len(encryptedKey)))

	// This is a network-level response, independent of the user's LogOn context.
	if err := a.socket.SendRaw(context.Background(), protocol.EMsg_ChannelEncryptResponse, resp.Bytes()); err != nil {
		a.failLogin(fmt.Errorf("encrypt_request: failed to send response: %w", err))
	}
}

// handleChannelEncryptResult confirms the secure channel is established.
// If successful, it triggers the token exchange and sends ClientLogon.
func (a *Authenticator) handleChannelEncryptResult(packet *protocol.Packet) {
	r := bytes.NewReader(packet.Payload)
	var result uint32
	if err := binary.Read(r, binary.LittleEndian, &result); err != nil {
		a.failLogin(fmt.Errorf("encrypt_result: failed to read result code: %w", err))
		return
	}

	if eresult := protocol.EResult(result); eresult != protocol.EResult_OK {
		a.failLogin(fmt.Errorf("encryption failed with EResult: %s", eresult))
		return
	}

	// Atomically swap the temp key to prevent reuse/race conditions
	keyPtr := a.tempKey.Swap(nil)
	if keyPtr == nil || *keyPtr == nil {
		a.failLogin(errors.New("encrypt_result: no temporary session key found to activate"))
		return
	}

	a.socket.Session().SetEncryptionKey(*keyPtr)
	a.logger.Info("TCP Encryption established")

	// Get the context and details for the current login attempt
	loginCtx := a.loginCtx.Load().(context.Context)
	details := a.activeDetails.Load()

	if loginCtx == nil || details == nil {
		a.failLogin(errors.New("encrypt_result: login context or details are missing"))
		return
	}

	// Proceed to send logon credentials over the encrypted channel
	a.sendLogOn(loginCtx, details)
}

// handleLogOnResponse handles the final authentication verdict from Steam.
func (a *Authenticator) handleLogOnResponse(packet *protocol.Packet) {
	msg := &pb.CMsgClientLogonResponse{}
	if err := proto.Unmarshal(packet.Payload, msg); err != nil {
		a.failLogin(fmt.Errorf("logon_response: unmarshal failed: %w", err))
		return
	}

	eresult := protocol.EResult(msg.GetEresult())
	if eresult != protocol.EResult_OK {
		a.logger.Error("Logon denied by CM", log.String("eresult", eresult.String()))

		a.failLogin(fmt.Errorf("steam logon denied: %w", api.EResultError{EResult: eresult}))

		a.socket.Bus().Publish(&LoggedOffEvent{Result: eresult})
		return
	}

	// Update session identifiers
	if ah, ok := packet.Header.(protocol.AuthorizedHeader); ok {
		sess := a.socket.Session()
		if steamID := ah.GetSteamID(); steamID != 0 {
			sess.SetSteamID(steamID)
		}
		if sessionID := ah.GetSessionID(); sessionID != 0 {
			sess.SetSessionID(sessionID)
		}
	}

	// Start Heartbeat
	interval := time.Duration(msg.GetHeartbeatSeconds()) * time.Second
	if interval <= 0 {
		interval = 10 * time.Second
	}
	a.socket.StartHeartbeat(interval)

	a.socket.Bus().Publish(&LoggedOnEvent{
		SteamID: a.socket.Session().SteamID(),
	})

	a.succeedLogin()

	a.logger.Info("Logon successful",
		log.Int32("heartbeat_seconds", msg.GetHeartbeatSeconds()),
		log.Uint32("public_ip", msg.GetPublicIp().GetV4()),
	)
}

// handleLoggedOff handles server-side disconnections (e.g., "Logged in elsewhere").
func (a *Authenticator) handleLoggedOff(packet *protocol.Packet) {
	resp := &pb.CMsgClientLoggedOff{}
	_ = proto.Unmarshal(packet.Payload, resp)

	eresult := protocol.EResult(resp.GetEresult())
	a.logger.Warn("Logged off by server", log.String("eresult", eresult.String()))

	if api.IsAuthError(eresult) {
		a.failLogin(api.ErrSessionExpired)
	}

	a.setState(StateDisconnected)

	// Propagate the logoff event to other modules
	a.socket.Bus().Publish(&LoggedOffEvent{
		Result: eresult,
	})
}

// sendLogOn constructs and sends the ClientLogon message.
func (a *Authenticator) sendLogOn(ctx context.Context, details *LogOnDetails) {
	logon := &pb.CMsgClientLogon{
		ProtocolVersion:           proto.Uint32(details.ProtocolVersion),
		ClientOsType:              proto.Uint32(uint32(details.ClientOSType)),
		ClientLanguage:            proto.String(details.ClientLanguage),
		MachineId:                 details.MachineID,
		MachineName:               proto.String("g-man"),
		SupportsRateLimitResponse: proto.Bool(true),
		ObfuscatedPrivateIp: &pb.CMsgIPAddress{
			Ip: &pb.CMsgIPAddress_V4{V4: a.config.LogonID ^ 0xbaadf00d},
		},
	}

	if details.RefreshToken != "" {
		a.logger.Debug("Logging on with Refresh Token")
		logon.AccessToken = proto.String(details.RefreshToken)
		logon.AccountName = nil
	} else {
		logon.AccountName = proto.String(details.AccountName)
		if details.TwoFactorCode != "" {
			logon.TwoFactorCode = proto.String(details.TwoFactorCode)
		}
	}

	a.logger.Info("Sending ClientLogon")

	if err := a.socket.SendProto(ctx, protocol.EMsg_ClientLogon, logon); err != nil {
		a.failLogin(fmt.Errorf("send logon failed: %w", err))
	}
}
