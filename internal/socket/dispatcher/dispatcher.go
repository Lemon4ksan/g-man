// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package dispatcher routes incoming Steam packets to registered EMsg handlers, service methods, and job callbacks.
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
	"sync/atomic"

	"github.com/lemon4ksan/miyako/jobs"
	"github.com/lemon4ksan/miyako/log"
	"google.golang.org/protobuf/proto"

	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

var (
	// ErrDecompressionLimit is returned when an unzipped Multi-message payload exceeds safe memory limits.
	ErrDecompressionLimit = errors.New("dispatcher: decompression limit exceeded")
	// ErrDestJobFailed is returned when Steam CM reports a destination job failure.
	ErrDestJobFailed = errors.New("dispatcher: destination job failed on steam side")
)

// Handler processes a fully parsed Steam packet.
type Handler func(p *protocol.Packet)

// Writer writes raw binary bytes to the network socket.
type Writer interface {
	Send(ctx context.Context, data []byte) error
}

// SessionReader reads SteamID and SessionID values.
type SessionReader interface {
	SteamID() uint64
	SessionID() int32
}

// SendConfig specifies parameters for sending outbound socket messages.
type SendConfig struct {
	Callback jobs.Callback[*protocol.Packet]
	Token    string
}

// SendOption configures a Send operation.
type SendOption func(*SendConfig)

// WithCallback assigns an asynchronous job response callback.
func WithCallback(cb jobs.Callback[*protocol.Packet]) SendOption {
	return func(c *SendConfig) { c.Callback = cb }
}

// WithToken configures an access token for service call routing.
func WithToken(token string) SendOption {
	return func(c *SendConfig) { c.Token = token }
}

// PayloadBuilder serializes a binary packet into a destination buffer.
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

// Raw builds a packet with Extended headers.
func Raw(eMsg enums.EMsg, payload []byte) PayloadBuilder {
	return func(sess SessionReader, buf *bytes.Buffer, sourceJobID uint64, _ string) error {
		pkt := newPacket(sess, eMsg, sourceJobID, false, "", "", 0)
		pkt.Payload = payload

		return pkt.SerializeTo(buf)
	}
}

// DynamicRaw builds a packet selecting Protobuf or Extended headers based on targetName presence.
func DynamicRaw(eMsg enums.EMsg, targetName string, payload []byte, routingAppID uint32) PayloadBuilder {
	return func(sess SessionReader, buf *bytes.Buffer, sourceJobID uint64, token string) error {
		isProto := targetName != ""
		pkt := newPacket(sess, eMsg, sourceJobID, isProto, targetName, token, routingAppID)
		pkt.Payload = payload

		return pkt.SerializeTo(buf)
	}
}

// DynamicRawProto builds a packet using Protobuf headers for non-unified EMsg messages.
func DynamicRawProto(eMsg enums.EMsg, payload []byte, routingAppID uint32) PayloadBuilder {
	return func(sess SessionReader, buf *bytes.Buffer, sourceJobID uint64, token string) error {
		pkt := newPacket(sess, eMsg, sourceJobID, true, "", token, routingAppID)
		pkt.Payload = payload

		return pkt.SerializeTo(buf)
	}
}

const (
	maxDenseEMsg     = 16384
	serviceTableSize = 256
)

type serviceEntry struct {
	hash    uint64
	method  string
	handler atomic.Pointer[Handler]
}

func fastHashString(s string) uint64 {
	var h uint64 = 14695981039346656037
	for i := range len(s) {
		h ^= uint64(s[i])
		h *= 1099511628211
	}

	return h
}

// Dispatcher manages packet handlers, service method tables, and job callbacks.
type Dispatcher struct {
	mu sync.RWMutex

	logger     log.Logger
	writer     Writer
	session    SessionReader
	jobManager *jobs.Manager[uint64, *protocol.Packet]

	denseHandlers     [maxDenseEMsg]atomic.Pointer[Handler]
	sparseHandlers    map[enums.EMsg]Handler
	denseServiceTable [serviceTableSize]serviceEntry
	bufferPool        *sync.Pool

	DecompressionLimit int64
}

// New initializes a Dispatcher.
func New(
	jm *jobs.Manager[uint64, *protocol.Packet],
	writer Writer,
	session SessionReader,
	logger log.Logger,
) *Dispatcher {
	return &Dispatcher{
		writer:             writer,
		session:            session,
		logger:             logger.With(log.Component("dispatch")),
		jobManager:         jm,
		sparseHandlers:     make(map[enums.EMsg]Handler),
		DecompressionLimit: 100 * 1024 * 1024,
		bufferPool: &sync.Pool{
			New: func() any {
				return bytes.NewBuffer(make([]byte, 0, 1024))
			},
		},
	}
}

