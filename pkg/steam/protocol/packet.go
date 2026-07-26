// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package protocol implements Steam Connection Manager (CM) wire formats and packet serialization.
package protocol

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/internal/framer"
	"github.com/lemon4ksan/g-man/internal/network"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

var (
	// ErrHeaderTooLarge is returned when the target header exceeds the limit.
	ErrHeaderTooLarge = errors.New("proto header too large")
	// ErrPayloadTooLarge is returned when the target payload exceeds the imit.
	ErrPayloadTooLarge = errors.New("payload exceeds maximum size")
	// ErrInvalidHeader is returned when the invalid header is passed.
	ErrInvalidHeader = errors.New("invalid header format")
	// ErrProtoHeaderUnmarshal is returned when the proto header unmarshal fails.
	ErrProtoHeaderUnmarshal = errors.New("gc: unmarshal proto header")
)

// VTUnmarshaler defines an interface for vtprotobuf unmarshaling.
type VTUnmarshaler interface {
	UnmarshalVT(data []byte) error
}

// VTMarshaler defines an interface for vtprotobuf marshaling.
type VTMarshaler interface {
	MarshalVT() ([]byte, error)
}

// UnmarshalProto unmarshals Protobuf with maximum speed:
// If the structure supports vtprotobuf (UnmarshalVT) — calls the zero-reflection code directly.
// Otherwise — falls back to the standard proto.Unmarshal.
func UnmarshalProto(data []byte, msg proto.Message) error {
	if vt, ok := msg.(VTUnmarshaler); ok {
		return vt.UnmarshalVT(data)
	}

	return proto.Unmarshal(data, msg)
}

// MarshalProto marshals Protobuf with minimum reflection overhead.
func MarshalProto(msg proto.Message) ([]byte, error) {
	if vt, ok := msg.(VTMarshaler); ok {
		return vt.MarshalVT()
	}

	return proto.Marshal(msg)
}

// Header describes the common interface for all Steam message headers.
// It provides methods for accessing job IDs used for request-response tracking.
type Header interface {
	GetSourceJob() uint64
	GetTargetJob() uint64
	SerializeTo(w io.Writer) error
}

// AuthorizedHeader describes a header that contains steamID and SessionID.
type AuthorizedHeader interface {
	Header
	GetSteamID() uint64
	GetSessionID() int32
}

// EHeader describes a header that has a [EResult].
type EHeader interface {
	Header
	GetEResult() enums.EResult
}

// TransportType represents the protocol transport type.
type TransportType string

// Transport types supported by G-MAN.
const (
	TransportTCP    TransportType = "TCP"
	TransportWS     TransportType = "WebSocket"
	TransportWebAPI TransportType = "WebAPI"
)

var (
	transportMappingsMu sync.RWMutex
	transportMappings   = map[string]TransportType{
		network.ConnTypeTCP: TransportTCP,
		network.ConnTypeWS:  TransportWS,
	}
)

// RegisterTransportMapping registers a mapping between a raw connection name
// (e.g. from network.Connection.Name()) and a business-level [TransportType].
// This is thread-safe and allows G-MAN to support new transport protocols.
func RegisterTransportMapping(connName string, t TransportType) {
	transportMappingsMu.Lock()
	defer transportMappingsMu.Unlock()

	transportMappings[connName] = t
}

// MapConnectionToTransport maps a raw connection protocol name to a [TransportType].
// If no mapping is registered, it defaults to treating the connection name itself
// as the TransportType.
func MapConnectionToTransport(connName string) TransportType {
	transportMappingsMu.RLock()
	defer transportMappingsMu.RUnlock()

	if t, ok := transportMappings[connName]; ok {
		return t
	}

	return TransportType(connName)
}

type contextKey string

const (
	transportTypeKey contextKey = "transport_type"
	callerInfoKey    contextKey = "caller_info"
)

// WithTransportType returns a new context with the given transport type.
func WithTransportType(ctx context.Context, t TransportType) context.Context {
	return context.WithValue(ctx, transportTypeKey, t)
}

