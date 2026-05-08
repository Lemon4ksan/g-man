// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dispatcher_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/jobs"
	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/dispatcher"
)

func setupDispatcher() (*dispatcher.Dispatcher, *jobs.Manager[*protocol.Packet]) {
	jm := jobs.NewManager[*protocol.Packet](10)
	d := dispatcher.New(jm, log.Discard)
	return d, jm
}

func packProto(eMsg enums.EMsg, jobId uint64, payload []byte) *protocol.Packet {
	hdr := protocol.NewMsgHdrProtoBuf(eMsg, 0, 0)
	hdr.Proto.JobidTarget = proto.Uint64(jobId)

	return &protocol.Packet{
		EMsg:    eMsg,
		Header:  hdr,
		Payload: payload,
		IsProto: true,
	}
}

func TestDispatcher_MsgRouting(t *testing.T) {
	d, _ := setupDispatcher()
	called := atomic.Bool{}

	d.RegisterMsgHandler(enums.EMsg_ClientLogon, func(p *protocol.Packet) {
		called.Store(true)
	})

	d.Dispatch(&protocol.Packet{EMsg: enums.EMsg_ClientLogon})
	assert.True(t, called.Load(), "Standard message handler should be called")

	// Unregister
	called.Store(false)
	d.RegisterMsgHandler(enums.EMsg_ClientLogon, nil)
	d.Dispatch(&protocol.Packet{EMsg: enums.EMsg_ClientLogon})
	assert.False(t, called.Load(), "Handler should not be called after removal")
}

func TestDispatcher_ServiceRouting(t *testing.T) {
	d, _ := setupDispatcher()
	called := atomic.Bool{}

	d.RegisterServiceHandler("Player.GetGameBadgeLevels#1", func(p *protocol.Packet) {
		called.Store(true)
	})

	t.Run("Successful Dispatch", func(t *testing.T) {
		hdr := protocol.NewMsgHdrProtoBuf(enums.EMsg_ServiceMethod, 0, 0)
		hdr.Proto.TargetJobName = proto.String("Player.GetGameBadgeLevels#1")

		d.Dispatch(&protocol.Packet{EMsg: enums.EMsg_ServiceMethod, Header: hdr})
		assert.True(t, called.Load())
	})

	t.Run("Non-Proto Header", func(t *testing.T) {
		d.Dispatch(&protocol.Packet{EMsg: enums.EMsg_ServiceMethod, Header: &protocol.MsgHdr{}})
		// Should log warning and return without panic
	})

	t.Run("Unhandled Method", func(t *testing.T) {
		hdr := protocol.NewMsgHdrProtoBuf(enums.EMsg_ServiceMethod, 0, 0)
		hdr.Proto.TargetJobName = proto.String("Unknown.Method")

		assert.NotPanics(t, func() {
			d.Dispatch(&protocol.Packet{EMsg: enums.EMsg_ServiceMethod, Header: hdr})
		})
	})
}

func TestDispatcher_JobRouting(t *testing.T) {
	d, jm := setupDispatcher()
	jobID := jm.NextID()
	resolved := make(chan struct{})

	_ = jm.Add(jobID, func(p *protocol.Packet, err error) {
		close(resolved)
	}, jobs.WithContext[*protocol.Packet](context.Background()))

	pkt := packProto(enums.EMsg_ClientLogOnResponse, jobID, nil)
	d.Dispatch(pkt)

	select {
	case <-resolved:
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Packet with TargetJobID was not routed")
	}
}

