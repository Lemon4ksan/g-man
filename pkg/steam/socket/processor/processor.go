// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package processor

import (
	"bytes"
	"context"
	"runtime"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/network"
)

// Dispatcher defines the interface for routing parsed packets.
type Dispatcher interface {
	Dispatch(packet *protocol.Packet)
}

// Config defines the concurrency and buffering parameters for the processor.
type Config struct {
	WorkerCount int // Number of parallel goroutines processing packets.
	BufferSize  int // Size of the internal channel buffer.
}

// DefaultConfig returns a balanced configuration based on the available CPU cores.
func DefaultConfig() Config {
	workers := runtime.NumCPU()
	if workers < 2 {
		workers = 2
	}

	return Config{
		WorkerCount: workers,
		BufferSize:  1024,
	}
}

// Processor handles the transformation of raw network messages into structured packets.
// It orchestrates a worker pool to handle decompression and parsing asynchronously,
// ensuring the network thread remains unblocked.
type Processor struct {
	cfg    Config
	logger log.Logger
	dist   Dispatcher

	packetCh chan *protocol.Packet

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	isStarted sync.Once
	isStopped sync.Once
}

// New initializes a new Processor with the given configuration and dispatcher.
func New(cfg Config, dist Dispatcher, logger log.Logger) *Processor {
	ctx, cancel := context.WithCancel(context.Background()) // #nosec G118

	return &Processor{
		cfg:      cfg,
		logger:   logger.With(log.Module("proc")),
		dist:     dist,
		packetCh: make(chan *protocol.Packet, cfg.BufferSize),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start spawns the worker pool. This method is idempotent.
func (p *Processor) Start() {
	p.isStarted.Do(func() {
		p.logger.Debug("Starting worker pool", log.Int("workers", p.cfg.WorkerCount))
		p.wg.Add(p.cfg.WorkerCount)

		for range p.cfg.WorkerCount {
			go p.worker()
		}
	})
}

// Stop gracefully shuts down the worker pool, waiting for all pending packets to be processed.
func (p *Processor) Stop() {
	p.isStopped.Do(func() {
		p.logger.Debug("Stopping processor...")
		p.cancel()
		close(p.packetCh)
		p.wg.Wait()
		p.logger.Debug("Processor stopped")
	})
}

// Process takes raw decrypted data from the network and parses it into a packet.
// The packet is then queued for asynchronous dispatching.
// This method implements the network.Handler interface's OnNetMessage.
func (p *Processor) Process(data network.NetMessage) {
	if p.ctx.Err() != nil {
		return
	}

	reader := bytes.NewReader(data)

	packet, err := protocol.ParsePacket(reader)
	if err != nil {
		p.logger.Error("Failed to parse incoming packet", log.Err(err))
		return
	}

	select {
	case p.packetCh <- packet:
		// Packet queued successfully
	case <-p.ctx.Done():
		// Processor is shutting down
	default:
		// Backpressure: Buffer is full. We block until space is available to prevent state desync.
		p.logger.Warn("Packet queue saturated, blocking network thread",
			log.Int("cap", cap(p.packetCh)))

		select {
		case p.packetCh <- packet:
		case <-p.ctx.Done():
		}
	}
}

// OnNetMessage allows Processor to be used directly as a network.Handler.
func (p *Processor) OnNetMessage(msg network.NetMessage) {
	p.Process(msg)
}

// OnNetError logs network errors received from the connector.
func (p *Processor) OnNetError(err error) {
	p.logger.Error("Upstream network error", log.Err(err))
}

// OnNetClose is called when the underlying connection is closed.
func (p *Processor) OnNetClose() {
	p.logger.Debug("Network connection closed")
}

// worker processes packets from the internal queue and feeds them to the dispatcher.
func (p *Processor) worker() {
	defer p.wg.Done()

	for packet := range p.packetCh {
		if packet == nil {
			continue
		}

		p.dist.Dispatch(packet)
	}
}
