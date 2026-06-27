// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package protocol_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

type infiniteZeros struct{}

func (infiniteZeros) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}

	return len(p), nil
}

func TestParsePacket(t *testing.T) {
	t.Parallel()

	t.Run("proto_packet", func(t *testing.T) {
		t.Parallel()

		hdr := protocol.NewMsgHdrProtoBuf(enums.EMsg(100), 1, 1)
		buf := new(bytes.Buffer)
		_ = hdr.SerializeTo(buf)
		buf.WriteString("payload")

		pkt, err := protocol.ParsePacket(buf)
		require.NoError(t, err)
		assert.True(t, pkt.IsProto)
		assert.Equal(t, enums.EMsg(100), pkt.EMsg)
		assert.Equal(t, []byte("payload"), pkt.Payload)
	})

	t.Run("encrypt_handshake_packet", func(t *testing.T) {
		t.Parallel()

		hdr := protocol.NewMsgHdr(enums.EMsg_ChannelEncryptRequest, 0)
		buf := new(bytes.Buffer)
		_ = hdr.SerializeTo(buf)

		pkt, err := protocol.ParsePacket(buf)
		require.NoError(t, err)
		assert.False(t, pkt.IsProto)
		_, ok := pkt.Header.(*protocol.MsgHdr)
		assert.True(t, ok)
	})

	t.Run("extended_packet", func(t *testing.T) {
		t.Parallel()

		hdr := protocol.NewMsgHdrExtended(enums.EMsg(200), 1, 1)
		buf := new(bytes.Buffer)
		_ = hdr.SerializeTo(buf)

		pkt, err := protocol.ParsePacket(buf)
		require.NoError(t, err)

		_, ok := pkt.Header.(*protocol.MsgHdrExtended)
		assert.True(t, ok)
	})

	t.Run("payload_too_large", func(t *testing.T) {
		t.Parallel()

		hdr := protocol.NewMsgHdr(enums.EMsg_ChannelEncryptRequest, 0)
		buf := new(bytes.Buffer)
		_ = hdr.SerializeTo(buf)

		reader := io.MultiReader(buf, io.LimitReader(infiniteZeros{}, protocol.MaxPayloadSize+1))
		_, err := protocol.ParsePacket(reader)
		assert.ErrorIs(t, err, protocol.ErrPayloadTooLarge)
	})

	t.Run("payload_read_error", func(t *testing.T) {
		t.Parallel()

		hdr := protocol.NewMsgHdr(enums.EMsg_ChannelEncryptRequest, 0)
		buf := new(bytes.Buffer)
		_ = hdr.SerializeTo(buf)

		reader := io.MultiReader(buf, &faultyIO{})
		_, err := protocol.ParsePacket(reader)
		assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
	})

	t.Run("read_emsg_error", func(t *testing.T) {
		t.Parallel()

		_, err := protocol.ParsePacket(new(bytes.Buffer))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "read emsg")
	})

	t.Run("deserialize_header_error", func(t *testing.T) {
		t.Parallel()

		buf := bytes.NewReader([]byte{0x01, 0x00, 0x00, 0x00})
		_, err := protocol.ParsePacket(buf)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "deserialize header")
	})
}

func TestPacket_Getters(t *testing.T) {
	t.Parallel()

	t.Run("default_values", func(t *testing.T) {
		t.Parallel()

		pkt := &protocol.Packet{Header: nil}
		assert.Equal(t, protocol.NoJob, pkt.GetSourceJobID())
		assert.Equal(t, protocol.NoJob, pkt.GetTargetJobID())
		assert.Equal(t, uint64(0), pkt.GetSteamID())
		assert.Equal(t, int32(0), pkt.GetSessionID())
		assert.Equal(t, enums.EResult_Invalid, pkt.GetEResult())
	})

	t.Run("protobuf_getters", func(t *testing.T) {
		t.Parallel()

		hdr := protocol.NewMsgHdrProtoBuf(enums.EMsg(1), 123, 456)
		pkt := &protocol.Packet{Header: hdr}
		assert.Equal(t, uint64(123), pkt.GetSteamID())
		assert.Equal(t, int32(456), pkt.GetSessionID())
	})

	t.Run("nil_header_getters", func(t *testing.T) {
		t.Parallel()

		pkt := &protocol.Packet{Header: nil}
		assert.Equal(t, protocol.NoJob, pkt.GetTargetJobID())
		assert.Equal(t, protocol.NoJob, pkt.GetSourceJobID())
	})

	t.Run("eheader_interface_negative", func(t *testing.T) {
		t.Parallel()

		pkt := &protocol.Packet{Header: &protocol.MsgHdr{}}
		assert.Equal(t, enums.EResult_Invalid, pkt.GetEResult())
	})
}

func TestPacket_Context(t *testing.T) {
	t.Parallel()

	t.Run("default_context", func(t *testing.T) {
		t.Parallel()

		pkt := &protocol.Packet{}
		ctx := pkt.Context()
		assert.NotNil(t, ctx)
		assert.Equal(t, context.Background(), ctx)
	})

	t.Run("custom_context", func(t *testing.T) {
		t.Parallel()

		var key struct{}

		customCtx := context.WithValue(t.Context(), &key, "test_val")
		pkt := &protocol.Packet{Ctx: customCtx}
		ctx := pkt.Context()
		assert.Equal(t, customCtx, ctx)
	})
}