// GetTransportType retrieves the transport type from the context.
func GetTransportType(ctx context.Context) (TransportType, bool) {
	t, ok := ctx.Value(transportTypeKey).(TransportType)
	return t, ok
}

// InboundMessage represents a raw network message packaged with its exact
// arrival time and transport type context. This is passed through internal G-MAN
// channels to eliminate metadata registry allocations.
type InboundMessage struct {
	Data       *framer.FrameBuffer
	ReceivedAt time.Time
	Transport  TransportType
}

// HeaderKind indicates the type of header embedded inside a Packet.
type HeaderKind uint8

// HeaderKind constants indicate the type of header embedded inside a Packet.
const (
	HeaderKindNone HeaderKind = iota
	HeaderKindProto
	HeaderKindExtended
	HeaderKindStandard
)

var packetPool = sync.Pool{
	New: func() any {
		return &Packet{
			Payload: make([]byte, 0, 4096),
		}
	},
}

// AcquirePacket acquires a packet from the pool and resets it for use.
func AcquirePacket() *Packet {
	return packetPool.Get().(*Packet)
}

// ReleasePacket releases a packet back to the pool.
func ReleasePacket(p *Packet) {
	if p == nil {
		return
	}

	p.Reset()
	packetPool.Put(p)
}

// Packet represents a parsed message received from or sent to a Steam Connection Manager.
// It serves as a unified interface regardless of the underlying header format.
// Concrete headers are embedded directly to avoid runtime.iface boxing allocations.
type Packet struct {
	// EMsg identifies the type of message this packet contains
	EMsg enums.EMsg
	// IsProto is true if the packet uses a Protobuf-style header.
	IsProto bool
	// HeaderKind indicates the type of header stored in the packet.
	HeaderKind HeaderKind

	// Inline concrete headers to completely bypass interface boxing allocations
	HdrProto MsgHdrProtoBuf
	HdrExt   MsgHdrExtended
	HdrStd   MsgHdr

	// Payload is the raw message body, which can be further
	// unmarshaled into a specific Protobuf struct or VDF map.
	Payload []byte
	// Ctx represents the request context associated with the packet execution.
	Ctx context.Context
	// ReceivedAt represents the exact time the packet was received by the transport layer.
	ReceivedAt time.Time
	// Transport represents the transport type used to receive this packet.
	Transport TransportType
}

// Context returns the packet's execution context, defaulting to context.Background() if nil.
func (p *Packet) Context() context.Context {
	ctx := p.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	if p.Transport != "" {
		if _, ok := GetTransportType(ctx); !ok {
			ctx = WithTransportType(ctx, p.Transport)
		}
	}

	return ctx
}

// ProtoHeader returns the concrete Protobuf header without interface conversion.
func (p *Packet) ProtoHeader() *MsgHdrProtoBuf {
	if p.HeaderKind == HeaderKindProto {
		return &p.HdrProto
	}

	return nil
}

// ParsePacket decodes a steam network message from an [io.Reader].
//
// It automatically detects the header format by examining EMsg bitmask.
// Uses fast-path byte reading when r is *bytes.Reader to eliminate binary.Read reflection overhead.
func ParsePacket(r io.Reader) (*Packet, error) {
	if br, ok := r.(*bytes.Reader); ok {
		return parseFast(br)
	}

	var rawEMsg uint32
	if err := binary.Read(r, binary.LittleEndian, &rawEMsg); err != nil {
		return nil, fmt.Errorf("read emsg: %w", err)
	}

	eMsg := enums.EMsg(rawEMsg & EMsgMask)
	isProto := (rawEMsg & ProtoMask) != 0

	p := AcquirePacket()
	p.EMsg = eMsg
	p.IsProto = isProto

	switch {
	case isProto:
		p.HeaderKind = HeaderKindProto

		p.HdrProto.EMsg = eMsg
		if err := p.HdrProto.Deserialize(r); err != nil {
			ReleasePacket(p)
			return nil, fmt.Errorf("deserialize header: %w", err)
		}

	case eMsg == enums.EMsg_ChannelEncryptRequest ||
		eMsg == enums.EMsg_ChannelEncryptResponse ||
		eMsg == enums.EMsg_ChannelEncryptResult:
		p.HeaderKind = HeaderKindStandard

		p.HdrStd.EMsg = eMsg
		if err := p.HdrStd.Deserialize(r); err != nil {
			ReleasePacket(p)
			return nil, fmt.Errorf("deserialize header: %w", err)
		}

	default:
		p.HeaderKind = HeaderKindExtended

		p.HdrExt.EMsg = eMsg
		if err := p.HdrExt.Deserialize(r); err != nil {
			ReleasePacket(p)
			return nil, fmt.Errorf("deserialize header: %w", err)
		}
	}

	payload, err := io.ReadAll(r)
	if err != nil {
		ReleasePacket(p)
		return nil, err
	}

	if len(payload) > MaxPayloadSize {
		ReleasePacket(p)
		return nil, ErrPayloadTooLarge
	}

	p.Payload = payload
	p.Transport = ""

	return p, nil
}

