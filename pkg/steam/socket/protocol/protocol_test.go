// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"google.golang.org/protobuf/proto"
)

type infiniteZeros struct{}

func (infiniteZeros) Read(p []byte) (n int, err error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func TestMsgHdr_Roundtrip(t *testing.T) {
	hdr := NewMsgHdr(EMsg_ChannelEncryptRequest, 456)

	if hdr.GetTargetJob() != 456 {
		t.Errorf("Expected TargetJobID 456, got %d", hdr.GetTargetJob())
	}
	if hdr.GetSourceJob() != NoJob {
		t.Errorf("Expected SourceJobID NoJob, got %d", hdr.GetSourceJob())
	}

	buf := new(bytes.Buffer)
	if err := hdr.SerializeTo(buf); err != nil {
		t.Fatalf("SerializeTo failed: %v", err)
	}

	if buf.Len() != 20 {
		t.Fatalf("Expected 20 bytes, got %d", buf.Len())
	}

	// Skip EMsg
	_ = buf.Next(4)

	parsedHdr := &MsgHdr{}
	if err := parsedHdr.Deserialize(buf); err != nil {
		t.Fatalf("Deserialize failed: %v", err)
	}

	if parsedHdr.TargetJobID != hdr.TargetJobID || parsedHdr.SourceJobID != hdr.SourceJobID {
		t.Errorf("Mismatch after deserialize. Got Target: %d, Source: %d", parsedHdr.TargetJobID, parsedHdr.SourceJobID)
	}
}

func TestMsgHdrExtended_DeserializeErrors(t *testing.T) {
	t.Run("InvalidSize", func(t *testing.T) {
		buf := make([]byte, HeaderSizeExtended-4)
		buf[0] = 99 // Wrong size
		hdr := &MsgHdrExtended{}
		err := hdr.Deserialize(bytes.NewReader(buf))
		if !errors.Is(err, ErrInvalidHeader) {
			t.Errorf("Expected ErrInvalidHeader for wrong size, got %v", err)
		}
	})

	t.Run("InvalidVersion", func(t *testing.T) {
		buf := make([]byte, HeaderSizeExtended-4)
		buf[0] = HeaderSizeExtended
		binary.LittleEndian.PutUint16(buf[1:3], 99) // Wrong version
		hdr := &MsgHdrExtended{}
		err := hdr.Deserialize(bytes.NewReader(buf))
		if !errors.Is(err, ErrInvalidHeader) {
			t.Errorf("Expected ErrInvalidHeader for wrong version, got %v", err)
		}
	})

	t.Run("InvalidCanary", func(t *testing.T) {
		buf := make([]byte, HeaderSizeExtended-4)
		buf[0] = HeaderSizeExtended
		binary.LittleEndian.PutUint16(buf[1:3], HeaderVersion)
		buf[19] = 0x00 // Wrong canary
		hdr := &MsgHdrExtended{}
		err := hdr.Deserialize(bytes.NewReader(buf))
		if !errors.Is(err, ErrInvalidHeader) {
			t.Errorf("Expected ErrInvalidHeader for wrong canary, got %v", err)
		}
	})
}

func TestMsgHdrProtoBuf_Roundtrip(t *testing.T) {
	hdr := NewMsgHdrProtoBuf(EMsg(777), 12345, 67890)
	hdr.Proto.JobidSource = proto.Uint64(111)
	hdr.Proto.JobidTarget = proto.Uint64(222)
	hdr.Proto.Eresult = proto.Int32(2)

	if hdr.GetSteamID() != 12345 || hdr.GetSessionID() != 67890 {
		t.Errorf("Getters mismatch")
	}
	if hdr.GetEResult() != EResult(2) {
		t.Errorf("Expected EResult 2, got %v", hdr.GetEResult())
	}

	buf := new(bytes.Buffer)
	if err := hdr.SerializeTo(buf); err != nil {
		t.Fatalf("SerializeTo failed: %v", err)
	}

	_ = buf.Next(4)

	parsedHdr := &MsgHdrProtoBuf{}
	if err := parsedHdr.Deserialize(buf); err != nil {
		t.Fatalf("Deserialize failed: %v", err)
	}

	if parsedHdr.GetSteamID() != hdr.GetSteamID() || parsedHdr.GetTargetJob() != hdr.GetTargetJob() {
		t.Errorf("ProtoBuf mismatch after deserialize")
	}
}

func TestMsgHdrProtoBuf_HeaderTooLarge(t *testing.T) {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint32(MaxHeaderSize+1))

	hdr := &MsgHdrProtoBuf{}
	err := hdr.Deserialize(buf)
	if !errors.Is(err, ErrHeaderTooLarge) {
		t.Errorf("Expected ErrHeaderTooLarge, got %v", err)
	}
}

