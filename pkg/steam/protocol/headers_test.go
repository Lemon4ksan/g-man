// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package protocol_test

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

type testLimitWriter struct {
	limit   int
	written int
}

func (lw *testLimitWriter) Write(p []byte) (int, error) {
	if lw.written+len(p) > lw.limit {
		return 0, io.ErrShortWrite
	}

	lw.written += len(p)

	return len(p), nil
}

type faultyIO struct{}

func (f *faultyIO) Read(p []byte) (n int, err error) { return 0, io.ErrUnexpectedEOF }
func (f *faultyIO) Write(p []byte) (int, error)      { return 0, io.ErrShortWrite }

func TestHeaders_Getters(t *testing.T) {
	t.Parallel()

	t.Run("msg_hdr", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "Invalid", id.UniverseInvalid.String())
	})

	t.Run("universe_stringer", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "Invalid", id.UniverseInvalid.String())
		assert.Equal(t, "Public", id.UniversePublic.String())
		assert.Equal(t, "Beta", id.UniverseBeta.String())
		assert.Equal(t, "Internal", id.UniverseInternal.String())
		assert.Equal(t, "Dev", id.UniverseDev.String())
		assert.Equal(t, "Universe(100)", id.Universe(100).String())
	})

	t.Run("account_type_stringer", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "Invalid", id.AccountTypeInvalid.String())
		assert.Equal(t, "Individual", id.AccountTypeIndividual.String())
		assert.Equal(t, "Multiseat", id.AccountTypeMultiseat.String())
		assert.Equal(t, "GameServer", id.AccountTypeGameServer.String())
		assert.Equal(t, "AnonGameServer", id.AccountTypeAnonGameServer.String())
		assert.Equal(t, "Pending", id.AccountTypePending.String())
		assert.Equal(t, "ContentServer", id.AccountTypeContentServer.String())
		assert.Equal(t, "Clan", id.AccountTypeClan.String())
		assert.Equal(t, "Chat", id.AccountTypeChat.String())
		assert.Equal(t, "ConsoleUser", id.AccountTypeConsoleUser.String())
		assert.Equal(t, "AnonUser", id.AccountTypeAnonUser.String())
		assert.Equal(t, "AccountType(100)", id.AccountType(100).String())
	})
}

func TestHeaders_Getters_Direct(t *testing.T) {
	t.Parallel()

	t.Run("msg_hdr", func(t *testing.T) {
		t.Parallel()

		h := protocol.NewMsgHdr(enums.EMsg_ChannelEncryptRequest, 1)
		h.SourceJobID = 2
		assert.Equal(t, uint64(1), h.GetTargetJob())
		assert.Equal(t, uint64(2), h.GetSourceJob())
	})

	t.Run("msg_hdr_extended", func(t *testing.T) {
		t.Parallel()

		h := protocol.NewMsgHdrExtended(enums.EMsg(100), 76561198000, 123)
		h.TargetJobID = 1
		h.SourceJobID = 2
		assert.Equal(t, uint64(1), h.GetTargetJob())
		assert.Equal(t, uint64(2), h.GetSourceJob())
		assert.Equal(t, uint64(76561198000), h.GetSteamID())
		assert.Equal(t, int32(123), h.GetSessionID())
	})

	t.Run("msg_hdr_protobuf", func(t *testing.T) {
		t.Parallel()

		h := protocol.NewMsgHdrProtoBuf(enums.EMsg(200), 76561198000, 456)
		h.Proto.JobidTarget = proto.Uint64(1)
		h.Proto.JobidSource = proto.Uint64(2)
		h.Proto.Eresult = proto.Int32(int32(enums.EResult_OK))

		assert.Equal(t, uint64(1), h.GetTargetJob())
		assert.Equal(t, uint64(2), h.GetSourceJob())
		assert.Equal(t, uint64(76561198000), h.GetSteamID())
		assert.Equal(t, int32(456), h.GetSessionID())
		assert.Equal(t, enums.EResult_OK, h.GetEResult())
	})

	t.Run("msg_hdr_protobuf_get_eresult_nil", func(t *testing.T) {
		t.Parallel()

		h := protocol.NewMsgHdrProtoBuf(enums.EMsg_ClientLogOnResponse, 0, 0)
		h.Proto.Eresult = nil
		assert.Equal(t, enums.EResult_OK, h.GetEResult())
	})
}

