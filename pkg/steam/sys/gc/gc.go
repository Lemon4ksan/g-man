// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gc

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/lemon4ksan/foundation/async/event"
	"github.com/lemon4ksan/foundation/async/logkit"
	"github.com/lemon4ksan/foundation/async/task"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/steam/client"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	pb "github.com/lemon4ksan/g-man/protobuf/steam"
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
func WithModule() client.Option {
	return client.WithModule(New())
}

// From retrieves the Coordinator module instance from the client.
func From(c *client.Client) *Coordinator {
	return client.GetModule[*Coordinator](c)
}

// Handler processes parsed Game Coordinator messages.
type Handler func(packet *protocol.GCPacket)

// MessageEvent is published when an unmapped Game Coordinator message arrives.
type MessageEvent struct {
	event.BaseEvent
	Packet *protocol.GCPacket
}

// Coordinator handles sending, receiving, and asynchronous job matching for Game Coordinator packets.
//
// Thread Safety:
//   - Safe for concurrent use across all public methods.
type Coordinator struct {
	module.Base

	events     Events
	jobManager *task.Manager[uint64, *protocol.GCPacket]

	handlersMu sync.RWMutex
	gcHandlers map[uint32]map[uint32]Handler
}

// New constructs a Coordinator module instance.
func New() *Coordinator {
	return &Coordinator{
		Base:       module.New(ModuleName),
		jobManager: task.NewManager[uint64, *protocol.GCPacket](2000),
		gcHandlers: make(map[uint32]map[uint32]Handler),
	}
}

func (c *Coordinator) Init(init module.InitContext) error {
	if err := c.Base.Init(init); err != nil {
		return err
	}

	c.events = NewEvents(init)
	c.events.OnClientFromGC(c.handleClientFromGC)

	return nil
}

func (c *Coordinator) Close() error {
	if c.events != nil {
		_ = c.events.Close()
	}

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
	cb task.Callback[*protocol.GCPacket],
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
	cb task.Callback[*protocol.GCPacket],
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
	cb task.Callback[*protocol.GCPacket],
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

		err := c.jobManager.Add(sourceJobID, cb, task.WithContext[*protocol.GCPacket](ctx))
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
		logkit.Uint32("appid", appID),
		logkit.Uint32("msg_type", msgType),
		logkit.Uint64("job_id", sourceJobID),
	)

	err = c.events.SendToGC(ctx, wrapper)
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

func (c *Coordinator) handleClientFromGC(wrapper *pb.CMsgGCClient) {
	if wrapper == nil {
		return
	}

	gcPacket, err := protocol.ParseGCPacket(wrapper.GetAppid(), wrapper.GetMsgtype(), wrapper.GetPayload())
	if err != nil {
		c.Logger.Error("Failed to parse inner GC packet", logkit.Err(err))
		return
	}

	c.Logger.Debug("Received GC Message",
		logkit.Uint32("appid", gcPacket.AppID),
		logkit.Uint32("msg_type", gcPacket.MsgType),
		logkit.Uint64("target_job", gcPacket.TargetJobID),
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