func TestParsePacket_Errors(t *testing.T) {
	t.Run("EmptyReader", func(t *testing.T) {
		_, err := ParsePacket(new(bytes.Buffer))
		if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("Expected EOF error, got %v", err)
		}
	})

	t.Run("PayloadTooLarge", func(t *testing.T) {
		hdr := NewMsgHdr(EMsg_ChannelEncryptRequest, NoJob)
		buf := new(bytes.Buffer)
		hdr.SerializeTo(buf)

		largeData := io.LimitReader(infiniteZeros{}, int64(MaxPayloadSize+1))
		reader := io.MultiReader(buf, largeData)

		_, err := ParsePacket(reader)
		if !errors.Is(err, ErrPayloadTooLarge) {
			t.Errorf("Expected ErrPayloadTooLarge, got %v", err)
		}
	})

	t.Run("InvalidProtoHeaderSize", func(t *testing.T) {
		buf := new(bytes.Buffer)
		binary.Write(buf, binary.LittleEndian, uint32(100)|ProtoMask) // EMsg с маской
		binary.Write(buf, binary.LittleEndian, uint32(MaxHeaderSize+1))

		_, err := ParsePacket(buf)
		if !errors.Is(err, ErrHeaderTooLarge) {
			t.Errorf("Expected ErrHeaderTooLarge, got %v", err)
		}
	})

	t.Run("InvalidProtoHeader", func(t *testing.T) {
		pkt := &Packet{
			EMsg:    EMsg(100),
			IsProto: true,
			Header:  NewMsgHdr(EMsg(100), NoJob),
		}
		err := pkt.SerializeTo(new(bytes.Buffer))
		if !errors.Is(err, ErrInvalidHeader) {
			t.Errorf("Expected ErrInvalidHeader when IsProto=true but header is MsgHdr")
		}
	})
}

func TestParsePacket(t *testing.T) {
	t.Run("StandardHeader", func(t *testing.T) {
		hdr := NewMsgHdr(EMsg_ChannelEncryptRequest, 100)
		buf := new(bytes.Buffer)
		hdr.SerializeTo(buf)
		buf.WriteString("payload")

		pkt, err := ParsePacket(buf)
		if err != nil {
			t.Fatalf("ParsePacket failed: %v", err)
		}

		if pkt.IsProto {
			t.Errorf("Packet should not be proto")
		}
		if _, ok := pkt.Header.(*MsgHdr); !ok {
			t.Errorf("Expected *MsgHdr header type")
		}
		if string(pkt.Payload) != "payload" {
			t.Errorf("Expected payload 'payload', got %s", string(pkt.Payload))
		}
	})

	t.Run("ProtoBufHeader", func(t *testing.T) {
		hdr := NewMsgHdrProtoBuf(EMsg(999), 11, 22)
		buf := new(bytes.Buffer)
		hdr.SerializeTo(buf)
		buf.WriteString("protopayload")

		pkt, err := ParsePacket(buf)
		if err != nil {
			t.Fatalf("ParsePacket failed: %v", err)
		}

		if !pkt.IsProto {
			t.Errorf("Packet should be proto")
		}
		if pkt.EMsg != EMsg(999) {
			t.Errorf("Expected EMsg 999, got %v", pkt.EMsg)
		}
		if _, ok := pkt.Header.(*MsgHdrProtoBuf); !ok {
			t.Errorf("Expected *MsgHdrProtoBuf header type")
		}
		if string(pkt.Payload) != "protopayload" {
			t.Errorf("Payload mismatch")
		}
	})
}

func TestParsePacket_PayloadTooLarge(t *testing.T) {
	hdr := NewMsgHdr(EMsg_ChannelEncryptRequest, 1)
	buf := new(bytes.Buffer)
	hdr.SerializeTo(buf)

	largePayload := bytes.NewReader(make([]byte, MaxPayloadSize+1))
	reader := io.MultiReader(buf, largePayload)

	_, err := ParsePacket(reader)
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Errorf("Expected ErrPayloadTooLarge, got: %v", err)
	}
}

