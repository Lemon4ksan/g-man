// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth

import (
	"context"
	"encoding/binary"
	"errors"
	"time"

	"github.com/stretchr/testify/mock"
	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

type mockAuthorizedHeader struct {
	protocol.Header
	steamID   uint64
	sessionID int32
}

func (m *mockAuthorizedHeader) GetSteamID() uint64  { return m.steamID }
func (m *mockAuthorizedHeader) GetSessionID() int32 { return m.sessionID }

func (s *AuthenticatorSuite) TestHandleChannelEncryptRequest_Failures() {
	// Fail to read protocol version (payload too short)
	s.auth.loginResult = make(chan error, 1)
	s.socket.SimulatePacketRaw(enums.EMsg_ChannelEncryptRequest, []byte{1, 2})
	s.ErrorContains(<-s.auth.loginResult, "failed to read protocol version")

	// Fail to read universe
	payload := make([]byte, 4) // only protocol version
	s.socket.SimulatePacketRaw(enums.EMsg_ChannelEncryptRequest, payload)
	s.ErrorContains(<-s.auth.loginResult, "failed to read universe")

	// Fail to read nonce
	payload = make([]byte, 8) // protocol + universe, no nonce
	s.socket.SimulatePacketRaw(enums.EMsg_ChannelEncryptRequest, payload)
	s.ErrorContains(<-s.auth.loginResult, "failed to read nonce")

	// SendRaw failure
	payload = make([]byte, 8+16)

	s.socket.On("SendRaw", mock.Anything, enums.EMsg_ChannelEncryptResponse, mock.Anything).
		Return(errors.New("network down")).
		Once()
	s.socket.SimulatePacketRaw(enums.EMsg_ChannelEncryptRequest, payload)
	s.ErrorContains(<-s.auth.loginResult, "failed to send response")
}

func (s *AuthenticatorSuite) TestHandleChannelEncryptResult_Failures() {
	// Fail to read result (empty payload)
	s.auth.loginResult = make(chan error, 1)
	s.socket.SimulatePacketRaw(enums.EMsg_ChannelEncryptResult, []byte{})
	s.ErrorContains(<-s.auth.loginResult, "failed to read result code")

	// EResult not OK
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint32(payload, uint32(enums.EResult_Fail))
	s.socket.SimulatePacketRaw(enums.EMsg_ChannelEncryptResult, payload)
	s.ErrorContains(<-s.auth.loginResult, "encryption failed with EResult")

	// No temporary session key found
	binary.LittleEndian.PutUint32(payload, uint32(enums.EResult_OK))
	s.auth.tempKey.Store(nil)
	s.socket.SimulatePacketRaw(enums.EMsg_ChannelEncryptResult, payload)
	s.ErrorContains(<-s.auth.loginResult, "no temporary session key found")

	// Missing active details
	key := []byte("secret")
	s.auth.tempKey.Store(&key)
	s.auth.activeDetails.Store(nil)
	s.socket.SimulatePacketRaw(enums.EMsg_ChannelEncryptResult, payload)
	s.ErrorContains(<-s.auth.loginResult, "login context or details are missing")
}

func (s *AuthenticatorSuite) TestHandleLogOnResponse_Coverage() {
	// Unmarshal failure
	s.auth.loginResult = make(chan error, 1)
	s.socket.SimulatePacketRaw(enums.EMsg_ClientLogOnResponse, []byte{0xFF, 0x01}) // Invalid proto
	s.ErrorContains(<-s.auth.loginResult, "unmarshal failed")

	// Denied by CM (Publishes LoggedOffEvent)
	sub := s.socket.Bus().Subscribe(&LoggedOffEvent{})
	s.socket.SimulatePacket(enums.EMsg_ClientLogOnResponse, &pb.CMsgClientLogonResponse{
		Eresult: proto.Int32(int32(enums.EResult_AccessDenied)),
	})
	s.Error(<-s.auth.loginResult)

	ev := (<-sub.C()).(*LoggedOffEvent)
	s.Equal(enums.EResult_AccessDenied, ev.Result)

	// Success with Header mapping and Heartbeat logic
	s.auth.loginResult = make(chan error, 1)
	s.socket.On("StartHeartbeat", 10*time.Second).Return().Once() // Case for 0 heartbeat seconds

	packet := &protocol.Packet{
		EMsg: enums.EMsg_ClientLogOnResponse,
		Payload: func() []byte {
			b, _ := proto.Marshal(&pb.CMsgClientLogonResponse{
				Eresult:          proto.Int32(int32(enums.EResult_OK)),
				HeartbeatSeconds: proto.Int32(0), // Should trigger 10s default
			})

			return b
		}(),
		Header: &mockAuthorizedHeader{steamID: 76561197960287930, sessionID: 123},
	}

	s.auth.handleLogOnResponse(packet)
	s.NoError(<-s.auth.loginResult)
}

func (s *AuthenticatorSuite) TestHandleLoggedOff_Coverage() {
	// Unmarshal fail
	s.socket.SimulatePacketRaw(enums.EMsg_ClientLoggedOff, []byte{0xFF})
	// Just logs error, no crash

	// Session Expired (Auth Error)
	sub := s.socket.bus.Subscribe(&LoggedOffEvent{})
	s.auth.loginResult = make(chan error, 1)
	s.socket.SimulatePacket(enums.EMsg_ClientLoggedOff, &pb.CMsgClientLoggedOff{
		Eresult: proto.Int32(int32(enums.EResult_AccountLogonDeniedVerifiedEmailRequired)),
	})
	<-sub.C()
	s.Equal(StateDisconnected, s.auth.State())

	// Normal Logoff
	s.socket.SimulatePacket(enums.EMsg_ClientLoggedOff, &pb.CMsgClientLoggedOff{
		Eresult: proto.Int32(int32(enums.EResult_OK)),
	})
	s.Equal(StateDisconnected, s.auth.State())
}

func (s *AuthenticatorSuite) TestSendLogOn_Branches() {
	//  Refresh Token branch
	details := &LogOnDetails{
		RefreshToken: "token",
		MachineID:    []byte("id"),
	}

	s.socket.On("SendProto", mock.Anything, enums.EMsg_ClientLogon, mock.MatchedBy(func(m *pb.CMsgClientLogon) bool {
		return m.GetAccessToken() == "token" && m.AccountName == nil
	})).Return(nil).Once()
	s.auth.sendLogOn(context.Background(), details)

	// AccountName + TwoFactor branch
	details2 := &LogOnDetails{
		AccountName:   "user",
		TwoFactorCode: "12345",
	}

	s.socket.On("SendProto", mock.Anything, enums.EMsg_ClientLogon, mock.MatchedBy(func(m *pb.CMsgClientLogon) bool {
		return m.GetAccountName() == "user" && m.GetTwoFactorCode() == "12345"
	})).Return(nil).Once()
	s.auth.sendLogOn(context.Background(), details2)

	// Send Failure
	s.auth.loginResult = make(chan error, 1)
	s.socket.On("SendProto", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("fail")).Once()
	s.auth.sendLogOn(context.Background(), details2)
	s.ErrorContains(<-s.auth.loginResult, "send logon failed")
}