func parseFast(br *bytes.Reader) (*Packet, error) {
	if br.Len() < 4 {
		return nil, fmt.Errorf("read emsg: %w", io.ErrUnexpectedEOF)
	}

	var emsgBuf [4]byte
	if _, err := io.ReadFull(br, emsgBuf[:]); err != nil {
		return nil, fmt.Errorf("read emsg: %w", err)
	}

	rawEMsg := binary.LittleEndian.Uint32(emsgBuf[:])
	eMsg := enums.EMsg(rawEMsg & EMsgMask)
	isProto := (rawEMsg & ProtoMask) != 0

	p := AcquirePacket()
	p.EMsg = eMsg
	p.IsProto = isProto

	switch {
	case isProto:
		p.HeaderKind = HeaderKindProto

		p.HdrProto.EMsg = eMsg
		if err := p.HdrProto.Deserialize(br); err != nil {
			ReleasePacket(p)
			return nil, fmt.Errorf("deserialize header: %w", err)
		}

	case eMsg == enums.EMsg_ChannelEncryptRequest ||
		eMsg == enums.EMsg_ChannelEncryptResponse ||
		eMsg == enums.EMsg_ChannelEncryptResult:
		p.HeaderKind = HeaderKindStandard

		p.HdrStd.EMsg = eMsg
		if err := p.HdrStd.Deserialize(br); err != nil {
			ReleasePacket(p)
			return nil, fmt.Errorf("deserialize header: %w", err)
		}

	default:
		p.HeaderKind = HeaderKindExtended

		p.HdrExt.EMsg = eMsg
		if err := p.HdrExt.Deserialize(br); err != nil {
			ReleasePacket(p)
			return nil, fmt.Errorf("deserialize header: %w", err)
		}
	}

	remLen := br.Len()
	if remLen > MaxPayloadSize {
		ReleasePacket(p)
		return nil, ErrPayloadTooLarge
	}

	if cap(p.Payload) < remLen {
		p.Payload = make([]byte, remLen)
	} else {
		p.Payload = p.Payload[:remLen]
	}

	if _, err := io.ReadFull(br, p.Payload); err != nil {
		ReleasePacket(p)
		return nil, err
	}

	return p, nil
}

// GetTargetJobID returns the JobID of the intended recipient.
// Returns [NoJob] if the header does not support job tracking
// or is not present.
func (p *Packet) GetTargetJobID() uint64 {
	switch p.HeaderKind {
	case HeaderKindProto:
		return p.HdrProto.GetTargetJob()
	case HeaderKindExtended:
		return p.HdrExt.GetTargetJob()
	case HeaderKindStandard:
		return p.HdrStd.GetTargetJob()
	default:
		return NoJob
	}
}

// GetSourceJobID returns the JobID assigned by the sender to track this request.
// This is used to map responses back to their original requests.
func (p *Packet) GetSourceJobID() uint64 {
	switch p.HeaderKind {
	case HeaderKindProto:
		return p.HdrProto.GetSourceJob()
	case HeaderKindExtended:
		return p.HdrExt.GetSourceJob()
	case HeaderKindStandard:
		return p.HdrStd.GetSourceJob()
	default:
		return NoJob
	}
}