func TestDispatcher_HandleMulti(t *testing.T) {
	t.Run("Recursive Unpacking", func(t *testing.T) {
		d, _ := setupDispatcher()
		count := atomic.Int32{}

		d.RegisterMsgHandler(enums.EMsg_ClientLogon, func(p *protocol.Packet) { count.Add(1) })
		d.RegisterMsgHandler(enums.EMsg_ClientPersonaState, func(p *protocol.Packet) { count.Add(1) })

		// Craft nested packets
		sub1 := packProto(enums.EMsg_ClientLogon, 0, nil)
		sub2 := packProto(enums.EMsg_ClientPersonaState, 0, nil)

		payload := new(bytes.Buffer)
		for _, p := range []*protocol.Packet{sub1, sub2} {
			buf := new(bytes.Buffer)
			_ = p.SerializeTo(buf)
			_ = binary.Write(payload, binary.LittleEndian, uint32(buf.Len()))
			payload.Write(buf.Bytes())
		}

		multi, _ := proto.Marshal(&pb.CMsgMulti{MessageBody: payload.Bytes()})
		d.Dispatch(&protocol.Packet{EMsg: enums.EMsg_Multi, Payload: multi})

		assert.Equal(t, int32(2), count.Load(), "All nested packets should be dispatched")
	})

	t.Run("Compressed Multi", func(t *testing.T) {
		d, _ := setupDispatcher()
		called := atomic.Bool{}
		d.RegisterMsgHandler(enums.EMsg_ClientLogon, func(p *protocol.Packet) { called.Store(true) })

		sub := packProto(enums.EMsg_ClientLogon, 0, nil)
		rawSub := new(bytes.Buffer)
		_ = sub.SerializeTo(rawSub)

		nestedPayload := new(bytes.Buffer)
		_ = binary.Write(nestedPayload, binary.LittleEndian, uint32(rawSub.Len()))
		nestedPayload.Write(rawSub.Bytes())

		// Gzip nested payload
		var zipped bytes.Buffer

		zw := gzip.NewWriter(&zipped)
		_, _ = zw.Write(nestedPayload.Bytes())
		zw.Close()

		multi, _ := proto.Marshal(&pb.CMsgMulti{
			SizeUnzipped: proto.Uint32(uint32(nestedPayload.Len())),
			MessageBody:  zipped.Bytes(),
		})

		d.Dispatch(&protocol.Packet{EMsg: enums.EMsg_Multi, Payload: multi})
		assert.True(t, called.Load())
	})

	t.Run("Unmarshal Failure", func(t *testing.T) {
		d, _ := setupDispatcher()
		assert.NotPanics(t, func() {
			d.Dispatch(&protocol.Packet{EMsg: enums.EMsg_Multi, Payload: []byte{0xFF, 0x00}})
		})
	})
}

func TestDispatcher_DecompressionSafety(t *testing.T) {
	d, _ := setupDispatcher()
	d.DecompressionLimit = 1024 // 1KB limit for test

	t.Run("Limit Exceeded", func(t *testing.T) {
		multi, _ := proto.Marshal(&pb.CMsgMulti{
			SizeUnzipped: proto.Uint32(2048), // Over limit
			MessageBody:  []byte("fake-data"),
		})

		d.Dispatch(&protocol.Packet{EMsg: enums.EMsg_Multi, Payload: multi})
		// Error logged, process stops
	})

	t.Run("Zip Bomb or Malformed Gzip", func(t *testing.T) {
		multi, _ := proto.Marshal(&pb.CMsgMulti{
			SizeUnzipped: proto.Uint32(500),
			MessageBody:  []byte("not-gzip"),
		})
		d.Dispatch(&protocol.Packet{EMsg: enums.EMsg_Multi, Payload: multi})
	})
}

func TestDispatcher_PanicRecovery(t *testing.T) {
	d, _ := setupDispatcher()

	d.RegisterMsgHandler(enums.EMsg_ClientLogon, func(p *protocol.Packet) {
		panic("boom")
	})

	assert.NotPanics(t, func() {
		d.Dispatch(&protocol.Packet{EMsg: enums.EMsg_ClientLogon})
	})
}

func TestDispatcher_ClearHandlers(t *testing.T) {
	d, _ := setupDispatcher()
	called := atomic.Bool{}

	d.RegisterMsgHandler(enums.EMsg_ClientLogon, func(p *protocol.Packet) { called.Store(true) })
	d.ClearHandlers()

	d.Dispatch(&protocol.Packet{EMsg: enums.EMsg_ClientLogon})
	assert.False(t, called.Load())
}