func TestHeaders_Serialize_Deserialize_Success(t *testing.T) {
	t.Parallel()

	t.Run("msg_hdr_success", func(t *testing.T) {
		t.Parallel()

		h := protocol.NewMsgHdr(enums.EMsg_ChannelEncryptRequest, 12345)
		h.SourceJobID = 67890

		buf := new(bytes.Buffer)
		err := h.SerializeTo(buf)
		assert.NoError(t, err)
		assert.Equal(t, 20, buf.Len())

		var emsg uint32

		err = binary.Read(buf, binary.LittleEndian, &emsg)
		assert.NoError(t, err)
		assert.Equal(t, uint32(enums.EMsg_ChannelEncryptRequest), emsg)

		h2 := &protocol.MsgHdr{EMsg: enums.EMsg_ChannelEncryptRequest}
		err = h2.Deserialize(buf)
		assert.NoError(t, err)
		assert.Equal(t, uint64(12345), h2.GetTargetJob())
		assert.Equal(t, uint64(67890), h2.GetSourceJob())
	})

	t.Run("msg_hdr_extended_success", func(t *testing.T) {
		t.Parallel()

		h := protocol.NewMsgHdrExtended(enums.EMsg_ChannelEncryptResponse, 76561198000, 54321)
		h.TargetJobID = 12345
		h.SourceJobID = 67890

		buf := new(bytes.Buffer)
		err := h.SerializeTo(buf)
		assert.NoError(t, err)
		assert.Equal(t, 36, buf.Len())

		var emsg uint32

		err = binary.Read(buf, binary.LittleEndian, &emsg)
		assert.NoError(t, err)
		assert.Equal(t, uint32(enums.EMsg_ChannelEncryptResponse), emsg)

		h2 := &protocol.MsgHdrExtended{EMsg: enums.EMsg_ChannelEncryptResponse}
		err = h2.Deserialize(buf)
		assert.NoError(t, err)

		assert.Equal(t, byte(36), h2.HeaderSize)
		assert.Equal(t, uint16(2), h2.HeaderVer)
		assert.Equal(t, uint64(12345), h2.GetTargetJob())
		assert.Equal(t, uint64(67890), h2.GetSourceJob())
		assert.Equal(t, byte(0xEF), h2.HeaderCanary)
		assert.Equal(t, uint64(76561198000), h2.GetSteamID())
		assert.Equal(t, int32(54321), h2.GetSessionID())
	})

	t.Run("msg_hdr_protobuf_success", func(t *testing.T) {
		t.Parallel()

		h := protocol.NewMsgHdrProtoBuf(enums.EMsg_ClientLogOnResponse, 76561198000, 98765)
		h.Proto.JobidTarget = proto.Uint64(12345)
		h.Proto.JobidSource = proto.Uint64(67890)

		buf := new(bytes.Buffer)
		err := h.SerializeTo(buf)
		assert.NoError(t, err)

		var emsg uint32

		err = binary.Read(buf, binary.LittleEndian, &emsg)
		assert.NoError(t, err)
		assert.Equal(t, uint32(enums.EMsg_ClientLogOnResponse)|protocol.ProtoMask, emsg)

		h2 := &protocol.MsgHdrProtoBuf{EMsg: enums.EMsg_ClientLogOnResponse}
		err = h2.Deserialize(buf)
		assert.NoError(t, err)

		assert.Equal(t, uint64(12345), h2.GetTargetJob())
		assert.Equal(t, uint64(67890), h2.GetSourceJob())
		assert.Equal(t, uint64(76561198000), h2.GetSteamID())
		assert.Equal(t, int32(98765), h2.GetSessionID())
	})
}

