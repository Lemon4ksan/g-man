// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

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

const ModuleName string = "gc"

// ErrCallbackRequired indicates Call or CallRaw was invoked with a nil callback parameter.
var ErrCallbackRequired = errors.New("gc: callback is required for Call")

// WithModule registers the Coordinator module in the client.
func WithModule() steam.Option {
	return steam.WithModule(New())
}

// From retrieves the Coordinator module instance from the client.
func From(c *steam.Client) *Coordinator {
	return steam.GetModule[*Coordinator](c)
}

// Handler processes parsed Game Coordinator messages.
type Handler func(packet *protocol.GCPacket)

// MessageEvent is published when an unmapped Game Coordinator message arrives.
type MessageEvent struct {
	bus.BaseEvent
	Packet *protocol.GCPacket
}

// Coordinator handles sending, receiving, and asynchronous job matching for Game Coordinator packets.
//
// Thread Safety:
//   - Safe for concurrent use across all public methods.
type Coordinator struct {
	module.Base

	client     service.Doer
	jobManager *jobs.Manager[uint64, *protocol.GCPacket]

	mu         sync.Mutex
	unregFuncs []func()

	handlersMu sync.RWMutex
	gcHandlers map[uint32]map[uint32]Handler
}

// New constructs a Coordinator module instance.
func New() *Coordinator {
	return &Coordinator{
		Base:       module.New(ModuleName),
		jobManager: jobs.NewManager[uint64, *protocol.GCPacket](2000),
		gcHandlers: make(map[uint32]map[uint32]Handler),
	}
}

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

// Send transmits a Protobuf message to a Game Coordinator.
func (c *Coordinator) Send(ctx context.Context, appID, msgType uint32, msg proto.Message) error {
	return c.send(ctx, appID, msgType, msg, nil, nil)
}

// SendRaw transmits raw bytes to a Game Coordinator.
func (c *Coordinator) SendRaw(ctx context.Context, appID, msgType uint32, payload []byte) error {
	return c.send(ctx, appID, msgType, nil, payload, nil)
}

// Call transmits a Protobuf message and registers an asynchronous response callback matched by JobID.
func (c *Coordinator) Call(
	ctx context.Context,
	appID, msgType uint32,
	msg proto.Message,
	cb jobs.Callback[*protocol.GCPacket],
) error {
	if cb == nil {
		return ErrCallbackRequired
	}

	return c.send(ctx, appID, msgType, msg, nil, cb)
}

// CallRaw transmits raw bytes and registers an asynchronous response callback matched by JobID.
func (c *Coordinator) CallRaw(
	ctx context.Context,
	appID, msgType uint32,
	payload []byte,
	cb jobs.Callback[*protocol.GCPacket],
) error {
	if cb == nil {
		return ErrCallbackRequired
	}

	return c.send(ctx, appID, msgType, nil, payload, cb)
}

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

// RegisterGCHandler registers a callback handler for an AppID and MsgType pair.
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

// UnregisterGCHandler removes a registered handler for an AppID and MsgType pair.
func (c *Coordinator) UnregisterGCHandler(appID, msgType uint32) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()

	if c.gcHandlers != nil && c.gcHandlers[appID] != nil {
		delete(c.gcHandlers[appID], msgType)
	}
}

func (c *Coordinator) handleClientFromGC(packet *protocol.Packet) {
	wrapper := &pb.CMsgGCClient{}
	if err := protocol.UnmarshalProto(packet.Payload, wrapper); err != nil {
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
