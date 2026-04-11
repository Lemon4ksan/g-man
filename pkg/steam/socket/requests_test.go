// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socket

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/internal/network"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/internal/session"
)

func TestBuilders(t *testing.T) {
	mockConn := newMockConnection()
	sess := session.New(mockConn)
	sess.SetSteamID(76561197960287930)
	sess.SetSessionID(12345)

	t.Run("Unified", func(t *testing.T) {
		method := "Player.GetOwnedGames#1"
		req := &pb.CMsgClientHeartBeat{}
		builder := Unified(method, req)

		buf := new(bytes.Buffer)
		jobID := uint64(888)
		err := builder(sess, buf, jobID, "test_token")
		if err != nil {
			t.Fatalf("Unified builder failed: %v", err)
		}

		pkt, err := protocol.ParsePacket(buf)
		if err != nil {
			t.Fatalf("Failed to parse built unified packet: %v", err)
		}

		if pkt.EMsg != enums.EMsg_ServiceMethodCallFromClient {
			t.Errorf("Expected EMsg_ServiceMethodCallFromClient, got %v", pkt.EMsg)
		}

		hdr, ok := pkt.Header.(*protocol.MsgHdrProtoBuf)
		if !ok {
			t.Fatal("Expected MsgHdrProtoBuf")
		}

		if hdr.Proto.GetTargetJobName() != method {
			t.Errorf("Expected method %s, got %s", method, hdr.Proto.GetTargetJobName())
		}
		if hdr.Proto.GetWgToken() != "test_token" {
			t.Errorf("Expected token test_token, got %s", hdr.Proto.GetWgToken())
		}
		if hdr.Proto.GetJobidSource() != jobID {
			t.Errorf("Expected jobID %d, got %d", jobID, hdr.Proto.GetJobidSource())
		}
	})

	t.Run("DynamicRaw_WithTarget", func(t *testing.T) {
		builder := DynamicRaw(enums.EMsg_ClientLogon, "SomeMethod", []byte("payload"))
		buf := new(bytes.Buffer)
		_ = builder(sess, buf, 1, "")

		pkt, _ := protocol.ParsePacket(buf)
		if !pkt.IsProto {
			t.Error("DynamicRaw with target should produce a Proto packet")
		}
	})

	t.Run("DynamicRaw_NoTarget", func(t *testing.T) {
		builder := DynamicRaw(enums.EMsg_ClientLogon, "", []byte("payload"))
		buf := new(bytes.Buffer)
		_ = builder(sess, buf, 1, "")

		pkt, _ := protocol.ParsePacket(buf)
		if pkt.IsProto {
			t.Error("DynamicRaw without target should not produce a Proto packet")
		}
	})
}

func TestSocket_SendMethods(t *testing.T) {
	mockConn := newMockConnection()
	cfg := DefaultTestConfig()
	cfg.Dialers = map[string]ConnectionDialer{
		"mock": func(nh network.Handler, l log.Logger, s string) (network.Connection, error) {
			return mockConn, nil
		},
	}

	sock := NewSocket(cfg)
	defer sock.Close()
	err := sock.Connect(t.Context(), CMServer{Type: "mock"})
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	mockSess := sock.Session().(*session.Logged)
	mockSess.SetSteamID(76561197960287930)
	mockSess.SetSessionID(12345)

	t.Run("SendUnified", func(t *testing.T) {
		err := sock.SendUnified(t.Context(), "Test.Method", &pb.CMsgClientHeartBeat{})
		if err != nil {
			t.Fatalf("SendUnified failed: %v", err)
		}

		data := <-mockConn.sentMsgs
		pkt, _ := protocol.ParsePacket(bytes.NewReader(data))
		if pkt.EMsg != enums.EMsg_ServiceMethodCallFromClient {
			t.Errorf("Unexpected EMsg: %v", pkt.EMsg)
		}
	})

	t.Run("SendRaw", func(t *testing.T) {
		testPayload := []byte("raw_binary_data")
		err := sock.SendRaw(t.Context(), enums.EMsg_ClientHeartBeat, testPayload)
		if err != nil {
			t.Fatalf("SendRaw failed: %v", err)
		}

		data := <-mockConn.sentMsgs
		pkt, err := protocol.ParsePacket(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("Failed to parse packet sent by SendRaw: %v", err)
		}

		if !bytes.Equal(pkt.Payload, testPayload) {
			t.Errorf("Payload mismatch in SendRaw. Expected %q, got %q", testPayload, pkt.Payload)
		}
		if pkt.IsProto {
			t.Error("SendRaw should produce non-proto packet")
		}

		hdr, ok := pkt.Header.(*protocol.MsgHdrExtended)
		if !ok {
			t.Fatalf("Expected MsgHdrExtended header, got %T", pkt.Header)
		}

		if hdr.SteamID != mockSess.SteamID() {
			t.Errorf("Expected SteamID %d from session, got %d", mockSess.SteamID(), hdr.SteamID)
		}
		if hdr.SessionID != mockSess.SessionID() {
			t.Errorf("Expected SessionID %d from session, got %d", mockSess.SessionID(), hdr.SessionID)
		}
		if hdr.SourceJobID != protocol.NoJob {
			t.Errorf("Expected SourceJobID to be NoJob (0), got %d", hdr.SourceJobID)
		}
	})
}

func TestSocket_SendSync(t *testing.T) {
	mockConn := newMockConnection()
	cfg := DefaultTestConfig()
	cfg.Dialers = map[string]ConnectionDialer{
		"mock": func(nh network.Handler, l log.Logger, s string) (network.Connection, error) {
			return mockConn, nil
		},
	}

	sock := NewSocket(cfg)
	defer sock.Close()
	_ = sock.Connect(t.Context(), CMServer{Type: "mock"})

	jobIDChan := make(chan uint64, 1)

	go func() {
		data := <-mockConn.sentMsgs
		pkt, _ := protocol.ParsePacket(bytes.NewReader(data))

		var sourceID uint64
		if pkt.IsProto {
			sourceID = pkt.Header.(*protocol.MsgHdrProtoBuf).Proto.GetJobidSource()
		} else {
			sourceID = pkt.Header.(*protocol.MsgHdrExtended).SourceJobID
		}

		jobIDChan <- sourceID

		resp := &protocol.Packet{
			EMsg: pkt.EMsg,
			Header: &protocol.MsgHdr{
				TargetJobID: sourceID,
			},
			Payload: []byte("sync_response"),
		}
		sock.msgCh <- resp
	}()

	t.Run("Success", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
		defer cancel()

		resp, err := sock.SendSync(ctx, Raw(enums.EMsg_ClientHeartBeat, []byte("req")))
		if err != nil {
			t.Fatalf("SendSync returned error: %v", err)
		}

		if string(resp.Payload) != "sync_response" {
			t.Errorf("Unexpected response payload: %s", string(resp.Payload))
		}
	})

	t.Run("Timeout", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := sock.SendSync(ctx, Raw(enums.EMsg_ClientHeartBeat, []byte("req")))
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected context.Canceled, got %v", err)
		}
	})
}
