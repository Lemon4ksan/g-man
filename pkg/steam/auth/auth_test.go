// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/g-man/pkg/bus"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	"google.golang.org/protobuf/proto"
)

type mockSession struct {
	steamID   uint64
	sessionID int32
	key       []byte
	token     string
	access    string
}

func (m *mockSession) SteamID() uint64                             { return m.steamID }
func (m *mockSession) SetSteamID(id uint64)                        { m.steamID = id }
func (m *mockSession) SessionID() int32                            { return m.sessionID }
func (m *mockSession) SetSessionID(id int32)                       { m.sessionID = id }
func (m *mockSession) SetEncryptionKey(k []byte) bool              { m.key = k; return true }
func (m *mockSession) IsEncrypted() bool                           { return m.key != nil }
func (m *mockSession) RefreshToken() string                        { return m.token }
func (m *mockSession) AccessToken() string                         { return m.access }
func (m *mockSession) SetRefreshToken(t string)                    { m.token = t }
func (m *mockSession) SetAccessToken(t string)                     { m.access = t }
func (m *mockSession) Close() error                                { return nil }
func (m *mockSession) IsAuthenticated() bool                       { return m.token != "" }
func (m *mockSession) Send(ctx context.Context, data []byte) error { return nil }

type mockSocketProvider struct {
	mu       sync.Mutex
	handlers map[enums.EMsg]socket.Handler
	eventBus *bus.Bus
	sess     *mockSession

	connectErr error
	sentProtos map[enums.EMsg]proto.Message
	sentRaws   map[enums.EMsg][]byte

	hbStarted  bool
	hbDuration time.Duration

	onSendProto func(eMsg enums.EMsg, req proto.Message)
}

func newMockSocket() *mockSocketProvider {
	return &mockSocketProvider{
		handlers:   make(map[enums.EMsg]socket.Handler),
		eventBus:   bus.NewBus(),
		sess:       &mockSession{},
		sentProtos: make(map[enums.EMsg]proto.Message),
		sentRaws:   make(map[enums.EMsg][]byte),
	}
}

func (m *mockSocketProvider) RegisterMsgHandler(eMsg enums.EMsg, handler socket.Handler) {
	m.mu.Lock()
	m.handlers[eMsg] = handler
	m.mu.Unlock()
}

func (m *mockSocketProvider) Connect(ctx context.Context, server socket.CMServer) error {
	return m.connectErr
}

func (m *mockSocketProvider) SendProto(ctx context.Context, eMsg enums.EMsg, req proto.Message, opts ...socket.SendOption) error {
	m.mu.Lock()
	m.sentProtos[eMsg] = req
	m.mu.Unlock()
	if m.onSendProto != nil {
		m.onSendProto(eMsg, req)
	}
	return nil
}

func (m *mockSocketProvider) SendRaw(ctx context.Context, eMsg enums.EMsg, payload []byte, opts ...socket.SendOption) error {
	m.mu.Lock()
	m.sentRaws[eMsg] = payload
	m.mu.Unlock()
	return nil
}

func (m *mockSocketProvider) Session() socket.Session { return m.sess }
func (m *mockSocketProvider) Bus() *bus.Bus           { return m.eventBus }
func (m *mockSocketProvider) StartHeartbeat(d time.Duration) {
	m.mu.Lock()
	m.hbStarted = true
	m.hbDuration = d
	m.mu.Unlock()
}

func (m *mockSocketProvider) simulatePacket(eMsg enums.EMsg, payload []byte) {
	m.mu.Lock()
	handler := m.handlers[eMsg]
	m.mu.Unlock()
	if handler != nil {
		handler(&protocol.Packet{EMsg: eMsg, Payload: payload})
	}
}

type mockWebAuth struct {
	beginResp *pb.CAuthentication_BeginAuthSessionViaCredentials_Response
	pollResp  *pb.CAuthentication_PollAuthSessionStatus_Response
	genResp   *pb.CAuthentication_AccessToken_GenerateForApp_Response
	err       error
}

func (m *mockWebAuth) BeginAuthSessionViaCredentials(ctx context.Context, user, pass, _ string) (*pb.CAuthentication_BeginAuthSessionViaCredentials_Response, error) {
	return m.beginResp, m.err
}
func (m *mockWebAuth) PollAuthSessionStatus(ctx context.Context, id uint64, reqID []byte) (*pb.CAuthentication_PollAuthSessionStatus_Response, error) {
	return m.pollResp, m.err
}
func (m *mockWebAuth) UpdateAuthSessionWithSteamGuardCode(ctx context.Context, cID, sID uint64, code string, t pb.EAuthSessionGuardType) error {
	return m.err
}
func (m *mockWebAuth) GenerateAccessTokenForApp(ctx context.Context, refreshToken string, steamID uint64) (*pb.CAuthentication_AccessToken_GenerateForApp_Response, error) {
	return m.genResp, m.err
}