func TestPacket_Getters(t *testing.T) {
	hdrProto := NewMsgHdrProtoBuf(EMsg(1), 10, 20)
	hdrProto.Proto.Eresult = proto.Int32(5)
	hdrProto.Proto.JobidSource = proto.Uint64(1)
	hdrProto.Proto.JobidTarget = proto.Uint64(2)
	pktProto := &Packet{Header: hdrProto}

	if pktProto.GetSteamID() != 10 {
		t.Errorf("Expected SteamID 10")
	}
	if pktProto.GetSessionID() != 20 {
		t.Errorf("Expected SessionID 20")
	}
	if pktProto.GetEResult() != EResult(5) {
		t.Errorf("Expected EResult 5")
	}
	if pktProto.GetSourceJobID() != 1 || pktProto.GetTargetJobID() != 2 {
		t.Errorf("JobID mismatch")
	}

	hdrStd := NewMsgHdr(EMsg_ChannelEncryptRequest, 99)
	hdrStd.SourceJobID = 88
	pktStd := &Packet{Header: hdrStd}

	if pktStd.GetSteamID() != 0 {
		t.Errorf("Standard header should return 0 for SteamID")
	}
	if pktStd.GetSessionID() != 0 {
		t.Errorf("Standard header should return 0 for SessionID")
	}
	if pktStd.GetEResult() != EResult_Invalid {
		t.Errorf("Standard header should return EResult_Invalid, got %v", pktStd.GetEResult())
	}
	if pktStd.GetSourceJobID() != 88 || pktStd.GetTargetJobID() != 99 {
		t.Errorf("JobID mismatch for std header")
	}
}

func TestPacket_SerializeTo(t *testing.T) {
	t.Run("ProtoSuccess", func(t *testing.T) {
		hdr := NewMsgHdrProtoBuf(EMsg(333), 1, 1)
		pkt := &Packet{
			EMsg:    EMsg(333),
			IsProto: true,
			Header:  hdr,
			Payload: []byte("data"),
		}
		buf := new(bytes.Buffer)
		if err := pkt.SerializeTo(buf); err != nil {
			t.Fatalf("SerializeTo failed: %v", err)
		}

		parsed, err := ParsePacket(buf)
		if err != nil || string(parsed.Payload) != "data" {
			t.Errorf("Failed to parse serialized proto packet: %v", err)
		}
	})

	t.Run("ProtoInvalidHeader", func(t *testing.T) {
		pkt := &Packet{
			EMsg:    EMsg(333),
			IsProto: true,
			Header:  NewMsgHdr(EMsg(333), 1), // Invalid header for proto
		}
		err := pkt.SerializeTo(new(bytes.Buffer))
		if !errors.Is(err, ErrInvalidHeader) {
			t.Errorf("Expected ErrInvalidHeader, got %v", err)
		}
	})

	t.Run("NonProto", func(t *testing.T) {
		hdr := NewMsgHdr(EMsg_ChannelEncryptRequest, 100)
		pkt := &Packet{
			EMsg:    EMsg_ChannelEncryptRequest,
			IsProto: false,
			Header:  hdr,
			Payload: []byte("test"),
		}
		buf := new(bytes.Buffer)
		if err := pkt.SerializeTo(buf); err != nil {
			t.Fatalf("SerializeTo failed: %v", err)
		}

		if buf.Len() == 0 {
			t.Errorf("Buffer should not be empty")
		}
	})
}

