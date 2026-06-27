// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dispatcher

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lemon4ksan/miyako/jobs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

type mockSession struct {
	steamID   uint64
	sessionID int32
}

func (s *mockSession) SteamID() uint64  { return s.steamID }
func (s *mockSession) SessionID() int32 { return s.sessionID }

type mockWriter struct {
	err error
	got []byte
}

func (m *mockWriter) Send(_ context.Context, data []byte) error {
	m.got = data
	return m.err
}

func setup(t *testing.T) (*Dispatcher, *jobs.Manager[uint64, *protocol.Packet], *mockWriter) {
	t.Helper()

	jm := jobs.NewManager[uint64, *protocol.Packet](10)
	mw := &mockWriter{}
	sess := &mockSession{steamID: 1, sessionID: 2}
	d := New(jm, mw, sess, log.Discard)
	t.Cleanup(func() { d.Close() })

	return d, jm, mw
}

func TestDispatcher_Send_Logic(t *testing.T) {
	t.Parallel()

	t.Run("successful_send_with_callback", func(t *testing.T) {
		t.Parallel()
		d, jm, mw := setup(t)
		called := make(chan struct{})

		err := d.Send(t.Context(), Proto(enums.EMsg_ClientLogon, &emptypb.Empty{}),
			WithCallback(func(ctx context.Context, p *protocol.Packet, err error) {
				close(called)
			}),
		)
		assert.NoError(t, err)
		assert.NotEmpty(t, mw.got)

		// Simulate response
		jobID := jm.NextID() - 1 // The ID generated inside Send
		pkt := &protocol.Packet{EMsg: enums.EMsg_ClientLogOnResponse, IsProto: true}
		hdr := protocol.NewMsgHdrProtoBuf(enums.EMsg_ClientLogOnResponse, 0, 0)
		hdr.Proto.JobidTarget = proto.Uint64(jobID)
		pkt.Header = hdr

		d.Dispatch(pkt)

		select {
		case <-called:
		case <-time.After(time.Second):
			t.Fatal("callback not called")
		}
	})

	t.Run("writer_error", func(t *testing.T) {
		t.Parallel()
		d, _, mw := setup(t)
		mw.err = errors.New("socket closed")
		err := d.Send(t.Context(), Raw(enums.EMsg_ClientLogon, nil))
		assert.ErrorIs(t, err, mw.err)
	})

	t.Run("context_cancellation", func(t *testing.T) {
		t.Parallel()
		d, _, _ := setup(t)
		ctx, cancel := context.WithCancel(t.Context())

		errChan := make(chan error, 1)
		_ = d.Send(
			ctx,
			Raw(enums.EMsg_ClientLogon, nil),
			WithCallback(func(ctx context.Context, p *protocol.Packet, err error) {
				errChan <- err
			}),
		)

		cancel()

		select {
		case err := <-errChan:
			assert.ErrorIs(t, err, jobs.ErrJobCancelled)
		case <-time.After(time.Second):
			t.Fatal("job not resolved on context cancel")
		}
	})

	t.Run("builder_error", func(t *testing.T) {
		t.Parallel()
		d, _, _ := setup(t)
		customErr := errors.New("builder failed")
		badBuilder := func(sess SessionReader, buf *bytes.Buffer, sourceJobID uint64, token string) error {
			return customErr
		}

		err := d.Send(t.Context(), badBuilder)
		assert.ErrorIs(t, err, customErr)
	})
}

