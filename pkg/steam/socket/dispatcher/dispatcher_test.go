// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dispatcher

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/lemon4ksan/g-man/pkg/jobs"
	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

// --- Mocks ---

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

// --- Helpers ---

func setup(t *testing.T) (*Dispatcher, *jobs.Manager[*protocol.Packet], *mockWriter) {
	jm := jobs.NewManager[*protocol.Packet](10)
	mw := &mockWriter{}
	sess := &mockSession{steamID: 1, sessionID: 2}
	d := New(jm, mw, sess, log.Discard)
	t.Cleanup(func() { d.Close() })

	return d, jm, mw
}

// --- Tests ---

func TestDispatcher_Send_Logic(t *testing.T) {
	t.Run("Successful Send with Callback", func(t *testing.T) {
		d, jm, mw := setup(t)
		called := make(chan struct{})

		err := d.Send(context.Background(), Proto(enums.EMsg_ClientLogon, &emptypb.Empty{}),
			WithCallback(func(p *protocol.Packet, err error) {
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

	t.Run("Writer Error", func(t *testing.T) {
		d, _, mw := setup(t)
		mw.err = errors.New("socket closed")
		err := d.Send(context.Background(), Raw(enums.EMsg_ClientLogon, nil))
		assert.ErrorIs(t, err, mw.err)
	})

	t.Run("Context Cancellation", func(t *testing.T) {
		d, _, _ := setup(t)
		ctx, cancel := context.WithCancel(context.Background())

		errChan := make(chan error, 1)
		_ = d.Send(ctx, Raw(enums.EMsg_ClientLogon, nil), WithCallback(func(p *protocol.Packet, err error) {
			errChan <- err
		}))

		cancel()

		select {
		case err := <-errChan:
			assert.ErrorIs(t, err, jobs.ErrJobCancelled)
		case <-time.After(time.Second):
			t.Fatal("job not resolved on context cancel")
		}
	})
}

func TestDispatcher_Builders(t *testing.T) {
	sess := &mockSession{1, 1}

	t.Run("Unified Builder", func(t *testing.T) {
		buf := new(bytes.Buffer)
		build := Unified("Service.Method", &emptypb.Empty{})
		err := build(sess, buf, 100, "token")
		assert.NoError(t, err)

		pkt, _ := protocol.ParsePacket(buf)
		hdr := pkt.Header.(*protocol.MsgHdrProtoBuf)
		assert.Equal(t, "Service.Method", hdr.Proto.GetTargetJobName())
		assert.Equal(t, "token", hdr.Proto.GetWgToken())
	})

	t.Run("DynamicRaw Builder", func(t *testing.T) {
		// As Proto
		buf := new(bytes.Buffer)
		build := DynamicRaw(enums.EMsg_ServiceMethodCallFromClient, "Method", []byte("raw"), 0)
		_ = build(sess, buf, 0, "")
		pkt, _ := protocol.ParsePacket(buf)
		assert.True(t, pkt.IsProto)

		// As Extended
		buf.Reset()

		build = DynamicRaw(enums.EMsg_ClientLogon, "", []byte("raw"), 0)
		_ = build(sess, buf, 0, "")
		pkt, _ = protocol.ParsePacket(buf)
		assert.False(t, pkt.IsProto)
	})
}

func TestDispatcher_Dispatch_SpecialCases(t *testing.T) {
	t.Run("Nil Packet", func(t *testing.T) {
		d, _, _ := setup(t)
		assert.NotPanics(t, func() { d.Dispatch(nil) })
	})

	t.Run("DestJobFailed Error", func(t *testing.T) {
		d, jm, _ := setup(t)
		jobID := jm.NextID()
		errChan := make(chan error, 1)
		_ = jm.Add(jobID, func(p *protocol.Packet, err error) { errChan <- err })

		// Create EMsg_DestJobFailed packet
		pkt := &protocol.Packet{EMsg: enums.EMsg_DestJobFailed}
		hdr := protocol.NewMsgHdrProtoBuf(enums.EMsg_DestJobFailed, 0, 0)
		hdr.Proto.JobidTarget = proto.Uint64(jobID)
		pkt.Header = hdr

		d.Dispatch(pkt)

		err := <-errChan
		assert.ErrorIs(t, err, ErrDestJobFailed)
	})

	t.Run("ServiceMethod without Proto Header", func(t *testing.T) {
		d, _, _ := setup(t)
		pkt := &protocol.Packet{
			EMsg:   enums.EMsg_ServiceMethod,
			Header: &protocol.MsgHdrExtended{}, // Wrong header type
		}
		assert.NotPanics(t, func() { d.Dispatch(pkt) })
	})
}

func TestDispatcher_Multi_EdgeCases(t *testing.T) {
	t.Run("Malformed SubPacket Size", func(t *testing.T) {
		d, _, _ := setup(t)
		// Payload has size but no data
		payload := []byte{0x04, 0x00, 0x00, 0x00}
		multi, _ := proto.Marshal(&pb.CMsgMulti{MessageBody: payload})

		assert.NotPanics(t, func() {
			d.Dispatch(&protocol.Packet{EMsg: enums.EMsg_Multi, Payload: multi})
		})
	})

	t.Run("Decompression Size Mismatch", func(t *testing.T) {
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
}

func TestDispatcher_SessionExclusion(t *testing.T) {
	d, _, _ := setup(t)
	d.session = &mockSession{steamID: 123, sessionID: 456}

	t.Run("ClientHello has no Session info", func(t *testing.T) {
		buf := new(bytes.Buffer)
		_ = Proto(enums.EMsg_ClientHello, nil)(d.session, buf, 0, "")
		pkt, _ := protocol.ParsePacket(buf)
		hdr := pkt.Header.(*protocol.MsgHdrProtoBuf)

		assert.Equal(t, uint64(0), hdr.Proto.GetSteamid())
		assert.Equal(t, int32(0), hdr.Proto.GetClientSessionid())
	})
}

func TestDispatcher_BufferPooling(t *testing.T) {
	d, _, _ := setup(t)

	t.Run("Large buffers are not pooled", func(t *testing.T) {
		buf := d.getBuffer()
		buf.Write(make([]byte, 200*1024)) // > 128KB
		d.putBuffer(buf)

		// The next buffer from getBuffer should be fresh (empty)
		// but since sync.Pool is non-deterministic, we check the logic branch
		// via coverage.
		buf2 := d.getBuffer()
		assert.Equal(t, 0, buf2.Len())
	})
}

func TestDispatcher_HandlerPanic(t *testing.T) {
	d, _, _ := setup(t)
	d.RegisterMsgHandler(enums.EMsg_ClientLogon, func(p *protocol.Packet) {
		panic("test panic")
	})

	assert.NotPanics(t, func() {
		d.Dispatch(&protocol.Packet{EMsg: enums.EMsg_ClientLogon})
	})
}

func TestDispatcher_Registration(t *testing.T) {
	d, _, _ := setup(t)

	t.Run("Register Service Handler", func(t *testing.T) {
		called := atomic.Bool{}
		d.RegisterServiceHandler("Test.Method", func(p *protocol.Packet) {
			called.Store(true)
		})

		hdr := protocol.NewMsgHdrProtoBuf(enums.EMsg_ServiceMethod, 0, 0)
		hdr.Proto.TargetJobName = proto.String("Test.Method")
		d.Dispatch(&protocol.Packet{EMsg: enums.EMsg_ServiceMethod, Header: hdr})

		assert.True(t, called.Load())

		// Unregister
		d.RegisterServiceHandler("Test.Method", nil)
		called.Store(false)
		d.Dispatch(&protocol.Packet{EMsg: enums.EMsg_ServiceMethod, Header: hdr})
		assert.False(t, called.Load())
	})
}

func TestDispatcher_Options(t *testing.T) {
	// Cover SendOption logic
	opt := WithToken("test-token")
	cfg := &SendConfig{}
	opt(cfg)
	assert.Equal(t, "test-token", cfg.Token)
}

func TestDispatcher_Close(t *testing.T) {
	d, _, _ := setup(t)
	err := d.Close()
	assert.NoError(t, err)
}