func TestPacket_Roundtrip(t *testing.T) {
	tests := []struct {
		name    string
		packet  *Packet
		wantErr bool
	}{
		{
			name: "Standard Handshake Packet",
			packet: &Packet{
				EMsg:    EMsg_ChannelEncryptRequest,
				IsProto: false,
				Header:  NewMsgHdr(EMsg_ChannelEncryptRequest, NoJob),
				Payload: []byte{0x01, 0x02, 0x03, 0x04},
			},
		},
		{
			name: "Legacy Extended Packet",
			packet: &Packet{
				EMsg:    EMsg(555),
				IsProto: false,
				Header:  NewMsgHdrExtended(EMsg(555), 123456, 789),
				Payload: []byte("legacy-payload"),
			},
		},
		{
			name: "Modern Proto Packet",
			packet: &Packet{
				EMsg:    EMsg(1000),
				IsProto: true,
				Header:  NewMsgHdrProtoBuf(EMsg(1000), 999, 888),
				Payload: []byte("proto-payload"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := new(bytes.Buffer)
			err := tt.packet.SerializeTo(buf)
			if (err != nil) != tt.wantErr {
				t.Fatalf("SerializeTo() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}

			parsed, err := ParsePacket(buf)
			if err != nil {
				t.Fatalf("ParsePacket() error = %v", err)
			}

			if parsed.EMsg != tt.packet.EMsg {
				t.Errorf("EMsg mismatch: got %v, want %v", parsed.EMsg, tt.packet.EMsg)
			}
			if parsed.IsProto != tt.packet.IsProto {
				t.Errorf("IsProto mismatch")
			}
			if !bytes.Equal(parsed.Payload, tt.packet.Payload) {
				t.Errorf("Payload mismatch: got %v, want %v", parsed.Payload, tt.packet.Payload)
			}

			if parsed.GetTargetJobID() != tt.packet.GetTargetJobID() {
				t.Errorf("TargetJobID mismatch")
			}
			if parsed.GetSteamID() != tt.packet.GetSteamID() {
				t.Errorf("SteamID mismatch")
			}
		})
	}
}

func TestMsgHdr_Serialization(t *testing.T) {
	hdr := NewMsgHdr(EMsg_ChannelEncryptRequest, 0x1122334455667788)
	hdr.SourceJobID = 0xAABBCCDDEE001122

	buf := new(bytes.Buffer)
	if err := hdr.SerializeTo(buf); err != nil {
		t.Fatalf("SerializeTo failed: %v", err)
	}

	if buf.Len() != 20 {
		t.Errorf("Expected 20 bytes for MsgHdr, got %d", buf.Len())
	}

	data := buf.Bytes()
	emsg := binary.LittleEndian.Uint32(data[0:4])
	target := binary.LittleEndian.Uint64(data[4:12])
	source := binary.LittleEndian.Uint64(data[12:20])

	if EMsg(emsg) != EMsg_ChannelEncryptRequest {
		t.Errorf("EMsg mismatch: expected %v, got %v", EMsg_ChannelEncryptRequest, emsg)
	}
	if target != 0x1122334455667788 {
		t.Errorf("TargetJobID mismatch")
	}
	if source != 0xAABBCCDDEE001122 {
		t.Errorf("SourceJobID mismatch")
	}
}

func TestMsgHdrExtended_Roundtrip(t *testing.T) {
	original := NewMsgHdrExtended(EMsg(500), 76561197960265728, 12345)
	original.SourceJobID = 1
	original.TargetJobID = 2

	buf := new(bytes.Buffer)
	if err := original.SerializeTo(buf); err != nil {
		t.Fatalf("SerializeTo failed: %v", err)
	}

	var rawEMsg uint32
	binary.Read(buf, binary.LittleEndian, &rawEMsg)

	decoded := &MsgHdrExtended{EMsg: EMsg(rawEMsg)}
	if err := decoded.Deserialize(buf); err != nil {
		t.Fatalf("Deserialize failed: %v", err)
	}

	if decoded.SteamID != original.SteamID || decoded.SessionID != original.SessionID {
		t.Errorf("Data mismatch: SteamID %d vs %d", decoded.SteamID, original.SteamID)
	}
	if decoded.HeaderCanary != HeaderCanary {
		t.Errorf("Canary corrupted")
	}
}

func TestMsgHdrProtoBuf_Serialization(t *testing.T) {
	hdr := NewMsgHdrProtoBuf(EMsg(1000), 76561197960265728, 99)
	hdr.Proto.Eresult = proto.Int32(int32(EResult_OK))

	buf := new(bytes.Buffer)
	if err := hdr.SerializeTo(buf); err != nil {
		t.Fatalf("SerializeTo failed: %v", err)
	}

	data := buf.Bytes()
	emsgWithMask := binary.LittleEndian.Uint32(data[0:4])
	if (emsgWithMask & ProtoMask) == 0 {
		t.Errorf("ProtoMask not set in serialized EMsg")
	}

	protoLen := binary.LittleEndian.Uint32(data[4:8])
	if int(protoLen) != len(data)-8 {
		t.Errorf("Header length mismatch: field says %d, actual remaining bytes %d", protoLen, len(data)-8)
	}
}
