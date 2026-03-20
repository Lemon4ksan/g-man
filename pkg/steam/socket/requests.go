package socket

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/lemon4ksan/g-man/pkg/jobs"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"google.golang.org/protobuf/proto"
)

// SendConfig contains parameters for sending a message.
type SendConfig struct {
	Callback jobs.Callback[*protocol.Packet]
}

// SendOption defines a functional option for SendConfig.
type SendOption func(*SendConfig)

// WithCallback adds a callback to asynchronously wait for a response to the sent packet.
func WithCallback(cb jobs.Callback[*protocol.Packet]) SendOption {
	return func(c *SendConfig) {
		c.Callback = cb
	}
}

// PayloadBuilder describes a function that assembles a binary packet body with the required headers.
type PayloadBuilder func(sess Session, buf *bytes.Buffer, sourceJobID uint64) error

// Proto creates a PayloadBuilder to send a Protobuf message.
func Proto(eMsg protocol.EMsg, req proto.Message) PayloadBuilder {
	return func(sess Session, buf *bytes.Buffer, sourceJobID uint64) error {
		pkt, err := buildProtoPacket(sess, eMsg, sourceJobID, req)
		if err != nil {
			return err
		}
		return pkt.SerializeTo(buf)
	}
}

// Unified creates a PayloadBuilder to call the Unified Service (e.g. "Player.GetGameBadgeLevels#1").
func Unified(method string, req proto.Message) PayloadBuilder {
	return func(sess Session, buf *bytes.Buffer, sourceJobID uint64) error {
		pkt, err := buildProtoPacket(sess, protocol.EMsg_ServiceMethodCallFromClient, sourceJobID, req)
		if err != nil {
			return err
		}
		pkt.Header.(*protocol.MsgHdrProtoBuf).Proto.TargetJobName = proto.String(method)
		return pkt.SerializeTo(buf)
	}
}

// Raw creates a PayloadBuilder to send a plain byte array (e.g. for encryption).
func Raw(eMsg protocol.EMsg, payload []byte) PayloadBuilder {
	return func(sess Session, buf *bytes.Buffer, sourceJobID uint64) error {
		hdr := protocol.NewMsgHdr(eMsg, protocol.NoJob)
		hdr.SourceJobID = sourceJobID

		pkt := &protocol.Packet{
			EMsg:    eMsg,
			IsProto: false,
			Header:  hdr,
			Payload: payload,
		}

		return pkt.SerializeTo(buf)
	}
}

// DynamicRaw creates a PayloadBuilder to send both unified and raw messages based on provided arguments.
func DynamicRaw(eMsg protocol.EMsg, targetName string, payload []byte) PayloadBuilder {
	return func(sess Session, buf *bytes.Buffer, sourceJobID uint64) error {
		var steamID uint64
		var sessionID int32
		if sess != nil {
			steamID = sess.SteamID()
			sessionID = sess.SessionID()
		}

		var pkt *protocol.Packet

		if targetName != "" {
			hdr := protocol.NewMsgHdrProtoBuf(eMsg, steamID, sessionID)
			hdr.Proto.JobidSource = proto.Uint64(sourceJobID)
			hdr.Proto.TargetJobName = proto.String(targetName)

			pkt = &protocol.Packet{
				EMsg:    eMsg,
				IsProto: true,
				Header:  hdr,
				Payload: payload,
			}
		} else {
			hdr := protocol.NewMsgHdrExtended(eMsg, steamID, sessionID)
			hdr.SourceJobID = sourceJobID

			pkt = &protocol.Packet{
				EMsg:    eMsg,
				IsProto: false,
				Header:  hdr,
				Payload: payload,
			}
		}

		return pkt.SerializeTo(buf)
	}
}

// Send performs a request to steam connection manager by constructing a payload using the provided builder.
func (s *Socket) Send(ctx context.Context, build PayloadBuilder, opts ...SendOption) error {
	cfg := &SendConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	sess := s.Session()
	if sess == nil {
		return errors.New("socket is disconnected")
	}

	sourceJobID := protocol.NoJob
	if cfg.Callback != nil {
		sourceJobID = s.jobManager.NextID()
		err := s.jobManager.Add(sourceJobID, func(response *protocol.Packet, err error) {
			var eMsg protocol.EMsg
			if response != nil {
				eMsg = response.EMsg
			}
			defer s.recoverPanic(eMsg)
			cfg.Callback(response, err)
		}, jobs.WithContext[*protocol.Packet](ctx))

		if err != nil {
			return fmt.Errorf("track job: %w", err)
		}
	}

	buf := s.bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer func() {
		if buf.Cap() <= 64*1024 {
			s.bufferPool.Put(buf)
		}
	}()

	if err := build(sess, buf, sourceJobID); err != nil {
		if cfg.Callback != nil {
			s.jobManager.Resolve(sourceJobID, nil, err)
		}
		return err
	}

	if err := sess.Send(ctx, buf.Bytes()); err != nil {
		if cfg.Callback != nil {
			s.jobManager.Resolve(sourceJobID, nil, err)
		}
		return err
	}

	return nil
}

func (s *Socket) SendProto(ctx context.Context, eMsg protocol.EMsg, req proto.Message, opts ...SendOption) error {
	return s.Send(ctx, Proto(eMsg, req), opts...)
}

func (s *Socket) SendUnified(ctx context.Context, method string, req proto.Message, opts ...SendOption) error {
	return s.Send(ctx, Unified(method, req), opts...)
}

func (s *Socket) SendRaw(ctx context.Context, eMsg protocol.EMsg, payload []byte, opts ...SendOption) error {
	return s.Send(ctx, Raw(eMsg, payload), opts...)
}

// SendSync is a convenient wrapper around Send for synchronous (blocking) request execution.
// The method will wait for a response from the server or fail if the context timeout occurs.
func (s *Socket) SendSync(ctx context.Context, build PayloadBuilder, opts ...SendOption) (*protocol.Packet, error) {
	resultCh := make(chan struct {
		pkt *protocol.Packet
		err error
	}, 1)

	syncOpt := WithCallback(func(pkt *protocol.Packet, err error) {
		resultCh <- struct {
			pkt *protocol.Packet
			err error
		}{pkt, err}
	})

	opts = append(opts, syncOpt)

	if err := s.Send(ctx, build, opts...); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-resultCh:
		return res.pkt, res.err
	}
}

func buildProtoPacket(sess Session, eMsg protocol.EMsg, jobID uint64, body proto.Message) (*protocol.Packet, error) {
	var steamID uint64
	var sessionID int32
	if sess != nil {
		steamID = sess.SteamID()
		sessionID = sess.SessionID()
	}
	if eMsg == protocol.EMsg_ClientHello {
		steamID, sessionID = 0, 0
	}

	payload, err := proto.Marshal(body)
	if err != nil {
		return nil, err
	}

	hdr := protocol.NewMsgHdrProtoBuf(eMsg, steamID, sessionID)
	hdr.Proto.JobidSource = proto.Uint64(jobID)

	return &protocol.Packet{EMsg: eMsg, IsProto: true, Header: hdr, Payload: payload}, nil
}
