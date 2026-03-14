// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package coordinator

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/jobs"
	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/gc"
	pb "github.com/lemon4ksan/g-man/pkg/steam/protocol/protobuf"
	"google.golang.org/protobuf/proto"
)

const ModuleName string = "gc"

// GCMessageEvent is triggered when a Game Coordinator message is received.
// and WAS NOT handled by a specific Job callback.
type GCMessageEvent struct {
	bus.BaseEvent
	Packet *gc.Packet
}

func (e *GCMessageEvent) Topic() string { return "gc.message" }

// Coordinator handles the routing of messages between the Client and Game Coordinators.
// It acts as a multiplexer/demultiplexer for AppID-specific traffic.
type Coordinator struct {
	mu         sync.RWMutex
	bus        *bus.Bus
	client     api.LegacyRequester
	logger     log.Logger
	closeFunc  func()
	jobManager *jobs.Manager[*gc.Packet] // Manages GC-specific jobs
}

// New creates a new GC module.
func New() *Coordinator {
	return &Coordinator{
		logger:     log.Discard,
		jobManager: jobs.NewManager[*gc.Packet](2000),
	}
}

func (c *Coordinator) Name() string { return ModuleName }

func (c *Coordinator) Init(init steam.InitContext) error {
	c.bus = init.Bus()
	if c.bus == nil {
		return errors.New("nil bus")
	}
	c.client = init.Proto()
	if c.client == nil {
		return errors.New("nil proto client")
	}
	c.logger = init.Logger().WithModule(ModuleName)

	init.RegisterPacketHandler(protocol.EMsg_ClientFromGC, c.handleClientFromGC)
	c.closeFunc = func() {
		init.UnregisterPacketHandler(protocol.EMsg_ClientFromGC)
	}
	return nil
}

// Start implements the Module interface.
func (c *Coordinator) Start(ctx context.Context) error {
	return nil
}

// Close unregisters the handlers for communicating with coordinator.
func (c *Coordinator) Close() error {
	if c.closeFunc != nil {
		c.closeFunc()
		c.closeFunc = nil
	}
	return nil
}

// Send fires a message to the GC without waiting for a response.
func (c *Coordinator) Send(ctx context.Context, appID uint32, msgType uint32, msg proto.Message) error {
	return c.sendInternal(ctx, appID, msgType, msg, nil, nil)
}

// Send fires a raw payload to the GC without waiting for a response.
func (c *Coordinator) SendRaw(ctx context.Context, appID uint32, msgType uint32, payload []byte) error {
	return c.sendInternal(ctx, appID, msgType, nil, payload, nil)
}

// Call sends a message to the GC and waits for a response with a matching JobID.
func (c *Coordinator) Call(ctx context.Context, appID uint32, msgType uint32, msg proto.Message, cb jobs.Callback[*gc.Packet]) error {
	return c.sendInternal(ctx, appID, msgType, msg, nil, cb)
}

// Call sends a message to the GC and waits for a response with a matching JobID.
func (c *Coordinator) CallRaw(ctx context.Context, appID uint32, msgType uint32, payload []byte, cb jobs.Callback[*gc.Packet]) error {
	return c.sendInternal(ctx, appID, msgType, nil, payload, cb)
}

func (c *Coordinator) sendInternal(ctx context.Context, appID uint32, msgType uint32, msg proto.Message, payload []byte, cb jobs.Callback[*gc.Packet]) error {
	var err error

	if msg != nil {
		payload, err = proto.Marshal(msg)
		if err != nil {
			return fmt.Errorf("gc marshal: %w", err)
		}
	}

	var sourceJobID uint64 = protocol.NoJob
	if cb != nil {
		sourceJobID = c.jobManager.NextID()
		// Register the job BEFORE sending to avoid race conditions where response comes too fast
		err := c.jobManager.Add(sourceJobID, cb, jobs.WithContext[*gc.Packet](ctx))
		if err != nil {
			return fmt.Errorf("gc job track: %w", err)
		}
	}

	packet := &gc.Packet{
		AppID:       appID,
		MsgType:     msgType,
		IsProto:     msg != nil,
		SourceJobID: sourceJobID,
		TargetJobID: protocol.NoJob, // We are initiating, so no target
		Payload:     payload,
	}

	gcData, err := packet.Serialize()
	if err != nil {
		if cb != nil {
			c.jobManager.Resolve(sourceJobID, nil, err)
		}
		return fmt.Errorf("gc serialize: %w", err)
	}

	wrapper := &pb.CMsgGCClient{
		Appid:   proto.Uint32(appID),
		Msgtype: proto.Uint32(msgType | gc.ProtoMask), // Hint for Steam routing
		Payload: gcData,
	}

	c.logger.Debug("Sending GC Message",
		log.Uint32("appid", appID),
		log.Uint32("msg_type", msgType),
		log.Uint64("job_id", sourceJobID),
	)

	err = c.client.CallLegacy(ctx, protocol.EMsg_ClientToGC, wrapper, nil)
	if err != nil {
		if cb != nil {
			c.jobManager.Resolve(sourceJobID, nil, err)
		}
		return fmt.Errorf("gc send: %w", err)
	}

	return nil
}

func (c *Coordinator) handleClientFromGC(packet *protocol.Packet) {
	wrapper := &pb.CMsgGCClient{}
	if err := proto.Unmarshal(packet.Payload, wrapper); err != nil {
		c.logger.Error("Failed to unmarshal ClientFromGC envelope", log.Err(err))
		return
	}

	gcPacket, err := gc.ParsePacket(wrapper.GetAppid(), wrapper.GetMsgtype(), wrapper.GetPayload())
	if err != nil {
		c.logger.Error("Failed to parse inner GC packet", log.Err(err))
		return
	}

	c.logger.Debug("Received GC Message",
		log.Uint32("appid", gcPacket.AppID),
		log.Uint32("msg_type", gcPacket.MsgType),
		log.Uint64("target_job", gcPacket.TargetJobID),
	)

	if gcPacket.TargetJobID != protocol.NoJob {
		if c.jobManager.Resolve(gcPacket.TargetJobID, gcPacket, nil) {
			return
		}
	}

	c.bus.Publish(&GCMessageEvent{
		Packet: gcPacket,
	})
}