func TestDispatcher_Builders(t *testing.T) {
	t.Parallel()

	sess := &mockSession{1, 1}

	t.Run("unified_builder", func(t *testing.T) {
		t.Parallel()

		buf := new(bytes.Buffer)
		build := Unified("Service.Method", &emptypb.Empty{})
		err := build(sess, buf, 100, "token")
		assert.NoError(t, err)

		pkt, _ := protocol.ParsePacket(buf)
		hdr := pkt.Header.(*protocol.MsgHdrProtoBuf)
		assert.Equal(t, "Service.Method", hdr.Proto.GetTargetJobName())
		assert.Equal(t, "token", hdr.Proto.GetWgToken())
	})

	t.Run("dynamic_raw_builder", func(t *testing.T) {
		t.Parallel()
		// As Proto
		buf := new(bytes.Buffer)
		build := DynamicRaw(enums.EMsg_ServiceMethodCallFromClient, "Method", []byte("raw"), 440)
		_ = build(sess, buf, 0, "tok")
		pkt, _ := protocol.ParsePacket(buf)
		assert.True(t, pkt.IsProto)
		hdr := pkt.Header.(*protocol.MsgHdrProtoBuf)
		assert.Equal(t, uint32(440), hdr.Proto.GetRoutingAppid())

		// As Extended
		buf.Reset()

		build = DynamicRaw(enums.EMsg_ClientLogon, "", []byte("raw"), 0)
		_ = build(sess, buf, 0, "")
		pkt, _ = protocol.ParsePacket(buf)
		assert.False(t, pkt.IsProto)
	})

	t.Run("dynamic_raw_proto", func(t *testing.T) {
		t.Parallel()

		buf := new(bytes.Buffer)
		build := DynamicRawProto(enums.EMsg_ClientLogon, []byte("raw"), 440)
		err := build(sess, buf, 100, "token")
		require.NoError(t, err)

		pkt, err := protocol.ParsePacket(buf)
		require.NoError(t, err)
		assert.True(t, pkt.IsProto)
		hdr := pkt.Header.(*protocol.MsgHdrProtoBuf)
		assert.Equal(t, uint32(440), hdr.Proto.GetRoutingAppid())
		assert.Equal(t, "token", hdr.Proto.GetWgToken())
	})
}

func TestDispatcher_Dispatch_SpecialCases(t *testing.T) {
	t.Parallel()

	t.Run("nil_packet", func(t *testing.T) {
		t.Parallel()
		d, _, _ := setup(t)
		assert.NotPanics(t, func() { d.Dispatch(nil) })
	})

	t.Run("dest_job_failed_error", func(t *testing.T) {
		t.Parallel()
		d, jm, _ := setup(t)
		jobID := jm.NextID()
		errChan := make(chan error, 1)
		_ = jm.Add(jobID, func(ctx context.Context, p *protocol.Packet, err error) { errChan <- err })

		// Create EMsg_DestJobFailed packet
		pkt := &protocol.Packet{EMsg: enums.EMsg_DestJobFailed}
		hdr := protocol.NewMsgHdrProtoBuf(enums.EMsg_DestJobFailed, 0, 0)
		hdr.Proto.JobidTarget = proto.Uint64(jobID)
		pkt.Header = hdr

		d.Dispatch(pkt)

		err := <-errChan
		assert.ErrorIs(t, err, ErrDestJobFailed)
	})

	t.Run("service_method_without_proto_header", func(t *testing.T) {
		t.Parallel()
		d, _, _ := setup(t)
		pkt := &protocol.Packet{
			EMsg:   enums.EMsg_ServiceMethod,
			Header: &protocol.MsgHdrExtended{}, // Wrong header type
		}
		assert.NotPanics(t, func() { d.Dispatch(pkt) })
	})

	t.Run("unhandled_service_method", func(t *testing.T) {
		t.Parallel()
		d, _, _ := setup(t)
		hdr := protocol.NewMsgHdrProtoBuf(enums.EMsg_ServiceMethod, 0, 0)
		hdr.Proto.TargetJobName = proto.String("NonExistent.Method")

		assert.NotPanics(t, func() {
			d.Dispatch(&protocol.Packet{EMsg: enums.EMsg_ServiceMethod, Header: hdr})
		})
	})

	t.Run("non_proto_encrypt_header", func(t *testing.T) {
		t.Parallel()

		buf := new(bytes.Buffer)
		build := Raw(enums.EMsg_ChannelEncryptRequest, []byte("data"))
		err := build(&mockSession{1, 2}, buf, 100, "")
		require.NoError(t, err)

		pkt, err := protocol.ParsePacket(buf)
		require.NoError(t, err)
		assert.False(t, pkt.IsProto)
		_, ok := pkt.Header.(*protocol.MsgHdr)
		assert.True(t, ok)
	})
}

