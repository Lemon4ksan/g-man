// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socket

import (
	"bytes"
	"strings"
	"testing"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestBaseEncoder_NilBodyErrors(t *testing.T) {
	enc := &BaseEncoder{}
	buf := &bytes.Buffer{}

	const expectedErr = "proto body is nil"

	tests := []struct {
		name string
		fn   func() error
	}{
		{
			name: "EncodeProto",
			fn: func() error {
				return enc.EncodeProto(buf, protocol.EMsg_ClientLogon, 1, 1, 1, 1, nil)
			},
		},
		{
			name: "EncodeUnified",
			fn: func() error {
				return enc.EncodeUnified(buf, 1, 1, "Method", 1, nil)
			},
		},
		{
			name: "EncodeLegacy",
			fn: func() error {
				return enc.EncodeLegacy(buf, protocol.EMsg_ClientLogon, 1, 1, 1, 1, nil)
			},
		},
		{
			name: "EncodeProtoRaw",
			fn: func() error {
				return enc.EncodeProtoRaw(buf, protocol.EMsg_ClientLogon, 1, 1, 1, 1, nil)
			},
		},
		{
			name: "EncodeUnifiedRaw",
			fn: func() error {
				return enc.EncodeUnifiedRaw(buf, 1, 1, "Method", 1, nil)
			},
		},
		{
			name: "EncodeRaw",
			fn: func() error {
				return enc.EncodeRaw(buf, protocol.EMsg_ChannelEncryptResponse, 1, 1, nil)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn()
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), expectedErr) {
				t.Errorf("expected error containing %q, got: %v", expectedErr, err)
			}
		})
	}
}

func TestBaseEncoder_EncodeMethods(t *testing.T) {
	enc := &BaseEncoder{}

	dummyProto := &emptypb.Empty{}
	dummyRaw := []byte{0xDE, 0xAD, 0xBE, 0xEF}

	t.Run("EncodeProto", func(t *testing.T) {
		buf := &bytes.Buffer{}
		err := enc.EncodeProto(buf, protocol.EMsg_ClientLogon, 123456, 10, 1, 2, dummyProto)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if buf.Len() == 0 {
			t.Error("expected buffer to contain serialized data, but it is empty")
		}
	})

	t.Run("EncodeProto_ClientHello_Quirk", func(t *testing.T) {
		buf := &bytes.Buffer{}
		err := enc.EncodeProto(buf, protocol.EMsg_ClientHello, 123456, 10, 1, 2, dummyProto)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if buf.Len() == 0 {
			t.Error("expected buffer to contain serialized data, but it is empty")
		}
	})

	t.Run("EncodeUnified", func(t *testing.T) {
		buf := &bytes.Buffer{}
		err := enc.EncodeUnified(buf, 123456, 10, "Player.GetGameBadgeLevels#1", 1, dummyProto)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if buf.Len() == 0 {
			t.Error("expected buffer to contain serialized data, but it is empty")
		}
	})

	t.Run("EncodeLegacy", func(t *testing.T) {
		buf := &bytes.Buffer{}
		err := enc.EncodeLegacy(buf, protocol.EMsg_ClientLogon, 123456, 10, 1, 2, dummyRaw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if buf.Len() == 0 {
			t.Error("expected buffer to contain serialized data, but it is empty")
		}
		if !bytes.HasSuffix(buf.Bytes(), dummyRaw) {
			t.Error("expected buffer to end with raw payload")
		}
	})

	t.Run("EncodeProtoRaw", func(t *testing.T) {
		buf := &bytes.Buffer{}
		err := enc.EncodeProtoRaw(buf, protocol.EMsg_ClientLogon, 123456, 10, 1, 2, dummyRaw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if buf.Len() == 0 {
			t.Error("expected buffer to contain serialized data, but it is empty")
		}
		if !bytes.HasSuffix(buf.Bytes(), dummyRaw) {
			t.Error("expected buffer to end with raw payload")
		}
	})

	t.Run("EncodeUnifiedRaw", func(t *testing.T) {
		buf := &bytes.Buffer{}
		err := enc.EncodeUnifiedRaw(buf, 123456, 10, "Player.GetGameBadgeLevels#1", 1, dummyRaw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if buf.Len() == 0 {
			t.Error("expected buffer to contain serialized data, but it is empty")
		}
		if !bytes.HasSuffix(buf.Bytes(), dummyRaw) {
			t.Error("expected buffer to end with raw payload")
		}
	})

	t.Run("EncodeRaw", func(t *testing.T) {
		buf := &bytes.Buffer{}
		err := enc.EncodeRaw(buf, protocol.EMsg_ChannelEncryptResponse, 2, 1, dummyRaw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if buf.Len() == 0 {
			t.Error("expected buffer to contain serialized data, but it is empty")
		}
		if !bytes.HasSuffix(buf.Bytes(), dummyRaw) {
			t.Error("expected buffer to end with raw payload")
		}
	})
}