func TestContext_TransportType(t *testing.T) {
	t.Parallel()

	t.Run("missing_transport_type", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		_, ok := protocol.GetTransportType(ctx)
		assert.False(t, ok)
	})

	t.Run("with_transport_type", func(t *testing.T) {
		t.Parallel()
		ctx := protocol.WithTransportType(t.Context(), protocol.TransportTCP)
		tt, ok := protocol.GetTransportType(ctx)
		assert.True(t, ok)
		assert.Equal(t, protocol.TransportTCP, tt)
	})
}

func TestPacket_SerializeTo(t *testing.T) {
	t.Parallel()

	t.Run("invalid_header_for_proto", func(t *testing.T) {
		t.Parallel()

		pkt := &protocol.Packet{
			IsProto: true,
			Header:  &protocol.MsgHdr{},
		}
		err := pkt.SerializeTo(io.Discard)
		assert.ErrorIs(t, err, protocol.ErrInvalidHeader)
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		hdr := protocol.NewMsgHdr(enums.EMsg_ChannelEncryptRequest, 0)
		pkt := &protocol.Packet{Header: hdr, Payload: []byte("hi")}
		buf := new(bytes.Buffer)
		err := pkt.SerializeTo(buf)
		assert.NoError(t, err)
		assert.Contains(t, buf.String(), "hi")
	})

	t.Run("serialize_to_header_error", func(t *testing.T) {
		t.Parallel()

		p := &protocol.Packet{
			Header: protocol.NewMsgHdr(enums.EMsg_ChannelEncryptRequest, 0),
		}
		err := p.SerializeTo(&faultyIO{})
		assert.Error(t, err)
	})
}

func TestGCPacket_Roundtrip(t *testing.T) {
	t.Parallel()

	const appID = uint32(440)

	msgType := uint32(1001)
	payload := []byte("gc-data")

	t.Run("proto", func(t *testing.T) {
		t.Parallel()

		p := protocol.NewGCPacket(appID, msgType, payload)
		p.IsProto = true
		p.SourceJobID = 111
		p.TargetJobID = 222

		serialized, err := p.Serialize()
		require.NoError(t, err)

		parsed, err := protocol.ParseGCPacket(appID, msgType|protocol.ProtoMask, serialized)
		require.NoError(t, err)

		assert.Equal(t, msgType, parsed.MsgType)
		assert.True(t, parsed.IsProto)
		assert.Equal(t, uint64(111), parsed.SourceJobID)
		assert.Equal(t, uint64(222), parsed.TargetJobID)
		assert.Equal(t, payload, parsed.Payload)
	})

	t.Run("legacy", func(t *testing.T) {
		t.Parallel()

		p := protocol.NewGCPacket(appID, msgType, payload)
		p.IsProto = false
		p.SourceJobID = 888
		p.TargetJobID = 999

		serialized, err := p.Serialize()
		require.NoError(t, err)

		parsed, err := protocol.ParseGCPacket(appID, msgType, serialized)
		require.NoError(t, err)

		assert.Equal(t, msgType, parsed.MsgType)
		assert.False(t, parsed.IsProto)
		assert.Equal(t, uint64(888), parsed.SourceJobID)
		assert.Equal(t, uint64(999), parsed.TargetJobID)
		assert.Equal(t, payload, parsed.Payload)
	})
}

func TestGCPacket_Errors(t *testing.T) {
	t.Parallel()

	const appID = uint32(440)

	t.Run("parse_proto_inner_msgtype_error", func(t *testing.T) {
		t.Parallel()

		_, err := protocol.ParseGCPacket(appID, protocol.ProtoMask, []byte{1, 2})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "gc: read inner msgtype")
	})

	t.Run("parse_proto_header_len_error", func(t *testing.T) {
		t.Parallel()

		_, err := protocol.ParseGCPacket(appID, protocol.ProtoMask, []byte{1, 2, 3, 4, 5, 6})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "gc: read proto header len")
	})

	t.Run("parse_proto_header_read_error", func(t *testing.T) {
		t.Parallel()

		data := []byte{1, 2, 3, 4, 10, 0, 0, 0, 1, 2}
		_, err := protocol.ParseGCPacket(appID, protocol.ProtoMask, data)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "gc: read proto header")
	})

	t.Run("parse_proto_header_unmarshal_error", func(t *testing.T) {
		t.Parallel()

		data := []byte{1, 2, 3, 4, 3, 0, 0, 0, 0xFF, 0xFF, 0xFF}
		_, err := protocol.ParseGCPacket(appID, protocol.ProtoMask, data)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "gc: unmarshal proto header")
	})

	t.Run("parse_legacy_header_error", func(t *testing.T) {
		t.Parallel()

		_, err := protocol.ParseGCPacket(appID, 0, []byte{1, 2, 3})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "gc: read legacy header")
	})
}

func TestTransportMappingRegistry(t *testing.T) {
	t.Parallel()

	randBytes := make([]byte, 8)
	_, _ = rand.Read(randBytes)
	uniqueKey := "CUSTOM_" + hex.EncodeToString(randBytes)

	assert.Equal(t, protocol.TransportTCP, protocol.MapConnectionToTransport("TCP"))
	assert.Equal(t, protocol.TransportWS, protocol.MapConnectionToTransport("WS"))

	assert.Equal(t, protocol.TransportType(uniqueKey), protocol.MapConnectionToTransport(uniqueKey))

	protocol.RegisterTransportMapping(uniqueKey, protocol.TransportType("WEB"))
	assert.Equal(t, protocol.TransportType("WEB"), protocol.MapConnectionToTransport(uniqueKey))
}
