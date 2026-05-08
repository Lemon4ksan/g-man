// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dispatcher

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/jobs"
	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

var (
	// ErrDecompressionLimit is returned when a Multi-message payload
	// exceeds the safety threshold (default 100MB) to prevent OOM attacks.
	ErrDecompressionLimit = errors.New("dispatcher: decompression limit exceeded")

	// ErrDestJobFailed is returned when the Steam CM indicates a job failure.
	ErrDestJobFailed = errors.New("dispatcher: destination job failed on Steam side")
)

// Handler defines a callback function for processing a fully-parsed Steam packet.
type Handler func(p *protocol.Packet)

// Dispatcher coordinates the routing of Steam packets to handlers and job callbacks.
type Dispatcher struct {
	mu sync.RWMutex

	logger     log.Logger
	jobManager *jobs.Manager[*protocol.Packet]

	handlers        map[enums.EMsg]Handler
	serviceHandlers map[string]Handler

	// DecompressionLimit defines the max size allowed for unzipped Multi-messages.
	DecompressionLimit int64
}

// New initializes a new packet dispatcher.
func New(jm *jobs.Manager[*protocol.Packet], logger log.Logger) *Dispatcher {
	d := &Dispatcher{
		logger:             logger.With(log.Component("dispatch")),
		jobManager:         jm,
		handlers:           make(map[enums.EMsg]Handler),
		serviceHandlers:    make(map[string]Handler),
		DecompressionLimit: 100 * 1024 * 1024, // 100MB Default
	}

	return d
}

// RegisterMsgHandler registers a callback for a specific EMsg.
func (d *Dispatcher) RegisterMsgHandler(eMsg enums.EMsg, handler Handler) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if handler == nil {
		delete(d.handlers, eMsg)
	} else {
		d.handlers[eMsg] = handler
	}
}

// RegisterServiceHandler registers a callback for a specific Unified Service Method.
// Example method: "Player.GetGameBadgeLevels#1".
func (d *Dispatcher) RegisterServiceHandler(method string, handler Handler) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if handler == nil {
		delete(d.serviceHandlers, method)
	} else {
		d.serviceHandlers[method] = handler
	}
}

// ClearHandlers removes all registered message and service handlers.
func (d *Dispatcher) ClearHandlers() {
	d.mu.Lock()
	defer d.mu.Unlock()

	clear(d.handlers)
	clear(d.serviceHandlers)
}

// Dispatch routes a single packet. If the packet is an EMsg_Multi, it will be
// unpacked and each sub-packet will be dispatched recursively.
func (d *Dispatcher) Dispatch(packet *protocol.Packet) {
	if packet == nil {
		return
	}

	// Handle special infrastructure messages first
	switch packet.EMsg {
	case enums.EMsg_Multi:
		d.handleMulti(packet)
		return
	case enums.EMsg_ServiceMethod:
		d.handleService(packet)
		return
	}

	l := d.logger.With(
		log.EMsg(packet.EMsg),
		log.JobID(packet.GetTargetJobID()),
	)

	// Check if this packet is a response to a previously registered Job
	if d.handleJobResponse(packet) {
		l.Debug("Packet routed to job callback")
		return
	}

	// Route to standard EMsg handlers
	d.mu.RLock()
	handler, ok := d.handlers[packet.EMsg]
	d.mu.RUnlock()

	if ok {
		l.Debug("Packet routed to handler")
		d.invokeHandler(handler, packet)
	} else {
		l.Debug("Unhandled message")
	}
}

func (d *Dispatcher) invokeHandler(handler Handler, packet *protocol.Packet) {
	defer func() {
		if r := recover(); r != nil {
			d.logger.Error("Recovered from handler panic",
				log.EMsg(packet.EMsg),
				log.Any("panic", r),
			)
		}
	}()

	handler(packet)
}

func (d *Dispatcher) handleService(packet *protocol.Packet) {
	header, ok := packet.Header.(*protocol.MsgHdrProtoBuf)
	if !ok {
		d.logger.Warn("Received ServiceMethod with non-protobuf header")
		return
	}

	methodName := header.Proto.GetTargetJobName()

	d.mu.RLock()
	handler, ok := d.serviceHandlers[methodName]
	d.mu.RUnlock()

	if ok {
		d.invokeHandler(handler, packet)
	} else {
		d.logger.Debug("Unhandled ServiceMethod", log.String("method", methodName))
	}
}

func (d *Dispatcher) handleJobResponse(packet *protocol.Packet) bool {
	targetID := packet.GetTargetJobID()
	if targetID == protocol.NoJob {
		return false
	}

	var err error
	if packet.EMsg == enums.EMsg_DestJobFailed {
		err = ErrDestJobFailed
	}

	return d.jobManager.Resolve(targetID, packet, err)
}

func (d *Dispatcher) handleMulti(packet *protocol.Packet) {
	msg := &pb.CMsgMulti{}
	if err := proto.Unmarshal(packet.Payload, msg); err != nil {
		d.logger.Error("Failed to unmarshal CMsgMulti", log.Err(err))
		return
	}

	payload := msg.GetMessageBody()
	if size := msg.GetSizeUnzipped(); size > 0 {
		var err error

		payload, err = d.decompressPayload(payload, int64(size))
		if err != nil {
			d.logger.Error("Multi-packet decompression failed", log.Err(err))
			return
		}
	}

	reader := bytes.NewReader(payload)
	for reader.Len() > 0 {
		var subSize uint32
		if err := binary.Read(reader, binary.LittleEndian, &subSize); err != nil {
			d.logger.Warn("Failed to read multi-packet sub-size", log.Err(err))
			break
		}

		subPkt, err := protocol.ParsePacket(io.LimitReader(reader, int64(subSize)))
		if err != nil {
			d.logger.Warn("Failed to parse nested multi-packet", log.Err(err))
			continue
		}

		// Recursively dispatch nested packets
		d.Dispatch(subPkt)
	}
}

func (d *Dispatcher) decompressPayload(data []byte, unzippedSize int64) ([]byte, error) {
	if unzippedSize > d.DecompressionLimit {
		return nil, fmt.Errorf("%w: %d bytes", ErrDecompressionLimit, unzippedSize)
	}

	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gzip reader creation failed: %w", err)
	}
	defer gr.Close()

	out := make([]byte, unzippedSize)
	if _, err := io.ReadFull(gr, out); err != nil {
		return nil, fmt.Errorf("failed to read full decompressed payload: %w", err)
	}

	return out, nil
}
