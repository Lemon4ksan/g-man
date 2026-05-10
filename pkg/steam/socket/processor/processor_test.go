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

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/g-man/pkg/log"
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

func TestProcessor_Lifecycle(t *testing.T) {
	md := newMockDispatcher()
	cfg := processor.Config{
		WorkerCount: 2,
	}

	input := make(chan []byte, 10)
	p := processor.New(cfg, input, md, log.Discard)

	// Idempotent Start
	p.Start()
	p.Start()

	input <- packRaw(enums.EMsg_ClientLogon, 0, []byte("Hello World"))

	select {
	case pkt := <-md.packets:
		assert.Equal(t, enums.EMsg_ClientLogon, pkt.EMsg)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Packet was not dispatched via worker loop")
	}

	// Graceful Stop
	p.Stop()
	p.Stop() // Idempotent Stop

	input <- packRaw(enums.EMsg_ClientHeartBeat, 0, nil)

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(1), md.count.Load(), "Dispatcher should not have received more packets after stop")
}

func TestProcessor_WorkerPool(t *testing.T) {
	md := newMockDispatcher()
	cfg := processor.Config{
		WorkerCount: 5,
	}
	p := processor.New(cfg, nil, md, log.Discard)
	p.Start()

	const packetCount = 50
	for i := range packetCount {
		p.Process(packRaw(enums.EMsg(i+1000), 0, nil))
	}

	assert.Eventually(t, func() bool {
		return md.count.Load() == int32(packetCount)
	}, time.Second, 10*time.Millisecond)

	p.Stop()
}

func TestProcessor_ParseFailure(t *testing.T) {
	md := newMockDispatcher()
	p := processor.New(processor.DefaultConfig(), nil, md, log.Discard)

	p.Start()
	defer p.Stop()

	// Send garbage that fails protocol.ParsePacket
	p.Process([]byte{0x00}) // Too short for any EMsg

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(0), md.count.Load(), "Malformed packets should be dropped before dispatch")
}

func TestProcessor_ConcurrencySafety(t *testing.T) {
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
				p.Process(packRaw(enums.EMsg(id+1000), uint64(id*j), nil))
			}
		}(i)
	}

	wg.Wait()
	p.Stop()

	assert.Equal(t, int32(1000), md.count.Load())
}
