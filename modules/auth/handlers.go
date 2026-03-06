// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"time"

	"github.com/lemon4ksan/g-man/log"
	"github.com/lemon4ksan/g-man/steam/crypto"
	"github.com/lemon4ksan/g-man/steam/protocol"
	pb "github.com/lemon4ksan/g-man/steam/protocol/protobuf"

	"google.golang.org/protobuf/proto"
)

// handleChannelEncryptRequest processes the initial handshake from Steam CM.
// It generates a symmetric session key and sends it back encrypted with Steam's public key.
func (a *Authenticator) handleChannelEncryptRequest(packet *protocol.Packet) {
	a.logger.Debug("Received ChannelEncryptRequest", log.Int("size", len(packet.Payload)))

	r := bytes.NewReader(packet.Payload)
	var protocolVer, universe uint32
	if err := binary.Read(r, binary.LittleEndian, &protocolVer); err != nil {
		a.failLogin(fmt.Errorf("encrypt_request read proto ver: %w", err))
		return
	}
	if err := binary.Read(r, binary.LittleEndian, &universe); err != nil {
		a.failLogin(fmt.Errorf("encrypt_request read universe: %w", err))
		return
	}

	nonce := make([]byte, 16)
	if _, err := io.ReadFull(r, nonce); err != nil {
		a.failLogin(fmt.Errorf("encrypt_request read nonce: %w", err))
		return
	}

	// Generate symmetric key for this session
	plainKey, encryptedKey, err := crypto.GenerateSessionKey(nonce)
	if err != nil {
		a.failLogin(fmt.Errorf("encrypt_request gen key: %w", err))
		return
	}

	// Store temporarily until CM confirms it
	a.mu.Lock()
	a.tempKey = plainKey
	a.mu.Unlock()

	// Structure: [ProtocolVersion] [KeySize] [EncryptedKey] [CRC32] [Trailer(0)]
	resp := new(bytes.Buffer)
	binary.Write(resp, binary.LittleEndian, protocolVer)
	binary.Write(resp, binary.LittleEndian, uint32(len(encryptedKey)))
	resp.Write(encryptedKey)
	binary.Write(resp, binary.LittleEndian, crc32.ChecksumIEEE(encryptedKey))
	binary.Write(resp, binary.LittleEndian, uint32(0))

	a.logger.Debug("Sending ChannelEncryptResponse", log.Int("key_size", len(encryptedKey)))

	// Use background context here as this is an asynchronous response to a CM event
	if err := a.socket.CallRaw(context.Background(), protocol.EMsg_ChannelEncryptResponse, resp.Bytes(), nil); err != nil {
		a.failLogin(fmt.Errorf("encrypt_request send failed: %w", err))
	}
}

// handleChannelEncryptResult confirms the secure channel is established.
// If successful, it triggers the token exchange and sends ClientLogon.
func (a *Authenticator) handleChannelEncryptResult(packet *protocol.Packet) {
	r := bytes.NewReader(packet.Payload)
	var result uint32
	if err := binary.Read(r, binary.LittleEndian, &result); err != nil {
		a.failLogin(fmt.Errorf("encrypt_result read result: %w", err))
		return
	}

	if eresult := protocol.EResult(result); eresult != protocol.EResult_OK {
		a.failLogin(fmt.Errorf("encryption failed: %s", eresult))
		return
	}

	a.mu.Lock()
	key := a.tempKey
	a.tempKey = nil // Clear temp key to prevent reuse
	details := a.details
	loginCtx := a.loginCtx // Grab the context tied to this login attempt
	a.mu.Unlock()

	if key == nil {
		a.failLogin(fmt.Errorf("encrypt_result no temp key found"))
		return
	}

	a.socket.Session().SetEncryptionKey(key)
	a.logger.Info("TCP Encryption established")

	if loginCtx == nil {
		loginCtx = context.Background()
	}

	// Proceed to send logon credentials over the encrypted channel
	a.sendLogOn(loginCtx, details, details.RefreshToken)
}

// handleLogOnResponse handles the final verdict from Steam.
func (a *Authenticator) handleLogOnResponse(packet *protocol.Packet) {
	resp := &pb.CMsgClientLogonResponse{}
	if err := proto.Unmarshal(packet.Payload, resp); err != nil {
		a.failLogin(fmt.Errorf("logon_response unmarshal: %w", err))
		return
	}

	eresult := protocol.EResult(resp.GetEresult())
	if eresult != protocol.EResult_OK {
		a.logger.Error("Logon denied by CM", log.String("eresult", eresult.String()))
		a.failLogin(fmt.Errorf("steam logon denied: %s", eresult))

		a.socket.Bus().Publish(&LoggedOffEvent{
			Result:     eresult,
		})
		return
	}

	a.logger.Info("Logon successful",
		log.Int32("hb_seconds", resp.GetHeartbeatSeconds()),
		log.Uint32("public_ip", resp.GetPublicIp().GetV4()),
	)

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
	interval := time.Duration(resp.GetHeartbeatSeconds()) * time.Second
	if interval <= 0 {
		interval = 10 * time.Second
	}
	a.socket.StartHeartbeat(interval)

	// Unblock the LogOn() function call successfully!
	a.succeedLogin()

	a.socket.Bus().Publish(&LoggedOnEvent{
		SteamID: a.socket.Session().SteamID(),
	})
}

// handleLoggedOff handles server-side disconnections (e.g. logged in elsewhere).
func (a *Authenticator) handleLoggedOff(packet *protocol.Packet) {
	resp := &pb.CMsgClientLoggedOff{}
	_ = proto.Unmarshal(packet.Payload, resp)

	eresult := protocol.EResult(resp.GetEresult())
	a.logger.Warn("Logged off by server", log.String("eresult", eresult.String()))

	a.setState(StateDisconnected)

	a.socket.Bus().Publish(&LoggedOffEvent{
		Result:     eresult,
	})
}

func (a *Authenticator) sendLogOn(ctx context.Context, details *LogOnDetails, accessToken string) {
	logon := &pb.CMsgClientLogon{
		ProtocolVersion:           proto.Uint32(details.ProtocolVersion),
		ClientOsType:              proto.Uint32(uint32(details.ClientOSType)),
		ClientLanguage:            proto.String(details.ClientLanguage),
		MachineId:                 details.MachineID,
		MachineName:               proto.String("g-man"),
		AccessToken:               proto.String(accessToken),
		SupportsRateLimitResponse: proto.Bool(true),
		ObfuscatedPrivateIp: &pb.CMsgIPAddress{
			Ip: &pb.CMsgIPAddress_V4{V4: a.config.LogonID ^ 0xbaadf00d},
		},
	}

	if details.AuthCode != "" {
		logon.AuthCode = proto.String(details.AuthCode)
	}
	if details.TwoFactorCode != "" {
		logon.TwoFactorCode = proto.String(details.TwoFactorCode)
	}
	if details.CellID > 0 {
		logon.CellId = proto.Uint32(details.CellID)
	}

	a.logger.Info("Sending ClientLogon", log.Uint64("steam_id", details.SteamID))

	// If this fails, we kill the login process.
	if err := a.socket.CallProto(ctx, protocol.EMsg_ClientLogon, logon, nil); err != nil {
		a.failLogin(fmt.Errorf("send logon failed: %w", err))
	}
}