func TestAuthenticator_CryptoHandshake(t *testing.T) {
	s := newMockSocket()
	w := &mockWebAuth{}
	a := NewAuthenticator(s, w, DefaultConfig())

	ctx, cancel := context.WithCancelCause(context.Background())
	details := &LogOnDetails{RefreshToken: "test_token"}

	a.loginCtx.Store(ctx)
	a.loginCancel.Store(cancel)
	a.activeDetails.Store(details)
	a.state.Store(int32(StateLoggingOn))

	t.Run("Receive ChannelEncryptRequest", func(t *testing.T) {
		nonce := make([]byte, 16)
		buf := new(bytes.Buffer)
		binary.Write(buf, binary.LittleEndian, uint32(ProtocolVersion))
		binary.Write(buf, binary.LittleEndian, uint32(enums.EUniverse_Public))
		buf.Write(nonce)

		s.simulatePacket(enums.EMsg_ChannelEncryptRequest, buf.Bytes())

		if a.tempKey.Load() == nil {
			t.Error("tempKey should be stored after request")
		}

		s.mu.Lock()
		resp := s.sentRaws[enums.EMsg_ChannelEncryptResponse]
		s.mu.Unlock()

		if resp == nil {
			t.Fatal("expected ChannelEncryptResponse to be sent")
		}

		keyLen := binary.LittleEndian.Uint32(resp[4:8])
		checksum := binary.LittleEndian.Uint32(resp[8+keyLen : 12+keyLen])
		if checksum != crc32.ChecksumIEEE(resp[8:8+keyLen]) {
			t.Error("invalid checksum in encrypt response")
		}
	})

	t.Run("Receive ChannelEncryptResult OK", func(t *testing.T) {
		res := make([]byte, 4)
		binary.LittleEndian.PutUint32(res, uint32(enums.EResult_OK))

		s.simulatePacket(enums.EMsg_ChannelEncryptResult, res)

		if !s.sess.IsEncrypted() {
			t.Error("session key should be applied to socket")
		}
		if a.tempKey.Load() != nil {
			t.Error("tempKey should be cleared after activation")
		}

		s.mu.Lock()
		logonMsg := s.sentProtos[enums.EMsg_ClientLogon]
		s.mu.Unlock()

		if logonMsg == nil {
			t.Error("ClientLogon should be sent after encryption success")
		}
	})
}

func TestAuthenticator_LogOn_WebSocketFlow(t *testing.T) {
	s := newMockSocket()
	w := &mockWebAuth{}
	a := NewAuthenticator(s, w, DefaultConfig())

	s.onSendProto = func(eMsg enums.EMsg, req proto.Message) {
		if eMsg == enums.EMsg_ClientLogon {
			resp := &pb.CMsgClientLogonResponse{
				Eresult:          proto.Int32(int32(enums.EResult_OK)),
				HeartbeatSeconds: proto.Int32(30),
			}
			data, _ := proto.Marshal(resp)
			s.simulatePacket(enums.EMsg_ClientLogOnResponse, data)
		}
	}

	details := &LogOnDetails{RefreshToken: "valid_token", SteamID: 12345}
	server := socket.CMServer{Type: "websockets"}

	err := a.LogOn(context.Background(), details, server)
	if err != nil {
		t.Fatalf("LogOn failed: %v", err)
	}

	if a.State() != StateLoggedOn {
		t.Errorf("expected state LoggedOn, got %s", a.State())
	}

	s.mu.Lock()
	hb := s.hbStarted
	s.mu.Unlock()
	if !hb {
		t.Error("heartbeat should be started after successful logon")
	}
}

func TestAuthenticator_LogOn_Failure(t *testing.T) {
	s := newMockSocket()
	w := &mockWebAuth{}
	a := NewAuthenticator(s, w, DefaultConfig())

	s.onSendProto = func(eMsg enums.EMsg, req proto.Message) {
		if eMsg == enums.EMsg_ClientLogon {
			resp := &pb.CMsgClientLogonResponse{
				Eresult: proto.Int32(int32(enums.EResult_InvalidPassword)),
			}
			data, _ := proto.Marshal(resp)
			s.simulatePacket(enums.EMsg_ClientLogOnResponse, data)
		}
	}

	details := &LogOnDetails{RefreshToken: "bad_token"}
	err := a.LogOn(context.Background(), details, socket.CMServer{Type: "websockets"})

	if err == nil {
		t.Fatal("expected error for invalid password, got nil")
	}
	if a.State() != StateFailed {
		t.Errorf("expected state Failed, got %s", a.State())
	}
}

func TestAuthenticator_LogOff_Detection(t *testing.T) {
	s := newMockSocket()
	w := &mockWebAuth{}
	a := NewAuthenticator(s, w, DefaultConfig())

	a.state.Store(int32(StateLoggedOn))
	sub := s.Bus().Subscribe(&LoggedOffEvent{})

	resp := &pb.CMsgClientLoggedOff{
		Eresult: proto.Int32(int32(enums.EResult_LoggedInElsewhere)),
	}
	data, _ := proto.Marshal(resp)
	s.simulatePacket(enums.EMsg_ClientLoggedOff, data)

	if a.State() != StateDisconnected {
		t.Errorf("expected state Disconnected, got %s", a.State())
	}

	select {
	case ev := <-sub.C():
		offEv := ev.(*LoggedOffEvent)
		if offEv.Result != enums.EResult_LoggedInElsewhere {
			t.Errorf("unexpected logoff result: %v", offEv.Result)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("LoggedOffEvent not published to bus")
	}
}
