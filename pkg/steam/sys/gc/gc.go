// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gc

import (
	"context"
	"fmt"
	"sync"

	"github.com/lemon4ksan/g-man/pkg/bus"
	"github.com/lemon4ksan/g-man/pkg/jobs"
	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"google.golang.org/protobuf/proto"
)

const ModuleName string = "gc"

func WithModule() steam.Option {
	return func(c *steam.Client) {
		c.RegisterModule(New())
	}
}

// GCMessageEvent is triggered when a Game Coordinator message is received.
// and WAS NOT handled by a specific Job callback.
type GCMessageEvent struct {
	bus.BaseEvent
	Packet *protocol.GCPacket
}

// Coordinator acts as a multiplexer for Game Coordinator messages.
// It handles routing based on AppID and manages GC-level request-response cycles.
type Coordinator struct {
	module.Base

	client     service.Doer
	jobManager *jobs.Manager[*protocol.GCPacket]

	mu         sync.Mutex
	unregFuncs []func()
}

// New creates a new Game Coordinator module.
func New() *Coordinator {
	return &Coordinator{
		Base:       module.New(ModuleName),
		jobManager: jobs.NewManager[*protocol.GCPacket](2000),
	}
}

// Init registers global packet handlers for GC communication.
func (c *Coordinator) Init(init module.InitContext) error {
	if err := c.Base.Init(init); err != nil {
		return err
	}

	c.client = init.Service()

	init.RegisterPacketHandler(protocol.EMsg_ClientFromGC, c.handleClientFromGC)

	c.unregFuncs = append(c.unregFuncs, func() {
		init.UnregisterPacketHandler(protocol.EMsg_ClientFromGC)
	})

	return nil
}

// Close ensures all GC jobs are canceled and handlers are removed.
func (c *Coordinator) Close() error {
	c.mu.Lock()
	for _, unreg := range c.unregFuncs {
		unreg()
	}
	c.unregFuncs = nil
	c.mu.Unlock()

	c.jobManager.Close()
	return c.Base.Close()
}

// Send sends a message to a Game Coordinator without expecting a response.
func (c *Coordinator) Send(ctx context.Context, appID uint32, msgType uint32, msg proto.Message) error {
	return c.send(ctx, appID, msgType, msg, nil, nil)
}

// Send fires a raw payload to the GC without waiting for a response.
func (c *Coordinator) SendRaw(ctx context.Context, appID uint32, msgType uint32, payload []byte) error {
	return c.send(ctx, appID, msgType, nil, payload, nil)
}

// Call sends a message to a Game Coordinator and registers a callback for the response.
// The response is matched using the GC's internal JobID system.
func (c *Coordinator) Call(ctx context.Context, appID uint32, msgType uint32, msg proto.Message, cb jobs.Callback[*protocol.GCPacket]) error {
	if cb == nil {
		return fmt.Errorf("gc: callback is required for Call")
	}
	return c.send(ctx, appID, msgType, msg, nil, cb)
}

// Call sends a message to the GC and waits for a response with a matching JobID.
func (c *Coordinator) CallRaw(ctx context.Context, appID uint32, msgType uint32, payload []byte, cb jobs.Callback[*protocol.GCPacket]) error {
	return c.send(ctx, appID, msgType, nil, payload, cb)
}

// send handles the low-level wrapping of GC messages into Steam CM packets.
func (c *Coordinator) send(ctx context.Context, appID uint32, msgType uint32, msg proto.Message, payload []byte, cb jobs.Callback[*protocol.GCPacket]) error {
	var err error

	if msg != nil {
		payload, err = proto.Marshal(msg)
		if err != nil {
			return fmt.Errorf("gc marshal: %w", err)
		}
	}

	sourceJobID := protocol.NoJob
	if cb != nil {
		sourceJobID = c.jobManager.NextID()
		err := c.jobManager.Add(sourceJobID, cb, jobs.WithContext[*protocol.GCPacket](ctx))
		if err != nil {
			return fmt.Errorf("gc job track: %w", err)
		}
	}

	packet := &protocol.GCPacket{
		AppID:       appID,
		MsgType:     msgType,
		IsProto:     msg != nil,
		SourceJobID: sourceJobID,
		TargetJobID: protocol.NoJob,
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
		Msgtype: proto.Uint32(msgType | protocol.ProtoMask),
		Payload: gcData,
	}

	c.Logger.Debug("Sending GC Message",
		log.Uint32("appid", appID),
		log.Uint32("msg_type", msgType),
		log.Uint64("job_id", sourceJobID),
	)

	_, err = service.Legacy[service.NoResponse](ctx, c.client, protocol.EMsg_ClientToGC, wrapper)
	if err != nil {
		if cb != nil {
			c.jobManager.Resolve(sourceJobID, nil, err)
		}
		return fmt.Errorf("gc transport send: %w", err)
	}

	return nil
}

func (c *Coordinator) handleClientFromGC(packet *protocol.Packet) {
	wrapper := &pb.CMsgGCClient{}
	if err := proto.Unmarshal(packet.Payload, wrapper); err != nil {
		c.Logger.Error("Failed to unmarshal ClientFromGC envelope", log.Err(err))
		return
	}

	gcPacket, err := protocol.ParseGCPacket(wrapper.GetAppid(), wrapper.GetMsgtype(), wrapper.GetPayload())
	if err != nil {
		c.Logger.Error("Failed to parse inner GC packet", log.Err(err))
		return
	}

	c.Logger.Debug("Received GC Message",
		log.Uint32("appid", gcPacket.AppID),
		log.Uint32("msg_type", gcPacket.MsgType),
		log.Uint64("target_job", gcPacket.TargetJobID),
	)

	if gcPacket.TargetJobID != protocol.NoJob {
		if c.jobManager.Resolve(gcPacket.TargetJobID, gcPacket, nil) {
			return
		}
	}

	c.Bus.Publish(&GCMessageEvent{
		Packet: gcPacket,
	})
}
