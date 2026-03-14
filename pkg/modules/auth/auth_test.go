// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/g-man/pkg/steam/bus"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	pb "github.com/lemon4ksan/g-man/pkg/steam/protocol/protobuf"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	"google.golang.org/protobuf/proto"
)

type mockSession struct {
	steamID uint64
	key     []byte
}

func (m *mockSession) SteamID() uint64                { return m.steamID }
func (m *mockSession) SetSteamID(id uint64)           { m.steamID = id }
func (m *mockSession) SessionID() int32               { return 0 }
func (m *mockSession) SetSessionID(id int32)          {}
func (m *mockSession) SetEncryptionKey(k []byte) bool { m.key = k; return true }
func (m *mockSession) IsEncrypted() bool              { return m.key != nil }

func (m *mockSession) AccessToken() string {
	panic("unimplemented")
}
func (m *mockSession) Close() error {
	panic("unimplemented")
}
func (m *mockSession) IsAuthenticated() bool {
	panic("unimplemented")
}
func (m *mockSession) Send(ctx context.Context, data []byte) error {
	panic("unimplemented")
}
func (m *mockSession) SetAccessToken(string) {
	panic("unimplemented")
}

type mockSocketProvider struct {
	mu         sync.Mutex
	handlers   map[protocol.EMsg]socket.Handler
	connectErr error
	protoCalls map[protocol.EMsg]proto.Message
	rawCalls   map[protocol.EMsg][]byte
	sess       *mockSession
	eventBus   *bus.Bus
	hbStarted  bool
	hbDuration time.Duration
}

func newMockSocket() *mockSocketProvider {
	return &mockSocketProvider{
		handlers:   make(map[protocol.EMsg]socket.Handler),
		protoCalls: make(map[protocol.EMsg]proto.Message),
		rawCalls:   make(map[protocol.EMsg][]byte),
		sess:       &mockSession{},
		eventBus:   bus.NewBus(),
	}
}

func (m *mockSocketProvider) RegisterMsgHandler(eMsg protocol.EMsg, handler socket.Handler) {
	m.handlers[eMsg] = handler
}

func (m *mockSocketProvider) Connect(ctx context.Context, server socket.CMServer) error {
	return m.connectErr
}

func (m *mockSocketProvider) SendProto(ctx context.Context, eMsg protocol.EMsg, req proto.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.protoCalls[eMsg] = req
	return nil
}

func (m *mockSocketProvider) SendRaw(ctx context.Context, eMsg protocol.EMsg, payload []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rawCalls[eMsg] = payload
	return nil
}

func (m *mockSocketProvider) Session() socket.Session { return m.sess }
func (m *mockSocketProvider) Bus() *bus.Bus           { return m.eventBus }
func (m *mockSocketProvider) StartHeartbeat(d time.Duration) {
	m.hbStarted = true
	m.hbDuration = d
}

func (m *mockSocketProvider) simulateIncomingPacket(eMsg protocol.EMsg, payload []byte, header protocol.Header) {
	if handler, ok := m.handlers[eMsg]; ok {
		handler(&protocol.Packet{EMsg: eMsg, Payload: payload, Header: header})
	}
}

type mockWebAuthenticator struct {
	mu                        sync.Mutex
	beginAuthResp             *pb.CAuthentication_BeginAuthSessionViaCredentials_Response
	beginAuthErr              error
	pollResp                  *pb.CAuthentication_PollAuthSessionStatus_Response
	pollErr                   error
	updateGuardErr            error
	updateGuardCalledWithCode string
	updateGuardCalledWithType pb.EAuthSessionGuardType
}

func (m *mockWebAuthenticator) BeginAuthSessionViaCredentials(ctx context.Context, accountName, password string, authCode string) (*pb.CAuthentication_BeginAuthSessionViaCredentials_Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.beginAuthResp, m.beginAuthErr
}

func (m *mockWebAuthenticator) PollAuthSessionStatus(ctx context.Context, clientID uint64, requestID []byte) (*pb.CAuthentication_PollAuthSessionStatus_Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pollResp, m.pollErr
}

func (m *mockWebAuthenticator) UpdateAuthSessionWithSteamGuardCode(ctx context.Context, clientID uint64, steamID uint64, code string, codeType pb.EAuthSessionGuardType) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateGuardCalledWithCode = code
	m.updateGuardCalledWithType = codeType
	return m.updateGuardErr
}