func TestHeaders_Deserialize_Errors(t *testing.T) {
	t.Parallel()

	t.Run("msg_hdr_short_read", func(t *testing.T) {
		t.Parallel()

		h := &protocol.MsgHdr{}
		err := h.Deserialize(bytes.NewReader([]byte{1, 2, 3}))
		assert.Error(t, err)
	})

	t.Run("msg_hdr_extended_short_read", func(t *testing.T) {
		t.Parallel()

		h := &protocol.MsgHdrExtended{}
		err := h.Deserialize(bytes.NewReader([]byte{1}))
		assert.Error(t, err)
	})

	t.Run("msg_hdr_extended_invalid_size", func(t *testing.T) {
		t.Parallel()

		data := make([]byte, 32)
		data[0] = 20
		h := &protocol.MsgHdrExtended{}
		err := h.Deserialize(bytes.NewReader(data))
		assert.ErrorIs(t, err, protocol.ErrInvalidHeader)
	})

	t.Run("msg_hdr_extended_invalid_version", func(t *testing.T) {
		t.Parallel()

		data := make([]byte, 32)
		data[0] = 36
		data[1] = 9
		h := &protocol.MsgHdrExtended{}
		err := h.Deserialize(bytes.NewReader(data))
		assert.ErrorIs(t, err, protocol.ErrInvalidHeader)
	})

	t.Run("msg_hdr_extended_invalid_canary", func(t *testing.T) {
		t.Parallel()

		data := make([]byte, 32)
		data[0] = 36
		data[1] = 2
		data[19] = 0xAA
		h := &protocol.MsgHdrExtended{}
		err := h.Deserialize(bytes.NewReader(data))
		assert.ErrorIs(t, err, protocol.ErrInvalidHeader)
	})

	t.Run("msg_hdr_protobuf_read_len_error", func(t *testing.T) {
		t.Parallel()

		h := &protocol.MsgHdrProtoBuf{}
		err := h.Deserialize(bytes.NewReader([]byte{1}))
		assert.Error(t, err)
	})

	t.Run("msg_hdr_protobuf_read_body_error", func(t *testing.T) {
		t.Parallel()

		h := &protocol.MsgHdrProtoBuf{}
		err := h.Deserialize(bytes.NewReader([]byte{100, 0, 0, 0}))
		assert.Error(t, err)
	})

	t.Run("msg_hdr_protobuf_unmarshal_proto_error", func(t *testing.T) {
		t.Parallel()

		buf := new(bytes.Buffer)
		_ = binary.Write(buf, binary.LittleEndian, uint32(3))
		buf.Write([]byte{0xFF, 0xFF, 0xFF})

		h := &protocol.MsgHdrProtoBuf{}
		err := h.Deserialize(buf)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unmarshal proto hdr")
	})

	t.Run("msg_hdr_protobuf_limit", func(t *testing.T) {
		t.Parallel()

		buf := new(bytes.Buffer)
		_ = binary.Write(buf, binary.LittleEndian, uint32(protocol.MaxHeaderSize+1))

		h := &protocol.MsgHdrProtoBuf{}
		err := h.Deserialize(buf)
		assert.ErrorIs(t, err, protocol.ErrHeaderTooLarge)
	})
}

func TestHeaders_Serialize_Errors(t *testing.T) {
	t.Parallel()

	t.Run("msg_hdr_write_error", func(t *testing.T) {
		t.Parallel()

		h := protocol.NewMsgHdr(enums.EMsg(1), 0)
		err := h.SerializeTo(&faultyIO{})
		assert.ErrorIs(t, err, io.ErrShortWrite)
	})

	t.Run("msg_hdr_extended_write_error", func(t *testing.T) {
		t.Parallel()

		h := protocol.NewMsgHdrExtended(enums.EMsg(1), 0, 0)
		err := h.SerializeTo(&faultyIO{})
		assert.ErrorIs(t, err, io.ErrShortWrite)
	})

	t.Run("msg_hdr_protobuf_write_error", func(t *testing.T) {
		t.Parallel()

		h := protocol.NewMsgHdrProtoBuf(enums.EMsg(1), 0, 0)
		err := h.SerializeTo(&faultyIO{})
		assert.ErrorIs(t, err, io.ErrShortWrite)
	})

	t.Run("msg_hdr_protobuf_second_write_error", func(t *testing.T) {
		t.Parallel()

		h := protocol.NewMsgHdrProtoBuf(enums.EMsg_ClientLogOnResponse, 0, 0)
		lw := &testLimitWriter{limit: 8}
		err := h.SerializeTo(lw)
		assert.ErrorIs(t, err, io.ErrShortWrite)
	})
}
