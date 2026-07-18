// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package processor_test

import (
	"bytes"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lemon4ksan/miyako/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/processor"
)

type mockDispatcher struct {
	packets chan *protocol.Packet
	count   atomic.Int32
}

func newMockDispatcher() *mockDispatcher {
	return &mockDispatcher{
		packets: make(chan *protocol.Packet, 100),
	}
}

func (m *mockDispatcher) Dispatch(p *protocol.Packet) {
	m.count.Add(1)

	m.packets <- p
}

type panicDispatcher struct {
	called chan struct{}
}

func (p *panicDispatcher) Dispatch(packet *protocol.Packet) {
	close(p.called)
	panic("something went wrong inside dispatcher")
}

func packRaw(eMsg enums.EMsg, targetJob uint64, payload []byte) []byte {
	pkt := &protocol.Packet{
		EMsg:    eMsg,
		IsProto: false,
		Header: &protocol.MsgHdrExtended{
			EMsg:        eMsg,
			TargetJobID: targetJob,
		},
		Payload: payload,
	}
	buf := new(bytes.Buffer)
	_ = pkt.SerializeTo(buf)

	return buf.Bytes()
}

func TestProcessor_DefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := processor.DefaultConfig()
	assert.True(t, cfg.WorkerCount >= 2)
}

func TestProcessor_Lifecycle(t *testing.T) {
	t.Parallel()

	t.Run("lifecycle_start_stop", func(t *testing.T) {
		t.Parallel()

		md := newMockDispatcher()
		cfg := processor.Config{
			WorkerCount: 2,
		}

		input := make(chan *protocol.InboundMessage, 10)
		p := processor.New(cfg, input, md, log.Discard)

		// Idempotent Start
		p.Start()
		p.Start()

		input <- &protocol.InboundMessage{
			Data: packRaw(enums.EMsg_ClientLogon, 0, []byte("Hello World")),
		}

		select {
		case pkt := <-md.packets:
			assert.Equal(t, enums.EMsg_ClientLogon, pkt.EMsg)
		case <-time.After(1 * time.Second):
			t.Fatal("Packet was not dispatched via worker loop")
		}

		// Graceful Stop
		p.Stop()
		p.Stop() // Idempotent Stop

		input <- &protocol.InboundMessage{
			Data: packRaw(enums.EMsg_ClientHeartBeat, 0, nil),
		}

		assert.Equal(t, int32(1), md.count.Load(), "Dispatcher should not have received more packets after stop")
	})

	t.Run("process_on_closed", func(t *testing.T) {
		t.Parallel()

		md := newMockDispatcher()
		p := processor.New(processor.DefaultConfig(), nil, md, log.Discard)
		p.Stop()

		// Process should return immediately since p.ctx.Err() != nil
		p.Process(&protocol.InboundMessage{})
		assert.Equal(t, int32(0), md.count.Load())
	})

	t.Run("worker_exit_on_input_close", func(t *testing.T) {
		t.Parallel()

		md := newMockDispatcher()
		input := make(chan *protocol.InboundMessage)
		p := processor.New(processor.Config{WorkerCount: 1}, input, md, log.Discard)
		p.Start()

		close(input)
		p.Stop() // WaitGroup wait inside Stop unblocks immediately
		assert.Equal(t, int32(0), md.count.Load())
	})
}

func TestProcessor_Concurrency(t *testing.T) {
	t.Parallel()

	t.Run("worker_pool_processing", func(t *testing.T) {
		t.Parallel()

		md := newMockDispatcher()
		cfg := processor.Config{
			WorkerCount: 5,
		}
		p := processor.New(cfg, nil, md, log.Discard)
		p.Start()

		const packetCount = 50
		for i := range packetCount {
			p.Process(&protocol.InboundMessage{
				Data: packRaw(enums.EMsg(i+1000), 0, nil),
			})
		}

		assert.Eventually(t, func() bool {
			return md.count.Load() == int32(packetCount)
		}, time.Second, 10*time.Millisecond)

		p.Stop()
	})

	t.Run("concurrency_safety_stress_test", func(t *testing.T) {
		t.Parallel()

		md := newMockDispatcher()

		go func() {
			for range md.packets {
			}
		}()

		p := processor.New(processor.DefaultConfig(), nil, md, log.Discard)
		p.Start()

		var wg sync.WaitGroup
		for i := range 10 {
			wg.Add(1)

			go func(id int) {
				defer wg.Done()

				for j := range 100 {
					p.Process(&protocol.InboundMessage{
						Data: packRaw(enums.EMsg(id+1000), uint64(id*j), nil),
					})
				}
			}(i)
		}

		wg.Wait()
		p.Stop()

		assert.Equal(t, int32(1000), md.count.Load())
	})
}