func TestDispatcher_Multi_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("malformed_subpacket_size", func(t *testing.T) {
		t.Parallel()
		d, _, _ := setup(t)
		// Payload has size but no data
		payload := []byte{0x04, 0x00, 0x00, 0x00}
		multi, _ := proto.Marshal(&pb.CMsgMulti{MessageBody: payload})

		assert.NotPanics(t, func() {
			d.Dispatch(&protocol.Packet{EMsg: enums.EMsg_Multi, Payload: multi})
		})
	})

	t.Run("decompression_size_mismatch", func(t *testing.T) {
		t.Parallel()
		d, _, _ := setup(t)

		var zipped bytes.Buffer

		zw := gzip.NewWriter(&zipped)
		_, _ = zw.Write([]byte("short"))
		zw.Close()

		multi, _ := proto.Marshal(&pb.CMsgMulti{
			SizeUnzipped: proto.Uint32(100), // Claim 100, but only "short" provided
			MessageBody:  zipped.Bytes(),
		})

		assert.NotPanics(t, func() {
			d.Dispatch(&protocol.Packet{EMsg: enums.EMsg_Multi, Payload: multi})
		})
	})

	t.Run("unmarshal_multi_error", func(t *testing.T) {
		t.Parallel()
		d, _, _ := setup(t)
		pkt := &protocol.Packet{
			EMsg:    enums.EMsg_Multi,
			Payload: []byte{0xFF, 0xFF}, // invalid protobuf
		}
		assert.NotPanics(t, func() {
			d.Dispatch(pkt)
		})
	})

	t.Run("decompression_limit_exceeded", func(t *testing.T) {
		t.Parallel()
		d, _, _ := setup(t)
		d.DecompressionLimit = 5 // low limit

		var zipped bytes.Buffer

		zw := gzip.NewWriter(&zipped)
		_, _ = zw.Write([]byte("too_long_payload"))
		zw.Close()

		multi, _ := proto.Marshal(&pb.CMsgMulti{
			SizeUnzipped: proto.Uint32(10), // > 5
			MessageBody:  zipped.Bytes(),
		})

		assert.NotPanics(t, func() {
			d.Dispatch(&protocol.Packet{EMsg: enums.EMsg_Multi, Payload: multi})
		})
	})

	t.Run("invalid_gzip_headers", func(t *testing.T) {
		t.Parallel()
		d, _, _ := setup(t)
		multi, _ := proto.Marshal(&pb.CMsgMulti{
			SizeUnzipped: proto.Uint32(10),
			MessageBody:  []byte("not_gzip_at_all"),
		})

		assert.NotPanics(t, func() {
			d.Dispatch(&protocol.Packet{EMsg: enums.EMsg_Multi, Payload: multi})
		})
	})

	t.Run("decompression_read_error", func(t *testing.T) {
		t.Parallel()
		d, _, _ := setup(t)

		var zipped bytes.Buffer

		zw := gzip.NewWriter(&zipped)
		_, _ = zw.Write([]byte("short"))
		zw.Close()

		multi, _ := proto.Marshal(&pb.CMsgMulti{
			SizeUnzipped: proto.Uint32(50), // expects 50, but gzip only has "short" (5 bytes)
			MessageBody:  zipped.Bytes(),
		})

		assert.NotPanics(t, func() {
			d.Dispatch(&protocol.Packet{EMsg: enums.EMsg_Multi, Payload: multi})
		})
	})
}

func TestDispatcher_SessionExclusion(t *testing.T) {
	t.Parallel()

	d, _, _ := setup(t)
	d.session = &mockSession{steamID: 123, sessionID: 456}

	t.Run("client_hello_has_no_session_info", func(t *testing.T) {
		t.Parallel()

		buf := new(bytes.Buffer)
		_ = Proto(enums.EMsg_ClientHello, nil)(d.session, buf, 0, "")
		pkt, _ := protocol.ParsePacket(buf)
		hdr := pkt.Header.(*protocol.MsgHdrProtoBuf)

		assert.Equal(t, uint64(0), hdr.Proto.GetSteamid())
		assert.Equal(t, int32(0), hdr.Proto.GetClientSessionid())
	})
}