// GetSteamID returns the steamID of the header.
// Returns 0 if header doesn't implement [AuthorizedHeader].
func (p *Packet) GetSteamID() uint64 {
	switch p.HeaderKind {
	case HeaderKindProto:
		return p.HdrProto.GetSteamID()
	case HeaderKindExtended:
		return p.HdrExt.GetSteamID()
	default:
		return 0
	}
}

// GetSessionID returns the sessionID of the header.
// Returns 0 if header doesn't implement [AuthorizedHeader].
func (p *Packet) GetSessionID() int32 {
	switch p.HeaderKind {
	case HeaderKindProto:
		return p.HdrProto.GetSessionID()
	case HeaderKindExtended:
		return p.HdrExt.GetSessionID()
	default:
		return 0
	}
}

// GetEResult returns the header result code.
// Returns [EResult_Invalid] if header doesn't implement [EHeader].
func (p *Packet) GetEResult() enums.EResult {
	if p.HeaderKind == HeaderKindProto {
		return p.HdrProto.GetEResult()
	}

	return enums.EResult_Invalid
}

// SerializeTo encodes the packet to [io.Writer] for sending.
// Returns error if packet marked as proto but header is not [MsgHdrProtoBuf].
// SerializeTo encodes the packet to [io.Writer] for sending.
func (p *Packet) SerializeTo(w io.Writer) error {
	var err error
	switch p.HeaderKind {
	case HeaderKindProto:
		err = p.HdrProto.SerializeTo(w)
	case HeaderKindExtended:
		err = p.HdrExt.SerializeTo(w)
	case HeaderKindStandard:
		err = p.HdrStd.SerializeTo(w)
	default:
		return ErrInvalidHeader
	}

	if err != nil {
		return err
	}

	_, err = w.Write(p.Payload)

	return err
}

// Reset resets the packet fields to their zero values and recycles headers to pools.
func (p *Packet) Reset() {
	if p.HeaderKind == HeaderKindProto {
		p.HdrProto.Reset()
	}

	p.EMsg = 0
	p.IsProto = false
	p.HeaderKind = HeaderKindNone
	p.Payload = p.Payload[:0]
	p.Ctx = nil
	p.ReceivedAt = time.Time{}
	p.Transport = ""
}

var gcPacketPool = sync.Pool{
	New: func() any { return &GCPacket{} },
}

// AcquireGCPacket acquires a GCPacket from the pool and resets it for use.
func AcquireGCPacket() *GCPacket {
	p := gcPacketPool.Get().(*GCPacket)
	p.Reset()
	return p
}

// ReleaseGCPacket releases a GCPacket back to the pool.
func ReleaseGCPacket(p *GCPacket) {
	if p == nil {
		return
	}

	p.Reset()
	gcPacketPool.Put(p)
}

// GCPacket represents a Game Coordinator message.
type GCPacket struct {
	// AppID is the Steam AppID of the target game (for example, 440 for TF2).
	AppID uint32
	// MsgType is the game-specific Game Coordinator message type identifier.
	MsgType uint32
	// IsProto is true if the Game Coordinator message is encoded using Protobuf.
	IsProto bool
	// TargetJobID is the unique job correlation ID of the recipient GC.
	TargetJobID uint64
	// SourceJobID is the unique job correlation ID of the sender GC.
	SourceJobID uint64
	// Payload is the raw game-specific payload data.
	Payload []byte
}

// NewGCPacket creates a new GC packet with the given parameters.
func NewGCPacket(appID, msgType uint32, payload []byte) *GCPacket {
	return &GCPacket{
		AppID:   appID,
		MsgType: msgType,
		Payload: payload,
	}
}

