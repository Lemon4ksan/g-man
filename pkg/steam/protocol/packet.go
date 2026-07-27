// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

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
	// ErrHeaderTooLarge indicates a header size exceeded MaxHeaderSize bounds.
	ErrHeaderTooLarge = errors.New("proto header too large")
	// ErrPayloadTooLarge indicates packet payload exceeded MaxPayloadSize bounds.
	ErrPayloadTooLarge = errors.New("payload exceeds maximum size")
	// ErrInvalidHeader indicates an unrecognized or corrupt header format.
	ErrInvalidHeader = errors.New("invalid header format")
	// ErrProtoHeaderUnmarshal indicates inner Game Coordinator proto header unmarshaling failed.
	ErrProtoHeaderUnmarshal = errors.New("gc: unmarshal proto header")
)

type VTUnmarshaler interface {
	UnmarshalVT(data []byte) error
}

type VTMarshaler interface {
	MarshalVT() ([]byte, error)
}

// UnmarshalProto unmarshals protobuf bytes using direct vtprotobuf code when supported, falling back to proto.Unmarshal.
func UnmarshalProto(data []byte, msg proto.Message) error {
	if vt, ok := msg.(VTUnmarshaler); ok {
		return vt.UnmarshalVT(data)
	}

	return proto.Unmarshal(data, msg)
}

// MarshalProto marshals protobuf structures using direct vtprotobuf code when supported, falling back to proto.Marshal.
func MarshalProto(msg proto.Message) ([]byte, error) {
	if vt, ok := msg.(VTMarshaler); ok {
		return vt.MarshalVT()
	}

	return proto.Marshal(msg)
}

type Header interface {
	GetSourceJob() uint64
	GetTargetJob() uint64
	SerializeTo(w io.Writer) error
}

type AuthorizedHeader interface {
	Header
	GetSteamID() uint64
	GetSessionID() int32
}

type EHeader interface {
	Header
	GetEResult() enums.EResult
}

type TransportType string

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

func RegisterTransportMapping(connName string, t TransportType) {
	transportMappingsMu.Lock()
	defer transportMappingsMu.Unlock()

	transportMappings[connName] = t
}

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
)

func WithTransportType(ctx context.Context, t TransportType) context.Context {
	return context.WithValue(ctx, transportTypeKey, t)
}

func GetTransportType(ctx context.Context) (TransportType, bool) {
	t, ok := ctx.Value(transportTypeKey).(TransportType)

	return t, ok
}

type InboundMessage struct {
	Data       *framer.FrameBuffer
	ReceivedAt time.Time
	Transport  TransportType
}

type HeaderKind uint8

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

// AcquirePacket fetches a Packet from memory pool.
func AcquirePacket() *Packet {
	return packetPool.Get().(*Packet)
}

// ReleasePacket recycles a Packet back to memory pool.
func ReleasePacket(p *Packet) {
	if p == nil {
		return
	}

	p.Reset()
	packetPool.Put(p)
}

// Packet encapsulates parsed Steam CM network messages and embedded concrete headers.
type Packet struct {
	EMsg       enums.EMsg
	IsProto    bool
	HeaderKind HeaderKind

	HdrProto MsgHdrProtoBuf
	HdrExt   MsgHdrExtended
	HdrStd   MsgHdr

	Payload    []byte
	Ctx        context.Context
	ReceivedAt time.Time
	Transport  TransportType
}

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

func (p *Packet) ProtoHeader() *MsgHdrProtoBuf {
	if p.HeaderKind == HeaderKindProto {
		return &p.HdrProto
	}

	return nil
}

// ParsePacket decodes a Steam network message from reader.
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

func (p *Packet) GetEResult() enums.EResult {
	if p.HeaderKind == HeaderKindProto {
		return p.HdrProto.GetEResult()
	}

	return enums.EResult_Invalid
}

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

func AcquireGCPacket() *GCPacket {
	p := gcPacketPool.Get().(*GCPacket)
	p.Reset()

	return p
}

func ReleaseGCPacket(p *GCPacket) {
	if p == nil {
		return
	}

	p.Reset()
	gcPacketPool.Put(p)
}

// GCPacket represents Game Coordinator messages.
type GCPacket struct {
	AppID       uint32
	MsgType     uint32
	IsProto     bool
	TargetJobID uint64
	SourceJobID uint64
	Payload     []byte
}

func NewGCPacket(appID, msgType uint32, payload []byte) *GCPacket {
	return &GCPacket{
		AppID:   appID,
		MsgType: msgType,
		Payload: payload,
	}
}

func (p *GCPacket) Serialize() ([]byte, error) {
	buf := new(bytes.Buffer)

	if p.IsProto {
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
		header := make([]byte, 18)
		binary.LittleEndian.PutUint16(header[0:], 1)
		binary.LittleEndian.PutUint64(header[2:], p.TargetJobID)
		binary.LittleEndian.PutUint64(header[10:], p.SourceJobID)
		buf.Write(header)
	}

	buf.Write(p.Payload)

	return buf.Bytes(), nil
}

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

func (p *GCPacket) Reset() {
	p.AppID = 0
	p.MsgType = 0
	p.IsProto = false
	p.TargetJobID = NoJob
	p.SourceJobID = NoJob
	p.Payload = nil
}
