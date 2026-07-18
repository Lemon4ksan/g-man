// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package gc implements a multiplexing gateway to communicate with Steam's Game Coordinators (GC).
package gc

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/lemon4ksan/miyako/bus"
	"github.com/lemon4ksan/miyako/jobs"
	"github.com/lemon4ksan/miyako/log"
	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
)

var gcBufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 1024)
		return &b
	},
}

// ModuleName is the unique string identifier of the GC coordinator module.
const ModuleName string = "gc"

// WithModule returns a [steam.Option] that registers the [Coordinator] module in the client.
func WithModule() steam.Option {
	return steam.WithModule(New())
}

// From retrieves the registered [Coordinator] module instance from the specified [steam.Client].
// It returns nil if the module is not registered or if the client is nil.
func From(c *steam.Client) *Coordinator {
	return steam.GetModule[*Coordinator](c)
}

// Handler processes a specific received [protocol.GCPacket].
// Register GC handlers using the [Coordinator.RegisterGCHandler] method.
type Handler func(packet *protocol.GCPacket)

// MessageEvent occurs when a Game Coordinator message is received and has no registered handler or pending job callback.
// Subscribe to this event using the client's internal bus to handle unmapped GC messages.
type MessageEvent struct {
	bus.BaseEvent
	// Packet is the underlying parsed Game Coordinator message.
	Packet *protocol.GCPacket
}

// Coordinator multiplexes Game Coordinator messages and manages GC-level request-response job cycles.
// It routes outbound payloads, maps incoming packets to pending callbacks, and executes registered handlers.
// Register the coordinator as a client module using [WithModule] or retrieve it via [From].
type Coordinator struct {
	module.Base

	client     service.Doer
	jobManager *jobs.Manager[uint64, *protocol.GCPacket]

	mu         sync.Mutex
	unregFuncs []func()

	handlersMu sync.RWMutex
	gcHandlers map[uint32]map[uint32]Handler
}

// New creates a new [Coordinator] module instance.
func New() *Coordinator {
	return &Coordinator{
		Base:       module.New(ModuleName),
		jobManager: jobs.NewManager[uint64, *protocol.GCPacket](2000),
		gcHandlers: make(map[uint32]map[uint32]Handler),
	}
}

// Init registers global packet handlers for GC network communication.
// It configures callbacks for low-level client-to-GC routing envelopes.
// It will panic if the provided [module.InitContext] argument is nil.
func (c *Coordinator) Init(init module.InitContext) error {
	if err := c.Base.Init(init); err != nil {
		return err
	}

	c.client = init.Service()

	init.RegisterPacketHandler(enums.EMsg_ClientFromGC, c.handleClientFromGC)

	c.unregFuncs = append(c.unregFuncs, func() {
		init.UnregisterPacketHandler(enums.EMsg_ClientFromGC)
	})

	return nil
}

// Close cancels all pending GC jobs, removes registered packet handlers, and releases resources.
// Subsequent calls to Close are safe and will be ignored.
func (c *Coordinator) Close() error {
	c.mu.Lock()
	for _, unreg := range c.unregFuncs {
		unreg()
	}

	c.unregFuncs = nil
	c.mu.Unlock()

	_ = c.jobManager.Close()

	return c.Base.Close()
}

// Send transmits a Protobuf message to the Game Coordinator without expecting a response.
// It returns an error if serialization or transport delivery fails.
// It will panic if either the context or message is nil.
func (c *Coordinator) Send(ctx context.Context, appID, msgType uint32, msg proto.Message) error {
	return c.send(ctx, appID, msgType, msg, nil, nil)
}

// SendRaw transmits a raw byte slice to the Game Coordinator without expecting a response.
// It returns an error if transport delivery fails.
// It will panic if the context is nil.
func (c *Coordinator) SendRaw(ctx context.Context, appID, msgType uint32, payload []byte) error {
	return c.send(ctx, appID, msgType, nil, payload, nil)
}