// UpdateLogger updates the logger instance.
func (d *Dispatcher) UpdateLogger(logger log.Logger) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.logger = logger.With(log.Component("dispatch"))
}

// RegisterMsgHandler registers a callback handler for an EMsg.
func (d *Dispatcher) RegisterMsgHandler(eMsg enums.EMsg, handler Handler) {
	if uint32(eMsg) < maxDenseEMsg {
		if handler == nil {
			d.denseHandlers[eMsg].Store(nil)
			return
		}

		hPtr := new(Handler)
		*hPtr = handler
		d.denseHandlers[eMsg].Store(hPtr)

		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if handler == nil {
		delete(d.sparseHandlers, eMsg)
	} else {
		d.sparseHandlers[eMsg] = handler
	}
}

// RegisterServiceHandler registers a callback handler for a Unified Service Method.
func (d *Dispatcher) RegisterServiceHandler(method string, handler Handler) {
	hash := fastHashString(method)
	idx := hash & (serviceTableSize - 1)

	for i := range serviceTableSize {
		slotIdx := (idx + uint64(i)) & (serviceTableSize - 1)
		entry := &d.denseServiceTable[slotIdx]

		if entry.hash == 0 || entry.hash == hash {
			entry.hash = hash
			entry.method = method

			if handler == nil {
				entry.handler.Store(nil)
			} else {
				hPtr := new(Handler)
				*hPtr = handler
				entry.handler.Store(hPtr)
			}

			return
		}
	}
}

// ClearHandlers removes all registered message and service handlers.
func (d *Dispatcher) ClearHandlers() {
	d.mu.Lock()
	defer d.mu.Unlock()

	clear(d.sparseHandlers)

	for i := range d.denseHandlers {
		d.denseHandlers[i].Store(nil)
	}
}

// Send serializes and transmits a packet, registering a response job if requested.
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

// Dispatch routes an incoming packet to matching jobs, EMsg handlers, or service handlers.
func (d *Dispatcher) Dispatch(packet *protocol.Packet) bool {
	if packet == nil {
		return false
	}

	if packet.Ctx == nil {
		packet.Ctx = context.Background()
	}

	switch packet.EMsg {
	case enums.EMsg_Multi:
		d.handleMulti(packet)
		return false

	case enums.EMsg_ServiceMethod:
		d.handleService(packet)
		return false
	}

	if d.handleJobResponse(packet) {
		return true
	}

	if uint32(packet.EMsg) < maxDenseEMsg {
		hPtr := d.denseHandlers[packet.EMsg].Load()
		if hPtr != nil && *hPtr != nil {
			d.invokeHandler(*hPtr, packet)
			return false
		}
	}

	d.mu.RLock()
	handler, ok := d.sparseHandlers[packet.EMsg]
	d.mu.RUnlock()

	if ok {
		d.invokeHandler(handler, packet)
	}

	return false
}

// Close closes the dispatcher and job manager.
func (d *Dispatcher) Close() error {
	return d.jobManager.Close()
}

func (d *Dispatcher) invokeHandler(handler Handler, packet *protocol.Packet) {
	defer func() {
		if r := recover(); r != nil {
			d.getLogger().ErrorContext(packet.Context(), "Recovered from handler panic",
				log.Int32("emsg", int32(packet.EMsg)),
				log.Any("panic", r),
			)
		}
	}()

	handler(packet)
}

func (d *Dispatcher) handleService(packet *protocol.Packet) {
	protoHdr := packet.ProtoHeader()
	if protoHdr == nil || protoHdr.Proto == nil {
		return
	}

	methodName := protoHdr.Proto.GetTargetJobName()
	hash := fastHashString(methodName)
	idx := hash & (serviceTableSize - 1)

	var handler Handler
	for i := range serviceTableSize {
		slotIdx := (idx + uint64(i)) & (serviceTableSize - 1)
		entry := &d.denseServiceTable[slotIdx]

		if entry.hash == hash && entry.method == methodName {
			hPtr := entry.handler.Load()
			if hPtr != nil {
				handler = *hPtr
			}

			break
		}

		if entry.hash == 0 {
			break
		}
	}

	if handler != nil {
		d.invokeHandler(handler, packet)
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

var cmsgMultiPool = sync.Pool{
	New: func() any { return &pb.CMsgMulti{} },
}

func acquireCMsgMulti() *pb.CMsgMulti {
	msg := cmsgMultiPool.Get().(*pb.CMsgMulti)
	body := msg.GetMessageBody()[:0]
	msg.Reset()
	msg.MessageBody = body

	return msg
}

func releaseCMsgMulti(msg *pb.CMsgMulti) {
	if msg == nil {
		return
	}

	cmsgMultiPool.Put(msg)
}

func (d *Dispatcher) handleMulti(packet *protocol.Packet) {
	msg := acquireCMsgMulti()
	defer releaseCMsgMulti(msg)

	if err := protocol.UnmarshalProto(packet.Payload, msg); err != nil {
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

	offset := 0
	for offset+4 <= len(payload) {
		subSize := binary.LittleEndian.Uint32(payload[offset : offset+4])
		offset += 4

		if offset+int(subSize) > len(payload) {
			break
		}

		subPayload := payload[offset : offset+int(subSize)]
		offset += int(subSize)

		subPacket, err := protocol.ParsePacket(bytes.NewReader(subPayload))
		if err != nil {
			continue
		}

		subPacket.Ctx = packet.Context()
		subPacket.ReceivedAt = packet.ReceivedAt

		if !d.Dispatch(subPacket) {
			protocol.ReleasePacket(subPacket)
		}
	}
}

var gzipReaderPool = sync.Pool{
	New: func() any { return new(gzip.Reader) },
}

var decompressedPool = sync.Pool{
	New: func() any {
		b := make([]byte, 64*1024)

		return &b
	},
}

func (d *Dispatcher) decompressPayload(data []byte, unzippedSize int64) ([]byte, error) {
	if unzippedSize > d.DecompressionLimit {
		return nil, fmt.Errorf("%w: %d bytes", ErrDecompressionLimit, unzippedSize)
	}

	gr := gzipReaderPool.Get().(*gzip.Reader)
	if err := gr.Reset(bytes.NewReader(data)); err != nil {
		gzipReaderPool.Put(gr)
		return nil, fmt.Errorf("gzip reader reset failed: %w", err)
	}

	defer gzipReaderPool.Put(gr)

	var out []byte
	if unzippedSize <= 64*1024 {
		bufPtr := decompressedPool.Get().(*[]byte)
		if cap(*bufPtr) < int(unzippedSize) {
			*bufPtr = make([]byte, unzippedSize)
		}

		out = (*bufPtr)[:unzippedSize]
	} else {
		out = make([]byte, unzippedSize)
	}

	if _, err := io.ReadFull(gr, out); err != nil {
		_ = gr.Close()
		return nil, fmt.Errorf("failed to read full decompressed payload: %w", err)
	}

	_ = gr.Close()

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
	if buf.Cap() <= 128*1024 {
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

	if sess != nil && eMsg != enums.EMsg_ClientHello {
		steamID = sess.SteamID()
		sessionID = sess.SessionID()
	}

	pkt := protocol.AcquirePacket()
	pkt.EMsg = eMsg
	pkt.IsProto = isProto

	if isProto {
		pkt.HeaderKind = protocol.HeaderKindProto
		if pkt.HdrProto.Proto == nil {
			pkt.HdrProto.Proto = protocol.AcquireProtoHeader()
		} else {
			pkt.HdrProto.Proto.Reset()
		}

		pkt.HdrProto.EMsg = eMsg
		protocol.SetProtoUint64(&pkt.HdrProto.Proto.Steamid, steamID)
		protocol.SetProtoInt32(&pkt.HdrProto.Proto.ClientSessionid, sessionID)
		protocol.SetProtoUint64(&pkt.HdrProto.Proto.JobidSource, jobID)
		protocol.SetProtoUint64(&pkt.HdrProto.Proto.JobidTarget, protocol.NoJob)

		if routingAppID > 0 {
			pkt.HdrProto.Proto.RoutingAppid = proto.Uint32(routingAppID)
		}

		if jobName != "" {
			pkt.HdrProto.Proto.TargetJobName = proto.String(jobName)
		}

		if token != "" {
			pkt.HdrProto.Proto.WgToken = proto.String(token)
		}
	} else {
		if eMsg == enums.EMsg_ChannelEncryptRequest ||
			eMsg == enums.EMsg_ChannelEncryptResponse ||
			eMsg == enums.EMsg_ChannelEncryptResult {
			pkt.HeaderKind = protocol.HeaderKindStandard
			hdr := protocol.NewMsgHdr(eMsg, protocol.NoJob)
			hdr.SourceJobID = jobID
			pkt.HdrStd = *hdr
		} else {
			pkt.HeaderKind = protocol.HeaderKindExtended
			hdr := protocol.NewMsgHdrExtended(eMsg, steamID, sessionID)
			hdr.SourceJobID = jobID
			pkt.HdrExt = *hdr
		}
	}

	return pkt
}

func (d *Dispatcher) getLogger() log.Logger {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.logger
}