func TestAuthenticator_StateTransitions(t *testing.T) {
	s := newMockSocket()
	a := NewAuthenticator(s, nil, DefaultConfig())

	if a.State() != StateDisconnected {
		t.Errorf("expected initial state Disconnected, got %s", a.State())
	}

	sub := s.eventBus.Subscribe(&StateEvent{})

	a.setState(StateAuthenticating)

	select {
	case ev := <-sub.C():
		stateEv := ev.(*StateEvent)
		if stateEv.Old != StateDisconnected || stateEv.New != StateAuthenticating {
			t.Errorf("invalid state transition event: %+v", stateEv)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for StateEvent")
	}
}

func TestAuthenticator_Validate(t *testing.T) {
	a := NewAuthenticator(newMockSocket(), nil, DefaultConfig())

	tests := []struct {
		name    string
		details *LogOnDetails
		wantErr bool
	}{
		{"Nil Details", nil, true},
		{"No Token and No Account", &LogOnDetails{}, true},
		{"Account but No Password", &LogOnDetails{AccountName: "user"}, true},
		{"Valid Token", &LogOnDetails{RefreshToken: "jwt_token"}, false},
		{"Valid Password", &LogOnDetails{AccountName: "user", Password: "pwd"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := a.validate(tt.details)
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err == nil {
				if tt.details.ClientOSType != uint32(protocol.EOSType_Windows10) {
					t.Error("expected default OSType")
				}
				if tt.details.ClientLanguage != "english" {
					t.Error("expected default language")
				}
			}
		})
	}
}

func TestAuthenticator_LogOn_WebSocketBypass(t *testing.T) {
	s := newMockSocket()
	a := NewAuthenticator(s, nil, DefaultConfig())

	details := &LogOnDetails{RefreshToken: "my_token", SteamID: 12345}
	server := socket.CMServer{Type: "websockets"}

	go func() {
		_ = a.LogOn(context.Background(), details, server)
	}()

	time.Sleep(50 * time.Millisecond)

	s.mu.Lock()
	logonMsg, ok := s.protoCalls[protocol.EMsg_ClientLogon]
	s.mu.Unlock()

	if !ok {
		t.Fatal("expected ClientLogon to be sent immediately for websockets")
	}

	req := logonMsg.(*pb.CMsgClientLogon)
	if req.GetAccessToken() != "my_token" {
		t.Errorf("expected access token 'my_token', got %s", req.GetAccessToken())
	}

	resp := &pb.CMsgClientLogonResponse{
		Eresult:          proto.Int32(int32(protocol.EResult_OK)),
		HeartbeatSeconds: proto.Int32(30),
	}
	payload, _ := proto.Marshal(resp)

	s.simulateIncomingPacket(protocol.EMsg_ClientLogOnResponse, payload, nil)

	time.Sleep(50 * time.Millisecond)

	if a.State() != StateLoggedOn {
		t.Errorf("expected state LoggedOn, got %s", a.State())
	}
	if !s.hbStarted || s.hbDuration != 30*time.Second {
		t.Errorf("expected heartbeat to be started with 30s interval")
	}
}

func TestAuthenticator_CryptoHandshake(t *testing.T) {
	s := newMockSocket()
	a := NewAuthenticator(s, nil, DefaultConfig())

	nonce := make([]byte, 16)
	for i := range nonce {
		nonce[i] = byte(i)
	}

	reqBuf := new(bytes.Buffer)
	binary.Write(reqBuf, binary.LittleEndian, uint32(ProtocolVersion))
	binary.Write(reqBuf, binary.LittleEndian, uint32(protocol.EUniverse_Public))
	reqBuf.Write(nonce)

	s.simulateIncomingPacket(protocol.EMsg_ChannelEncryptRequest, reqBuf.Bytes(), nil)

	s.mu.Lock()
	respPayload, ok := s.rawCalls[protocol.EMsg_ChannelEncryptResponse]
	s.mu.Unlock()

	if !ok {
		t.Fatal("expected ChannelEncryptResponse to be sent")
	}

	respReader := bytes.NewReader(respPayload)
	var protVer, keySize uint32
	binary.Read(respReader, binary.LittleEndian, &protVer)
	binary.Read(respReader, binary.LittleEndian, &keySize)

	encryptedKey := make([]byte, keySize)
	respReader.Read(encryptedKey)

	var checksum uint32
	binary.Read(respReader, binary.LittleEndian, &checksum)

	if protVer != ProtocolVersion {
		t.Errorf("invalid protocol version in response: %d", protVer)
	}
	if checksum != crc32.ChecksumIEEE(encryptedKey) {
		t.Error("invalid crc32 checksum in response")
	}

	a.mu.RLock()
	tempKey := a.tempKey
	a.mu.RUnlock()

	if len(tempKey) != 32 {
		t.Fatalf("expected 32-byte AES session key to be generated, got len %d", len(tempKey))
	}

	a.mu.Lock()
	a.details = &LogOnDetails{RefreshToken: "jwt"}
	a.loginCtx = context.Background()
	a.mu.Unlock()

	resBuf := new(bytes.Buffer)
	binary.Write(resBuf, binary.LittleEndian, uint32(protocol.EResult_OK))

	s.simulateIncomingPacket(protocol.EMsg_ChannelEncryptResult, resBuf.Bytes(), nil)

	if !bytes.Equal(s.sess.key, tempKey) {
		t.Error("expected session encryption key to be set")
	}

	a.mu.RLock()
	if a.tempKey != nil {
		t.Error("expected tempKey to be cleared")
	}
	a.mu.RUnlock()

	s.mu.Lock()
	_, logonSent := s.protoCalls[protocol.EMsg_ClientLogon]
	s.mu.Unlock()

	if !logonSent {
		t.Error("expected ClientLogon to be sent after successful encryption")
	}
}

func TestAuthenticator_HandleLogOff(t *testing.T) {
	s := newMockSocket()
	a := NewAuthenticator(s, nil, DefaultConfig())

	a.setState(StateLoggedOn)

	sub := s.eventBus.Subscribe(&LoggedOffEvent{})

	msg := &pb.CMsgClientLoggedOff{
		Eresult: proto.Int32(int32(protocol.EResult_LoggedInElsewhere)),
	}
	payload, _ := proto.Marshal(msg)

	s.simulateIncomingPacket(protocol.EMsg_ClientLoggedOff, payload, nil)

	if a.State() != StateDisconnected {
		t.Errorf("expected state to change to Disconnected, got %s", a.State())
	}

	select {
	case ev := <-sub.C():
		offEv := ev.(*LoggedOffEvent)
		if offEv.Result != protocol.EResult_LoggedInElsewhere {
			t.Errorf("expected LoggedInElsewhere result, got %s", offEv.Result)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for LoggedOffEvent")
	}
}

func TestAuthenticator_LogOn_FullPasswordFlow(t *testing.T) {
	s := newMockSocket()

	webAuth := &mockWebAuthenticator{
		beginAuthResp: &pb.CAuthentication_BeginAuthSessionViaCredentials_Response{
			ClientId: proto.Uint64(123),
			Steamid:  proto.Uint64(76561197960287930),
		},
		pollResp: &pb.CAuthentication_PollAuthSessionStatus_Response{
			RefreshToken: proto.String("jwt_token_from_poll"),
		},
	}

	a := NewAuthenticator(s, webAuth, DefaultConfig())

	details := &LogOnDetails{
		AccountName: "test_user",
		Password:    "password123",
	}
	server := socket.CMServer{Type: "websockets"}

	var loginErr error
	var wg sync.WaitGroup

	wg.Go(func() {
		loginErr = a.LogOn(context.Background(), details, server)
	})

	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		_, sent := s.protoCalls[protocol.EMsg_ClientLogon]
		s.mu.Unlock()
		if sent {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for ClientLogon to be sent")
		}
		time.Sleep(10 * time.Millisecond)
	}

	resp := &pb.CMsgClientLogonResponse{
		Eresult:          proto.Int32(int32(protocol.EResult_OK)),
		HeartbeatSeconds: proto.Int32(30),
	}
	payload, _ := proto.Marshal(resp)

	header := &protocol.MsgHdrProtoBuf{
		EMsg:  protocol.EMsg_ClientLogOnResponse,
		Proto: &pb.CMsgProtoBufHeader{Steamid: proto.Uint64(999)},
	}

	s.simulateIncomingPacket(protocol.EMsg_ClientLogOnResponse, payload, header)

	wg.Wait()

	if loginErr != nil {
		t.Errorf("LogOn returned error: %v", loginErr)
	}

	if a.State() != StateLoggedOn {
		t.Errorf("expected state LoggedOn, got %v", a.State())
	}
}

func TestAuthenticator_LogOn_FailurePath(t *testing.T) {
	s := newMockSocket()
	webAuth := &mockWebAuthenticator{
		beginAuthResp: &pb.CAuthentication_BeginAuthSessionViaCredentials_Response{
			ClientId: proto.Uint64(123),
		},
		pollResp: &pb.CAuthentication_PollAuthSessionStatus_Response{
			RefreshToken: proto.String("valid_token"),
		},
	}
	a := NewAuthenticator(s, webAuth, DefaultConfig())

	go func() {
		time.Sleep(50 * time.Millisecond)

		resp := &pb.CMsgClientLogonResponse{
			Eresult: proto.Int32(int32(protocol.EResult_InvalidPassword)),
		}
		payload, _ := proto.Marshal(resp)
		s.simulateIncomingPacket(protocol.EMsg_ClientLogOnResponse, payload, nil)
	}()

	err := a.LogOn(context.Background(), &LogOnDetails{RefreshToken: "abc"}, socket.CMServer{Type: "websockets"})

	if err == nil {
		t.Fatal("expected error from failed logon, got nil")
	}
	if !strings.Contains(err.Error(), "InvalidPassword") {
		t.Errorf("unexpected error message: %v", err)
	}
}