// Call transmits a Protobuf message and registers a callback to handle the asynchronous response.
// The response is matched to the callback using the GC's internal JobID system.
// It returns an error if the callback cb is nil, if serialization fails, or if job registration fails.
// It will panic if either the context or message is nil.
func (c *Coordinator) Call(
	ctx context.Context,
	appID, msgType uint32,
	msg proto.Message,
	cb jobs.Callback[*protocol.GCPacket],
) error {
	if cb == nil {
		return errors.New("gc: callback is required for Call")
	}

	return c.send(ctx, appID, msgType, msg, nil, cb)
}

// CallRaw transmits a raw byte slice and registers a callback to handle the asynchronous response.
// The response is matched to the callback using the GC's internal JobID system.
// It returns an error if the callback cb is nil or if job registration fails.
// It will panic if the context is nil.
func (c *Coordinator) CallRaw(
	ctx context.Context,
	appID, msgType uint32,
	payload []byte,
	cb jobs.Callback[*protocol.GCPacket],
) error {
	if cb == nil {
		return errors.New("gc: callback is required for Call")
	}

	return c.send(ctx, appID, msgType, nil, payload, cb)
}

// send handles the low-level wrapping of GC messages into Steam CM packets.
func (c *Coordinator) send(
	ctx context.Context,
	appID, msgType uint32,
	msg proto.Message,
	payload []byte,
	cb jobs.Callback[*protocol.GCPacket],
) error {
	var (
		err    error
		bufPtr *[]byte
	)

	if msg != nil {
		bufPtr = gcBufferPool.Get().(*[]byte)
		buf := (*bufPtr)[:0]

		payload, err = proto.MarshalOptions{}.MarshalAppend(buf, msg)
		if err != nil {
			return fmt.Errorf("gc marshal: %w", err)
		}

		defer func() {
			if cap(payload) <= 65536 {
				*bufPtr = payload
				gcBufferPool.Put(bufPtr)
			}
		}()
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

	finalMsgType := msgType
	if msg != nil {
		finalMsgType |= protocol.ProtoMask
	}

	wrapper := &pb.CMsgGCClient{
		Appid:   proto.Uint32(appID),
		Msgtype: proto.Uint32(finalMsgType),
		Payload: gcData,
	}

	c.Logger.Debug("Sending GC Message",
		log.Uint32("appid", appID),
		log.Uint32("msg_type", msgType),
		log.Uint64("job_id", sourceJobID),
	)

	_, err = service.LegacyProto[service.NoResponse](
		ctx, c.client, enums.EMsg_ClientToGC, wrapper,
		service.WithRoutingAppID(appID),
	)
	if err != nil {
		if cb != nil {
			c.jobManager.Resolve(sourceJobID, nil, err)
		}

		return fmt.Errorf("gc transport send: %w", err)
	}

	return nil
}

// RegisterGCHandler registers a custom [Handler] for a specific AppID and MsgType.
// Matching received GC packets are routed to this handler and are not published onto the event bus.
// It will panic upon execution if the provided handler argument is nil.
func (c *Coordinator) RegisterGCHandler(appID, msgType uint32, handler Handler) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()

	if c.gcHandlers == nil {
		c.gcHandlers = make(map[uint32]map[uint32]Handler)
	}

	if c.gcHandlers[appID] == nil {
		c.gcHandlers[appID] = make(map[uint32]Handler)
	}

	c.gcHandlers[appID][msgType] = handler
}

// UnregisterGCHandler removes a registered [Handler] for a specific AppID and MsgType.
// If no handler is registered for the specified keys, the method does nothing.
func (c *Coordinator) UnregisterGCHandler(appID, msgType uint32) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()

	if c.gcHandlers != nil && c.gcHandlers[appID] != nil {
		delete(c.gcHandlers[appID], msgType)
	}
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

	c.handlersMu.RLock()

	var handler Handler
	if c.gcHandlers != nil && c.gcHandlers[gcPacket.AppID] != nil {
		handler = c.gcHandlers[gcPacket.AppID][gcPacket.MsgType]
	}

	c.handlersMu.RUnlock()

	if handler != nil {
		handler(gcPacket)
		return
	}

	c.Bus.Publish(&MessageEvent{
		Packet: gcPacket,
	})
}
