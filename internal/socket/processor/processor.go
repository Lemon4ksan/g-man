// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package processor parses raw socket byte buffers into structured Steam packets using worker pools.
package processor

import (
	"bytes"
	"context"
	"runtime"
	"sync"

	"github.com/lemon4ksan/miyako/log"

	"github.com/lemon4ksan/g-man/internal/framer"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
)

// Dispatcher defines packet dispatching interfaces.
type Dispatcher interface {
	Dispatch(packet *protocol.Packet) bool
}

// DefaultRingBufferCap default capacity for inbound ring buffer.
const DefaultRingBufferCap uint64 = 4096

// Config defines worker concurrency and ring buffer parameters.
type Config struct {
	WorkerCount   int
	RingBufferCap uint64
}

// DefaultConfig builds default processor settings based on CPU core count.
func DefaultConfig() Config {
	return Config{
		WorkerCount:   max(runtime.NumCPU(), 2),
		RingBufferCap: DefaultRingBufferCap,
	}
}

// Processor manages background worker goroutines to parse incoming raw byte frames into structured packets.
type Processor struct {
	cfg    Config
	mu     sync.RWMutex
	logger log.Logger
	dist   Dispatcher

	input  <-chan *protocol.InboundMessage
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	isStarted sync.Once
	isStopped sync.Once
}

// New constructs a Processor.
func New(cfg Config, input <-chan *protocol.InboundMessage, dist Dispatcher, logger log.Logger) *Processor {
	ctx, cancel := context.WithCancel(context.Background()) // #nosec G118

	if input == nil {
		input = make(chan *protocol.InboundMessage, 1024)
	}

	return &Processor{
		ctx:    ctx,
		cancel: cancel,
		cfg:    cfg,
		logger: logger.With(log.Component("proc")),
		input:  input,
		dist:   dist,
	}
}

// UpdateLogger thread-safely sets the logger instance.
func (p *Processor) UpdateLogger(logger log.Logger) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.logger = logger.With(log.Component("proc"))
}

// Start spawns worker pool goroutines. Safe to call multiple times.
func (p *Processor) Start() {
	p.isStarted.Do(func() {
		p.getLogger().Debug("Starting worker pool", log.Int("workers", p.cfg.WorkerCount))

		for range p.cfg.WorkerCount {
			p.wg.Go(p.worker)
		}
	})
}

// Stop gracefully terminates worker pool goroutines.
func (p *Processor) Stop() {
	p.isStopped.Do(func() {
		p.getLogger().Debug("Stopping processor...")
		p.cancel()
		p.wg.Wait()
		p.getLogger().Debug("Processor stopped")
	})
}

// Process parses a raw decrypted message buffer and dispatches the packet.
func (p *Processor) Process(inbound *protocol.InboundMessage) {
	if p.ctx.Err() != nil {
		if inbound.Data != nil {
			framer.ReleaseFrameBuffer(inbound.Data)
		}

		return
	}

	if inbound.Data == nil || len(inbound.Data.B) == 0 {
		return
	}

	defer framer.ReleaseFrameBuffer(inbound.Data)

	reader := bytes.NewReader(inbound.Data.B)

	packet, err := protocol.ParsePacket(reader)
	if err != nil {
		p.getLogger().Error("Failed to parse incoming packet", log.Err(err))
		return
	}

	packet.ReceivedAt = inbound.ReceivedAt
	packet.Transport = inbound.Transport

	if inbound.Transport != "" {
		packet.Ctx = protocol.WithTransportType(p.ctx, inbound.Transport)
	} else {
		packet.Ctx = p.ctx
	}

	if !p.dist.Dispatch(packet) {
		protocol.ReleasePacket(packet)
	}
}

func (p *Processor) worker() {
	for {
		select {
		case <-p.ctx.Done():
			return

		case inbound, ok := <-p.input:
			if !ok {
				return
			}

			func() {
				defer p.recoverPanic()

				p.Process(inbound)
			}()
		}
	}
}

func (p *Processor) recoverPanic() {
	if r := recover(); r != nil {
		p.getLogger().Error("Processor worker recovered from panic", log.Any("panic", r))
	}
}

func (p *Processor) getLogger() log.Logger {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.logger
}