func TestProcessor_Errors(t *testing.T) {
	t.Parallel()

	t.Run("parse_failure", func(t *testing.T) {
		t.Parallel()

		md := newMockDispatcher()
		input := make(chan *protocol.InboundMessage, 2)
		p := processor.New(processor.DefaultConfig(), input, md, log.Discard)

		p.Start()
		defer p.Stop()

		// Send garbage that fails protocol.ParsePacket
		input <- &protocol.InboundMessage{
			Data: []byte{0x00}, // Too short for any EMsg
		}

		// FIFO synchronization: send a valid packet after the garbage one
		input <- &protocol.InboundMessage{
			Data: packRaw(enums.EMsg_ClientHeartBeat, 0, nil),
		}

		// Wait for the valid packet to be successfully processed.
		select {
		case pkt := <-md.packets:
			assert.Equal(t, enums.EMsg_ClientHeartBeat, pkt.EMsg)
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout waiting for subsequent valid packet")
		}

		// Verify that the garbage packet was safely ignored
		assert.Equal(t, int32(1), md.count.Load())
	})

	t.Run("worker_panic_recovery", func(t *testing.T) {
		t.Parallel()

		pd := &panicDispatcher{called: make(chan struct{})}
		input := make(chan *protocol.InboundMessage, 1)
		p := processor.New(processor.Config{WorkerCount: 1}, input, pd, log.Discard)

		p.Start()
		defer p.Stop()

		input <- &protocol.InboundMessage{
			Data: packRaw(enums.EMsg_ClientLogon, 0, []byte("Hello")),
		}

		// Wait until panic is triggered and safely recovered
		select {
		case <-pd.called:
		case <-time.After(1 * time.Second):
			t.Fatal("worker did not process message")
		}
	})
}

func TestProcessor_MetadataPropagation(t *testing.T) {
	t.Parallel()

	md := newMockDispatcher()
	cfg := processor.Config{
		WorkerCount: 1,
	}
	p := processor.New(cfg, nil, md, log.Discard)

	p.Start()
	defer p.Stop()

	t.Run("tcp_metadata", func(t *testing.T) {
		data := packRaw(enums.EMsg_ClientHeartBeat, 0, nil)
		p.Process(&protocol.InboundMessage{
			Data:       data,
			ReceivedAt: time.Now(),
			Transport:  protocol.TransportTCP,
		})

		var pkt *protocol.Packet
		select {
		case pkt = <-md.packets:
			assert.Equal(t, enums.EMsg_ClientHeartBeat, pkt.EMsg)
			assert.False(t, pkt.ReceivedAt.IsZero())
			assert.WithinDuration(t, time.Now(), pkt.ReceivedAt, time.Second)

			tr, ok := protocol.GetTransportType(pkt.Context())
			assert.True(t, ok)
			assert.Equal(t, protocol.TransportTCP, tr)

		case <-time.After(100 * time.Millisecond):
			t.Fatal("Timeout waiting for packet")
		}
	})

	t.Run("ws_metadata", func(t *testing.T) {
		protoPkt := &protocol.Packet{
			EMsg:    enums.EMsg_ClientLogon,
			IsProto: true,
			Header:  protocol.NewMsgHdrProtoBuf(enums.EMsg_ClientLogon, 987654321, 42),
			Payload: []byte("payload"),
		}
		buf := new(bytes.Buffer)
		err := protoPkt.SerializeTo(buf)
		require.NoError(t, err)

		protoData := buf.Bytes()
		p.Process(&protocol.InboundMessage{
			Data:       protoData,
			ReceivedAt: time.Now(),
			Transport:  protocol.TransportWS,
		})

		var pkt *protocol.Packet
		select {
		case pkt = <-md.packets:
			assert.Equal(t, enums.EMsg_ClientLogon, pkt.EMsg)
			assert.False(t, pkt.ReceivedAt.IsZero())
			assert.WithinDuration(t, time.Now(), pkt.ReceivedAt, time.Second)

			tr, ok := protocol.GetTransportType(pkt.Context())
			assert.True(t, ok)
			assert.Equal(t, protocol.TransportWS, tr)

		case <-time.After(100 * time.Millisecond):
			t.Fatal("Timeout waiting for protobuf packet")
		}
	})
}

func TestProcessor_Logger(t *testing.T) {
	t.Parallel()

	t.Run("update_logger", func(t *testing.T) {
		t.Parallel()

		md := newMockDispatcher()
		p := processor.New(processor.DefaultConfig(), nil, md, log.Discard)
		p.UpdateLogger(log.Discard)
	})
}
