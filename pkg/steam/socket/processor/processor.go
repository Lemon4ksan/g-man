// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package processor handles the transformation of raw network messages into structured packets.
package processor

import (
	"bytes"
	"context"
	"runtime"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
)

// Dispatcher defines the interface for routing parsed packets.
type Dispatcher interface {
	Dispatch(packet *protocol.Packet)
}

// Config defines the concurrency and buffering parameters for the processor.
type Config struct {
	// WorkerCount is the number of parallel goroutines processing raw packets.
	WorkerCount int
}

// DefaultConfig returns a balanced configuration based on the available CPU cores.
func DefaultConfig() Config {
	return Config{
		WorkerCount: max(runtime.NumCPU(), 2),
	}
}

// Processor handles the transformation of raw network messages into structured packets.
// It orchestrates a worker pool to handle decompression and parsing asynchronously,
// ensuring the network thread remains unblocked.
type Processor struct {
	cfg    Config
	mu     sync.RWMutex
	logger log.Logger
	dist   Dispatcher

	input <-chan *protocol.InboundMessage

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	isStarted sync.Once
	isStopped sync.Once
}

// New initializes a new Processor with the given configuration and dispatcher.
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

// UpdateLogger updates the logger used by the processor.
func (p *Processor) UpdateLogger(logger log.Logger) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.logger = logger.With(log.Component("proc"))
}

// Start spawns the worker pool. This method is idempotent.
func (p *Processor) Start() {
	p.isStarted.Do(func() {
		p.getLogger().Debug("Starting worker pool", log.Int("workers", p.cfg.WorkerCount))

		for range p.cfg.WorkerCount {
			p.wg.Go(p.worker)
		}
	})
}

// Stop gracefully shuts down the worker pool, waiting for all pending packets to be processed.
func (p *Processor) Stop() {
	p.isStopped.Do(func() {
		p.getLogger().Debug("Stopping processor...")
		p.cancel()
		p.wg.Wait()
		p.getLogger().Debug("Processor stopped")
	})
}

// Process takes raw decrypted data from the network and parses it into a packet.
// The packet is then queued for asynchronous dispatching.
func (p *Processor) Process(inbound *protocol.InboundMessage) {
	if p.ctx.Err() != nil {
		return
	}

	reader := bytes.NewReader(inbound.Data)

	packet, err := protocol.ParsePacket(reader)
	if err != nil {
		p.getLogger().Error("Failed to parse incoming packet", log.Err(err))
		return
	}

	packet.ReceivedAt = inbound.ReceivedAt

	// Initialize the packet context deriving it from connection context with a unique Correlation ID
	id := "pkt-" + log.GenerateCorrelationID()
	packet.Ctx = log.WithCorrelationID(p.ctx, id)

	if inbound.Transport != "" {
		packet.Ctx = protocol.WithTransportType(packet.Ctx, inbound.Transport)
	}

	p.dist.Dispatch(packet)
}

// worker processes packets from the internal queue and feeds them to the dispatcher.
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
		p.getLogger().Error("Processor worker recovered from panic",
			log.Any("panic", r))
	}
}

func (p *Processor) getLogger() log.Logger {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.logger
}
