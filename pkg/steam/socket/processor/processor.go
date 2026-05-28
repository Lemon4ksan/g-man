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
	"github.com/lemon4ksan/g-man/pkg/network"
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
	logger log.Logger
	dist   Dispatcher

	input <-chan []byte

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	isStarted sync.Once
	isStopped sync.Once
}

// New initializes a new Processor with the given configuration and dispatcher.
func New(cfg Config, input <-chan []byte, dist Dispatcher, logger log.Logger) *Processor {
	ctx, cancel := context.WithCancel(context.Background()) // #nosec G118

	if input == nil {
		input = make(chan []byte, 1024)
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

// Start spawns the worker pool. This method is idempotent.
func (p *Processor) Start() {
	p.isStarted.Do(func() {
		p.logger.Debug("Starting worker pool", log.Int("workers", p.cfg.WorkerCount))

		for range p.cfg.WorkerCount {
			p.wg.Go(p.worker)
		}
	})
}

// Stop gracefully shuts down the worker pool, waiting for all pending packets to be processed.
func (p *Processor) Stop() {
	p.isStopped.Do(func() {
		p.logger.Debug("Stopping processor...")
		p.cancel()
		p.wg.Wait()
		p.logger.Debug("Processor stopped")
	})
}

// Process takes raw decrypted data from the network and parses it into a packet.
// The packet is then queued for asynchronous dispatching.
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

	// Initialize the packet context deriving it from connection context with a unique Correlation ID
	id := "pkt-" + log.GenerateCorrelationID()
	packet.Ctx = log.WithCorrelationID(p.ctx, id)

	p.dist.Dispatch(packet)
}

// worker processes packets from the internal queue and feeds them to the dispatcher.
func (p *Processor) worker() {
	for {
		select {
		case <-p.ctx.Done():
			return

		case data, ok := <-p.input:
			if !ok {
				return
			}

			func() {
				defer p.recoverPanic()

				p.Process(data)
			}()
		}
	}
}

func (p *Processor) recoverPanic() {
	if r := recover(); r != nil {
		p.logger.Error("Processor worker recovered from panic",
			log.Any("panic", r))
	}
}
