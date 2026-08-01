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

	"github.com/lemon4ksan/miyako/log"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/internal/crypto"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
)

var (
	ErrNoTempSessionKey = errors.New("encrypt_result: no temporary session key found to activate")
	ErrMissingDetails   = errors.New("encrypt_result: login context or details are missing")
)

func (a *Authenticator) handleChannelEncryptRequest(packet *protocol.Packet) {
	a.getLogger().Debug("Received ChannelEncryptRequest", log.Int("size", len(packet.Payload)))

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

	var nonce [16]byte
	if _, err := io.ReadFull(r, nonce[:]); err != nil {
		a.failLogin(fmt.Errorf("encrypt_request: failed to read nonce: %w", err))
		return
	}

	plainKey, encryptedKey, err := crypto.GenerateSessionKey(nonce[:])
	if err != nil {
		a.failLogin(fmt.Errorf("encrypt_request: failed to generate session key: %w", err))
		return
	}

	a.tempKey.Store(&plainKey)

	resp := new(bytes.Buffer)
	_ = binary.Write(resp, binary.LittleEndian, protocolVer)
	_ = binary.Write(resp, binary.LittleEndian, uint32(len(encryptedKey)))
	resp.Write(encryptedKey)
	_ = binary.Write(resp, binary.LittleEndian, crc32.ChecksumIEEE(encryptedKey))
	_ = binary.Write(resp, binary.LittleEndian, uint32(0))

	a.getLogger().Debug("Sending ChannelEncryptResponse", log.Int("key_size", len(encryptedKey)))

	if err := a.socket.SendRaw(context.Background(), enums.EMsg_ChannelEncryptResponse, resp.Bytes()); err != nil {
		a.failLogin(fmt.Errorf("encrypt_request: failed to send response: %w", err))
	}
}

func (a *Authenticator) handleChannelEncryptResult(packet *protocol.Packet) {
	r := bytes.NewReader(packet.Payload)

	var result uint32
	if err := binary.Read(r, binary.LittleEndian, &result); err != nil {
		a.failLogin(fmt.Errorf("encrypt_result: failed to read result code: %w", err))
		return
	}

	if eresult := enums.EResult(result); eresult != enums.EResult_OK {
		a.failLogin(fmt.Errorf("encryption failed with EResult: %s", eresult))
		return
	}

	keyPtr := a.tempKey.Swap(nil)
	if keyPtr == nil || *keyPtr == nil {
		a.failLogin(ErrNoTempSessionKey)
		return
	}

	a.socket.SetEncryptionKey(*keyPtr)
	a.getLogger().Info("TCP Encryption established")

	details := a.activeDetails.Load()
	if details == nil {
		a.failLogin(ErrMissingDetails)
		return
	}

	a.sendLogOn(context.Background(), details)
}

func (a *Authenticator) handleLogOnResponse(packet *protocol.Packet) {
	msg := &pb.CMsgClientLogonResponse{}
	if err := protocol.UnmarshalProto(packet.Payload, msg); err != nil {
		a.failLogin(fmt.Errorf("logon_response: unmarshal failed: %w", err))
		return
	}

	if res := enums.EResult(msg.GetEresult()); res != enums.EResult_OK {
		a.getLogger().Error("Logon denied by CM", log.Int32("eresult", int32(res)))
		a.failLogin(service.NewEResultError(res, nil))

		return
	}

	sess := a.socket.Session()
	if steamID := packet.GetSteamID(); steamID != 0 {
		sess.SetSteamID(steamID)
	}

	if sessionID := packet.GetSessionID(); sessionID != 0 {
		sess.SetSessionID(sessionID)
	}

	interval := time.Duration(msg.GetHeartbeatSeconds()) * time.Second
	if interval <= 0 {
		interval = 10 * time.Second
	}

	if err := a.socket.StartHeartbeat(interval); err != nil {
		a.getLogger().Error("Failed to start heartbeat", log.Err(err))
		a.failLogin(fmt.Errorf("logon_response: failed to start heartbeat: %w", err))

		return
	}

	a.bus.Publish(&LoggedOnEvent{
		SteamID: a.socket.Session().SteamID(),
	})

	a.succeedLogin()

	a.getLogger().Info("Logon successful",
		log.Int32("heartbeat_seconds", msg.GetHeartbeatSeconds()),
		log.Uint32("public_ip", msg.GetPublicIp().GetV4()),
	)
}

func (a *Authenticator) handleLoggedOff(packet *protocol.Packet) {
	res := enums.EResult_OK

	if packet.IsProto {
		resp := &pb.CMsgClientLoggedOff{}
		if err := protocol.UnmarshalProto(packet.Payload, resp); err != nil {
			a.getLogger().Error("Unmarshal failed in handleLoggedOff", log.Err(err))
		} else {
			res = enums.EResult(resp.GetEresult())
		}
	} else if len(packet.Payload) >= 4 {
		res = enums.EResult(binary.LittleEndian.Uint32(packet.Payload[:4]))
	}

	a.getLogger().Warn("Logged off by server", log.Int32("eresult", int32(res)))

	if service.IsAuthError(res) {
		a.failLogin(service.ErrSessionExpired)
	}

	a.setState(StateDisconnected)
}

func (a *Authenticator) sendLogOn(ctx context.Context, details *LogOnDetails) {
	logon := &pb.CMsgClientLogon{
		ProtocolVersion:           proto.Uint32(details.ProtocolVersion),
		ClientOsType:              proto.Uint32(details.ClientOSType),
		ClientLanguage:            proto.String(details.ClientLanguage),
		MachineId:                 details.MachineID,
		MachineName:               proto.String(details.MachineName),
		SupportsRateLimitResponse: proto.Bool(true),
		ObfuscatedPrivateIp: &pb.CMsgIPAddress{
			Ip: &pb.CMsgIPAddress_V4{V4: uint32(time.Now().Unix()) ^ 0xbaadf00d},
		},
	}

	if details.RefreshToken != "" {
		logon.AccessToken = proto.String(details.RefreshToken)
		logon.AccountName = nil
	} else {
		logon.AccountName = proto.String(details.AccountName)
		if details.TwoFactorCode != "" {
			logon.TwoFactorCode = proto.String(details.TwoFactorCode)
		}
	}

	if err := a.socket.SendProto(ctx, enums.EMsg_ClientLogon, logon); err != nil {
		a.failLogin(fmt.Errorf("send logon failed: %w", err))
	}
}