// Serialize encodes the packet into the wire format expected by the Steam GC.
//
// It returns an error if Protobuf marshaling of the header fails.
func (p *GCPacket) Serialize() ([]byte, error) {
	buf := new(bytes.Buffer)

	if p.IsProto {
		// Protobuf Header: [MsgType | Mask] [HeaderLength] [ProtoHeader] [Body]
		msgType := p.MsgType | ProtoMask
		if err := binary.Write(buf, binary.LittleEndian, msgType); err != nil {
			return nil, err
		}

		hdr := &pb.CMsgProtoBufHeader{
			JobidSource: proto.Uint64(p.SourceJobID),
			JobidTarget: proto.Uint64(p.TargetJobID),
		}

		hdrBytes, err := proto.Marshal(hdr)
		if err != nil {
			return nil, fmt.Errorf("gc: marshal proto header: %w", err)
		}

		if err := binary.Write(buf, binary.LittleEndian, uint32(len(hdrBytes))); err != nil {
			return nil, err
		}

		buf.Write(hdrBytes)
	} else {
		// [HeaderVersion(1)] [TargetJobID] [SourceJobID] [Body]
		// Note: Legacy GC header structure varies by game, but usually standard 18 bytes
		header := make([]byte, 18)
		binary.LittleEndian.PutUint16(header[0:], 1) // Header Version
		binary.LittleEndian.PutUint64(header[2:], p.TargetJobID)
		binary.LittleEndian.PutUint64(header[10:], p.SourceJobID)
		buf.Write(header)
	}

	buf.Write(p.Payload)

	return buf.Bytes(), nil
}

// ParseGCPacket decodes a raw byte slice from ClientFromGC into a Packet without heap pointer allocations.
func ParseGCPacket(appID, msgType uint32, data []byte) (*GCPacket, error) {
	p := AcquireGCPacket()
	p.AppID = appID
	p.MsgType = msgType & ^uint32(ProtoMask)
	p.IsProto = (msgType & ProtoMask) > 0

	offset := 0

	if p.IsProto {
		if len(data) < 4 {
			ReleaseGCPacket(p)
			return nil, fmt.Errorf("gc: read inner msgtype: %w", io.ErrUnexpectedEOF)
		}

		if len(data) < 8 {
			ReleaseGCPacket(p)
			return nil, fmt.Errorf("gc: read proto header len: %w", io.ErrUnexpectedEOF)
		}

		hdrLen := binary.LittleEndian.Uint32(data[4:8])
		offset = 8

		if len(data) < offset+int(hdrLen) {
			ReleaseGCPacket(p)
			return nil, fmt.Errorf("gc: read proto header: %w", io.ErrUnexpectedEOF)
		}

		hdrBytes := data[offset : offset+int(hdrLen)]
		offset += int(hdrLen)

		// Use MsgHdrProtoBuf's zero-alloc FastUnmarshal instead of CMsgProtoBufHeader.UnmarshalVT to avoid 22.6M allocations
		pbHdr := acquireMsgHdrProtoBuf(0)

		err := pbHdr.FastUnmarshal(hdrBytes)
		if err != nil {
			releaseMsgHdrProtoBuf(pbHdr)
			ReleaseGCPacket(p)
			return nil, fmt.Errorf("%w: %w", ErrProtoHeaderUnmarshal, err)
		}

		p.TargetJobID = pbHdr.GetTargetJob()
		p.SourceJobID = pbHdr.GetSourceJob()
		releaseMsgHdrProtoBuf(pbHdr)
	} else {
		if len(data) < 18 {
			ReleaseGCPacket(p)
			return nil, fmt.Errorf("gc: read legacy header: %w", io.ErrUnexpectedEOF)
		}

		p.TargetJobID = binary.LittleEndian.Uint64(data[2:10])
		p.SourceJobID = binary.LittleEndian.Uint64(data[10:18])
		offset = 18
	}

	p.Payload = data[offset:]

	return p, nil
}

// Reset resets the packet fields to their zero values.
func (p *GCPacket) Reset() {
	p.AppID = 0
	p.MsgType = 0
	p.IsProto = false
	p.TargetJobID = NoJob
	p.SourceJobID = NoJob
	p.Payload = nil
}
