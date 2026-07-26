// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth

import (
	"encoding/binary"
	"errors"
	"time"

	"github.com/stretchr/testify/mock"
	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

func (s *AuthenticatorSuite) TestHandleChannelEncryptRequest_Failures() {
	s.auth.setLoginResult(make(chan error, 1))
	s.socket.SimulatePacketRaw(enums.EMsg_ChannelEncryptRequest, []byte{1, 2})
	s.ErrorContains(<-s.auth.getLoginResult(), "failed to read protocol version")

	payload := make([]byte, 4)
	s.socket.SimulatePacketRaw(enums.EMsg_ChannelEncryptRequest, payload)
	s.ErrorContains(<-s.auth.getLoginResult(), "failed to read universe")

	payload = make([]byte, 8)
	s.socket.SimulatePacketRaw(enums.EMsg_ChannelEncryptRequest, payload)
	s.ErrorContains(<-s.auth.getLoginResult(), "failed to read nonce")

	payload = make([]byte, 8+16)

	s.socket.On("SendRaw", mock.Anything, enums.EMsg_ChannelEncryptResponse, mock.Anything).
		Return(errors.New("network down")).
		Once()
	s.socket.SimulatePacketRaw(enums.EMsg_ChannelEncryptRequest, payload)
	s.ErrorContains(<-s.auth.getLoginResult(), "failed to send response")
}

func (s *AuthenticatorSuite) TestHandleChannelEncryptResult_Failures() {
	s.auth.setLoginResult(make(chan error, 1))
	s.socket.SimulatePacketRaw(enums.EMsg_ChannelEncryptResult, []byte{})
	s.ErrorContains(<-s.auth.getLoginResult(), "failed to read result code")

	s.auth.setLoginResult(make(chan error, 1))
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint32(payload, uint32(enums.EResult_Fail))
	s.socket.SimulatePacketRaw(enums.EMsg_ChannelEncryptResult, payload)
	s.ErrorContains(<-s.auth.getLoginResult(), "encryption failed with EResult")

	s.auth.setLoginResult(make(chan error, 1))
	binary.LittleEndian.PutUint32(payload, uint32(enums.EResult_OK))
	s.auth.tempKey.Store(nil)
	s.socket.SimulatePacketRaw(enums.EMsg_ChannelEncryptResult, payload)
	s.ErrorContains(<-s.auth.getLoginResult(), "no temporary session key found")

	s.auth.setLoginResult(make(chan error, 1))

	key := []byte("secret")
	s.auth.tempKey.Store(&key)
	s.auth.activeDetails.Store(nil)
	s.socket.SimulatePacketRaw(enums.EMsg_ChannelEncryptResult, payload)
	s.ErrorContains(<-s.auth.getLoginResult(), "login context or details are missing")
}

func (s *AuthenticatorSuite) TestHandleLogOnResponse_Coverage() {
	s.auth.setLoginResult(make(chan error, 1))
	s.socket.SimulatePacketRaw(enums.EMsg_ClientLogOnResponse, []byte{0xFF, 0x01})
	s.ErrorContains(<-s.auth.getLoginResult(), "unmarshal failed")

	s.socket.SimulatePacket(enums.EMsg_ClientLogOnResponse, &pb.CMsgClientLogonResponse{
		Eresult: proto.Int32(int32(enums.EResult_AccessDenied)),
	})
	s.Error(<-s.auth.getLoginResult())

	s.auth.setLoginResult(make(chan error, 1))
	s.socket.On("StartHeartbeat", 10*time.Second).Return().Once()

	hdr := protocol.NewMsgHdrProtoBuf(enums.EMsg_ClientLogOnResponse, 76561197960287930, 123)
	packet := &protocol.Packet{
		EMsg:       enums.EMsg_ClientLogOnResponse,
		IsProto:    true,
		HeaderKind: protocol.HeaderKindProto,
		HdrProto:   *hdr,
		Payload: func() []byte {
			b, _ := proto.Marshal(&pb.CMsgClientLogonResponse{
				Eresult:          proto.Int32(int32(enums.EResult_OK)),
				HeartbeatSeconds: proto.Int32(0),
			})

			return b
		}(),
	}

	s.auth.handleLogOnResponse(packet)
	s.NoError(<-s.auth.getLoginResult())
}

func (s *AuthenticatorSuite) TestHandleLoggedOff_Coverage() {
	s.socket.SimulatePacketRaw(enums.EMsg_ClientLoggedOff, []byte{0xFF})

	s.auth.setLoginResult(make(chan error, 1))
	s.socket.SimulatePacket(enums.EMsg_ClientLoggedOff, &pb.CMsgClientLoggedOff{
		Eresult: proto.Int32(int32(enums.EResult_AccountLogonDeniedVerifiedEmailRequired)),
	})
	s.Eventually(func() bool {
		return s.Equal(StateDisconnected, s.auth.State())
	}, 100*time.Millisecond, time.Second, "state should transition to disconnected")

	s.socket.SimulatePacket(enums.EMsg_ClientLoggedOff, &pb.CMsgClientLoggedOff{
		Eresult: proto.Int32(int32(enums.EResult_OK)),
	})
	s.Eventually(func() bool {
		return s.Equal(StateDisconnected, s.auth.State())
	}, 100*time.Millisecond, time.Second, "state should transition to disconnected")
}

func (s *AuthenticatorSuite) TestSendLogOn_Branches() {
	details := &LogOnDetails{
		RefreshToken: "token",
		MachineID:    []byte("id"),
	}

	s.socket.On("SendProto", mock.Anything, enums.EMsg_ClientLogon, mock.MatchedBy(func(m *pb.CMsgClientLogon) bool {
		return m.GetAccessToken() == "token" && m.AccountName == nil
	})).Return(nil).Once()
	s.auth.sendLogOn(s.T().Context(), details)

	details2 := &LogOnDetails{
		AccountName:   "user",
		TwoFactorCode: "12345",
	}

	s.socket.On("SendProto", mock.Anything, enums.EMsg_ClientLogon, mock.MatchedBy(func(m *pb.CMsgClientLogon) bool {
		return m.GetAccountName() == "user" && m.GetTwoFactorCode() == "12345"
	})).Return(nil).Once()
	s.auth.sendLogOn(s.T().Context(), details2)

	s.auth.setLoginResult(make(chan error, 1))
	s.socket.On("SendProto", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("fail")).Once()
	s.auth.sendLogOn(s.T().Context(), details2)
	s.ErrorContains(<-s.auth.getLoginResult(), "send logon failed")
}
