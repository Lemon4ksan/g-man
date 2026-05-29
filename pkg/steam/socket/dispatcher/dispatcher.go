// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dispatcher

import (
	"bytes"
	"compress/gzip"
	"context"
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

// Writer defines an interface for sending data through socket.
type Writer interface {
	Send(ctx context.Context, data []byte) error
}

// SessionReader is an interface for accessing fresh steam and session ids
type SessionReader interface {
	SteamID() uint64
	SessionID() int32
}

// SendConfig contains parameters for sending a message.
type SendConfig struct {
	// Callback is invoked asynchronously when a response to this message is received.
	Callback jobs.Callback[*protocol.Packet]
	// Token is an optional WebAPI access token for service method routing.
	Token string
}

// SendOption defines a functional option for configuring a Send operation.
type SendOption func(*SendConfig)

// WithCallback adds a callback to asynchronously wait for a response to the sent packet.
func WithCallback(cb jobs.Callback[*protocol.Packet]) SendOption {
	return func(c *SendConfig) { c.Callback = cb }
}

// WithToken sets an access token for service method calls via the socket.
func WithToken(token string) SendOption {
	return func(c *SendConfig) { c.Token = token }
}

// PayloadBuilder defines how to assemble a binary packet.
type PayloadBuilder func(sess SessionReader, buf *bytes.Buffer, sourceJobID uint64, token string) error

// Proto builds a standard Protobuf-wrapped packet.
func Proto(eMsg enums.EMsg, req proto.Message) PayloadBuilder {
	return func(sess SessionReader, buf *bytes.Buffer, sourceJobID uint64, token string) (err error) {
		pkt := newPacket(sess, eMsg, sourceJobID, true, "", token, 0)
		if req != nil {
			pkt.Payload, err = proto.Marshal(req)
			if err != nil {
				return fmt.Errorf("marshal proto: %w", err)
			}
		}

		return pkt.SerializeTo(buf)
	}
}

// Unified builds a Protobuf packet for Unified Service methods.
func Unified(method string, req proto.Message) PayloadBuilder {
	return func(sess SessionReader, buf *bytes.Buffer, sourceJobID uint64, token string) (err error) {
		pkt := newPacket(sess, enums.EMsg_ServiceMethodCallFromClient, sourceJobID, true, method, token, 0)
		if req != nil {
			pkt.Payload, err = proto.Marshal(req)
			if err != nil {
				return fmt.Errorf("marshal unified proto: %w", err)
			}
		}

		return pkt.SerializeTo(buf)
	}
}

// Raw builds a packet using Extended headers (non-protobuf).
func Raw(eMsg enums.EMsg, payload []byte) PayloadBuilder {
	return func(sess SessionReader, buf *bytes.Buffer, sourceJobID uint64, _ string) error {
		pkt := newPacket(sess, eMsg, sourceJobID, false, "", "", 0)
		pkt.Payload = payload
		return pkt.SerializeTo(buf)
	}
}

// DynamicRaw creates a PayloadBuilder that decides between Protobuf and Extended
// headers based on whether a targetName (Unified Service method) is provided.
// targetName == "" implies a standard (non-unified) message.
func DynamicRaw(eMsg enums.EMsg, targetName string, payload []byte, routingAppID uint32) PayloadBuilder {
	return func(sess SessionReader, buf *bytes.Buffer, sourceJobID uint64, token string) error {
		isProto := targetName != ""

		pkt := newPacket(sess, eMsg, sourceJobID, isProto, targetName, token, routingAppID)
		pkt.Payload = payload

		return pkt.SerializeTo(buf)
	}
}

// DynamicRawProto creates a PayloadBuilder that always uses a Protobuf header.
// Use this for EMsg-based proto messages that are not Unified Service methods.
func DynamicRawProto(eMsg enums.EMsg, payload []byte, routingAppID uint32) PayloadBuilder {
	return func(sess SessionReader, buf *bytes.Buffer, sourceJobID uint64, token string) error {
		pkt := newPacket(sess, eMsg, sourceJobID, true, "", token, routingAppID)
		pkt.Payload = payload
		return pkt.SerializeTo(buf)
	}
}

// Dispatcher coordinates the routing of Steam packets to handlers and job callbacks.
type Dispatcher struct {
	mu sync.RWMutex

	logger     log.Logger
	writer     Writer
	session    SessionReader
	jobManager *jobs.Manager[uint64, *protocol.Packet]

	handlers        map[enums.EMsg]Handler
	serviceHandlers map[string]Handler
	bufferPool      *sync.Pool

	// DecompressionLimit defines the maximum size allowed for unzipped Multi-messages.
	DecompressionLimit int64
}

// New initializes a new packet dispatcher.
func New(
	jm *jobs.Manager[uint64, *protocol.Packet],
	writer Writer,
	session SessionReader,
	logger log.Logger,
) *Dispatcher {
	d := &Dispatcher{
		writer:             writer,
		session:            session,
		logger:             logger.With(log.Component("dispatch")),
		jobManager:         jm,
		handlers:           make(map[enums.EMsg]Handler),
		serviceHandlers:    make(map[string]Handler),
		DecompressionLimit: 100 * 1024 * 1024, // 100MB Default
		bufferPool: &sync.Pool{
			New: func() any {
				return bytes.NewBuffer(make([]byte, 0, 1024))
			},
		},
	}

	return d
}

// UpdateLogger updates the logger used by the dispatcher.
func (d *Dispatcher) UpdateLogger(logger log.Logger) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.logger = logger.With(log.Component("dispatch"))
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

// Send is the primary method for transmitting data. It handles job registration,
// buffer pooling, and builder execution.
//
// It returns an error if packet serialization fails or the underlying socket transmission fails.
func (d *Dispatcher) Send(ctx context.Context, build PayloadBuilder, opts ...SendOption) error {
	cfg := &SendConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	jobID := d.registerJob(ctx, cfg.Callback)

	buf := d.getBuffer()
	defer d.putBuffer(buf)

	if err := build(d.session, buf, jobID, cfg.Token); err != nil {
		d.jobManager.Resolve(jobID, nil, err)
		return err
	}

	return d.writer.Send(ctx, buf.Bytes())
}

// Dispatch routes a single packet. If the packet is an EMsg_Multi, it will be
// unpacked and each sub-packet will be dispatched recursively.
func (d *Dispatcher) Dispatch(packet *protocol.Packet) {
	if packet == nil {
		return
	}

	if packet.Ctx == nil {
		id := "pkt-" + log.GenerateCorrelationID()
		packet.Ctx = log.WithCorrelationID(context.Background(), id)
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

	l := d.getLogger().With(
		log.EMsg(packet.EMsg),
		log.JobID(packet.GetTargetJobID()),
	)

	// Check if this packet is a response to a previously registered Job
	if d.handleJobResponse(packet) {
		l.DebugContext(packet.Context(), "Packet routed to job callback")
		return
	}

	// Route to standard EMsg handlers
	d.mu.RLock()
	handler, ok := d.handlers[packet.EMsg]
	d.mu.RUnlock()

	if ok {
		l.DebugContext(packet.Context(), "Packet routed to handler")
		d.invokeHandler(handler, packet)
	} else {
		l.DebugContext(packet.Context(), "Unhandled message")
	}
}

// Close closes the dispatcher and its job manager.
func (d *Dispatcher) Close() error {
	return d.jobManager.Close()
}

func (d *Dispatcher) invokeHandler(handler Handler, packet *protocol.Packet) {
	defer func() {
		if r := recover(); r != nil {
			d.getLogger().ErrorContext(packet.Context(), "Recovered from handler panic",
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
		d.getLogger().WarnContext(packet.Context(), "Received ServiceMethod with non-protobuf header")
		return
	}

	methodName := header.Proto.GetTargetJobName()

	d.mu.RLock()
	handler, ok := d.serviceHandlers[methodName]
	d.mu.RUnlock()

	if ok {
		d.invokeHandler(handler, packet)
	} else {
		d.getLogger().DebugContext(packet.Context(), "Unhandled ServiceMethod", log.String("method", methodName))
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
		d.getLogger().ErrorContext(packet.Context(), "Failed to unmarshal CMsgMulti", log.Err(err))
		return
	}

	payload := msg.GetMessageBody()
	if size := msg.GetSizeUnzipped(); size > 0 {
		var err error

		payload, err = d.decompressPayload(payload, int64(size))
		if err != nil {
			d.getLogger().ErrorContext(packet.Context(), "Multi-packet decompression failed", log.Err(err))
			return
		}
	}

	reader := bytes.NewReader(payload)
	for reader.Len() > 0 {
		var subSize uint32
		if err := binary.Read(reader, binary.LittleEndian, &subSize); err != nil {
			d.getLogger().WarnContext(packet.Context(), "Failed to read multi-packet sub-size", log.Err(err))
			break
		}

		subPkt, err := protocol.ParsePacket(io.LimitReader(reader, int64(subSize)))
		if err != nil {
			d.getLogger().WarnContext(packet.Context(), "Failed to parse nested multi-packet", log.Err(err))
			continue
		}

		// Propagate parent packet's context (and correlation ID) to sub-packets
		subPkt.Ctx = packet.Context()

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

func (d *Dispatcher) registerJob(ctx context.Context, cb jobs.Callback[*protocol.Packet]) uint64 {
	if cb == nil {
		return protocol.NoJob
	}

	id := d.jobManager.NextID()

	_ = d.jobManager.Add(id, cb, jobs.WithContext[*protocol.Packet](ctx))

	return id
}

func (d *Dispatcher) getBuffer() *bytes.Buffer {
	buf, _ := d.bufferPool.Get().(*bytes.Buffer)
	if buf == nil {
		return new(bytes.Buffer)
	}

	buf.Reset()

	return buf
}

func (d *Dispatcher) putBuffer(buf *bytes.Buffer) {
	if buf.Cap() <= 128*1024 { // Don't pool excessively large buffers
		d.bufferPool.Put(buf)
	}
}

func newPacket(
	sess SessionReader,
	eMsg enums.EMsg,
	jobID uint64,
	isProto bool,
	jobName, token string,
	routingAppID uint32,
) *protocol.Packet {
	var (
		steamID   uint64
		sessionID int32
	)

	// We don't attach session info to ClientHello (Steam requirement)
	if sess != nil && eMsg != enums.EMsg_ClientHello {
		steamID = sess.SteamID()
		sessionID = sess.SessionID()
	}

	pkt := &protocol.Packet{EMsg: eMsg, IsProto: isProto}
	if isProto {
		hdr := protocol.NewMsgHdrProtoBuf(eMsg, steamID, sessionID)
		hdr.Proto.JobidSource = proto.Uint64(jobID)

		if routingAppID > 0 {
			hdr.Proto.RoutingAppid = proto.Uint32(routingAppID)
		}

		if jobName != "" {
			hdr.Proto.TargetJobName = proto.String(jobName)
		}

		if token != "" {
			hdr.Proto.WgToken = proto.String(token)
		}

		pkt.Header = hdr
	} else {
		if eMsg == enums.EMsg_ChannelEncryptRequest ||
			eMsg == enums.EMsg_ChannelEncryptResponse ||
			eMsg == enums.EMsg_ChannelEncryptResult {
			hdr := protocol.NewMsgHdr(eMsg, protocol.NoJob)
			hdr.SourceJobID = jobID
			pkt.Header = hdr
		} else {
			hdr := protocol.NewMsgHdrExtended(eMsg, steamID, sessionID)
			hdr.SourceJobID = jobID
			pkt.Header = hdr
		}
	}

	return pkt
}

func (d *Dispatcher) getLogger() log.Logger {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.logger
}