func TestDispatcher_BufferPooling(t *testing.T) {
	t.Parallel()

	d, _, _ := setup(t)

	t.Run("large_buffers_are_not_pooled", func(t *testing.T) {
		t.Parallel()

		buf := d.getBuffer()
		buf.Write(make([]byte, 200*1024)) // > 128KB
		d.putBuffer(buf)

		buf2 := d.getBuffer()
		assert.Equal(t, 0, buf2.Len())
	})
}

func TestDispatcher_HandlerPanic(t *testing.T) {
	t.Parallel()

	d, _, _ := setup(t)
	d.RegisterMsgHandler(enums.EMsg_ClientLogon, func(p *protocol.Packet) {
		panic("test panic")
	})

	assert.NotPanics(t, func() {
		d.Dispatch(&protocol.Packet{EMsg: enums.EMsg_ClientLogon})
	})
}

func TestDispatcher_Registration(t *testing.T) {
	t.Parallel()

	d, _, _ := setup(t)

	t.Run("register_service_handler", func(t *testing.T) {
		t.Parallel()

		called := atomic.Bool{}
		d.RegisterServiceHandler("Test.Method", func(p *protocol.Packet) {
			called.Store(true)
		})

		hdr := protocol.NewMsgHdrProtoBuf(enums.EMsg_ServiceMethod, 0, 0)
		hdr.Proto.TargetJobName = proto.String("Test.Method")
		d.Dispatch(&protocol.Packet{EMsg: enums.EMsg_ServiceMethod, Header: hdr})

		assert.True(t, called.Load())

		d.RegisterServiceHandler("Test.Method", nil)
		called.Store(false)
		d.Dispatch(&protocol.Packet{EMsg: enums.EMsg_ServiceMethod, Header: hdr})
		assert.False(t, called.Load())
	})
}

func TestDispatcher_Options(t *testing.T) {
	t.Parallel()

	opt := WithToken("test-token")
	cfg := &SendConfig{}
	opt(cfg)
	assert.Equal(t, "test-token", cfg.Token)
}

func TestDispatcher_Close(t *testing.T) {
	t.Parallel()

	d, _, _ := setup(t)
	err := d.Close()
	assert.NoError(t, err)
}

func TestDispatcher_Multi_ReceivedAtPropagation(t *testing.T) {
	t.Parallel()

	d, _, _ := setup(t)

	subPkt := &protocol.Packet{
		EMsg:    enums.EMsg_ClientHeartBeat,
		IsProto: false,
		Header:  &protocol.MsgHdrExtended{EMsg: enums.EMsg_ClientHeartBeat},
		Payload: []byte("heartbeat"),
	}
	subBuf := new(bytes.Buffer)
	err := subPkt.SerializeTo(subBuf)
	assert.NoError(t, err)

	bodyBuf := new(bytes.Buffer)
	err = binary.Write(bodyBuf, binary.LittleEndian, uint32(subBuf.Len()))
	assert.NoError(t, err)
	bodyBuf.Write(subBuf.Bytes())

	multiBytes, err := proto.Marshal(&pb.CMsgMulti{
		MessageBody: bodyBuf.Bytes(),
	})
	assert.NoError(t, err)

	now := time.Now()
	multiPkt := &protocol.Packet{
		EMsg:       enums.EMsg_Multi,
		Payload:    multiBytes,
		ReceivedAt: now,
	}

	var called atomic.Bool
	d.RegisterMsgHandler(enums.EMsg_ClientHeartBeat, func(p *protocol.Packet) {
		assert.Equal(t, now, p.ReceivedAt)
		called.Store(true)
	})

	d.Dispatch(multiPkt)
	assert.True(t, called.Load())
}

func TestDispatcher_Logger(t *testing.T) {
	t.Parallel()

	t.Run("update_logger", func(t *testing.T) {
		t.Parallel()
		d, _, _ := setup(t)
		d.UpdateLogger(log.Discard)
	})
}

func TestDispatcher_ClearHandlers(t *testing.T) {
	t.Parallel()

	t.Run("clear_handlers", func(t *testing.T) {
		t.Parallel()
		d, _, _ := setup(t)
		d.RegisterMsgHandler(enums.EMsg_ClientLogon, func(p *protocol.Packet) {})
		d.ClearHandlers()
		d.mu.RLock()
		assert.Empty(t, d.handlers)
		d.mu.RUnlock()
	})
}
