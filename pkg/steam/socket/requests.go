// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socket

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/jobs"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
)

// SendConfig contains parameters for sending a message.
type SendConfig struct {
	// Callback is invoked when a response to this message is received.
	Callback jobs.Callback[*protocol.Packet]
	// Token is an optional WebAPI token for specific service calls.
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
type PayloadBuilder func(sess Session, buf *bytes.Buffer, sourceJobID uint64, token string) error

// Proto builds a standard Protobuf-wrapped packet.
func Proto(eMsg enums.EMsg, req proto.Message) PayloadBuilder {
	return func(sess Session, buf *bytes.Buffer, sourceJobID uint64, token string) (err error) {
		pkt := newPacket(sess, eMsg, sourceJobID, true, "", token)
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
	return func(sess Session, buf *bytes.Buffer, sourceJobID uint64, token string) (err error) {
		pkt := newPacket(sess, enums.EMsg_ServiceMethodCallFromClient, sourceJobID, true, method, token)
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
	return func(sess Session, buf *bytes.Buffer, sourceJobID uint64, _ string) error {
		pkt := newPacket(sess, eMsg, sourceJobID, false, "", "")
		pkt.Payload = payload
		return pkt.SerializeTo(buf)
	}
}

// DynamicRaw creates a PayloadBuilder that decides between Protobuf and Extended
// headers based on whether a targetName (Unified Service method) is provided.
// targetName == "" implies a standard (non-unified) message.
func DynamicRaw(eMsg enums.EMsg, targetName string, payload []byte) PayloadBuilder {
	return func(sess Session, buf *bytes.Buffer, sourceJobID uint64, token string) error {
		isProto := targetName != ""

		pkt := newPacket(sess, eMsg, sourceJobID, isProto, targetName, token)
		pkt.Payload = payload

		return pkt.SerializeTo(buf)
	}
}

// Send is the primary method for transmitting data. It handles job registration,
// buffer pooling, and builder execution.
func (s *Socket) Send(ctx context.Context, build PayloadBuilder, opts ...SendOption) error {
	if s.closed.Load() {
		return ErrClosed
	}

	cfg := &SendConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	sess := s.session
	jobID := s.registerJob(ctx, cfg.Callback)

	buf := s.getBuffer()
	defer s.putBuffer(buf)

	if err := build(sess, buf, jobID, cfg.Token); err != nil {
		s.abortJob(jobID, err, cfg.Callback)
		return fmt.Errorf("socket: build failed: %w", err)
	}

	if err := s.conn.Send(ctx, buf.Bytes()); err != nil {
		s.abortJob(jobID, err, cfg.Callback)
		return fmt.Errorf("socket: send failed: %w", err)
	}

	return nil
}

// SendRaw is a helper for raw messages.
func (s *Socket) SendRaw(ctx context.Context, eMsg enums.EMsg, payload []byte, opts ...SendOption) error {
	return s.Send(ctx, Raw(eMsg, payload), opts...)
}

// SendProto is a high-level helper for Protobuf messages.
func (s *Socket) SendProto(ctx context.Context, eMsg enums.EMsg, req proto.Message, opts ...SendOption) error {
	return s.Send(ctx, Proto(eMsg, req), opts...)
}

// SendUnified is a high-level helper for Unified Service calls.
func (s *Socket) SendUnified(ctx context.Context, method string, req proto.Message, opts ...SendOption) error {
	return s.Send(ctx, Unified(method, req), opts...)
}

// SendSync blocks until a response is received or the context is canceled.
func (s *Socket) SendSync(ctx context.Context, build PayloadBuilder, opts ...SendOption) (*protocol.Packet, error) {
	type result struct {
		pkt *protocol.Packet
		err error
	}

	resCh := make(chan result, 1)
	cb := func(pkt *protocol.Packet, err error) {
		resCh <- result{pkt, err}
	}

	if err := s.Send(ctx, build, append(opts, WithCallback(cb))...); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-resCh:
		return res.pkt, res.err
	}
}

func newPacket(
	sess Session,
	eMsg enums.EMsg,
	jobID uint64,
	isProto bool,
	jobName, token string,
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

		if jobName != "" {
			hdr.Proto.TargetJobName = proto.String(jobName)
		}

		if token != "" {
			hdr.Proto.WgToken = proto.String(token)
		}

		pkt.Header = hdr
	} else {
		hdr := protocol.NewMsgHdrExtended(eMsg, steamID, sessionID)
		hdr.SourceJobID = jobID
		pkt.Header = hdr
	}

	return pkt
}

func (s *Socket) registerJob(ctx context.Context, cb jobs.Callback[*protocol.Packet]) uint64 {
	if cb == nil {
		return protocol.NoJob
	}

	id := s.jobManager.NextID()

	// Clean up job if context expires or socket closes
	var once sync.Once

	stopRequest := context.AfterFunc(ctx, func() {
		s.jobManager.Resolve(id, nil, ctx.Err())
	})

	_ = s.jobManager.Add(id, func(response *protocol.Packet, err error) {
		once.Do(func() {
			stopRequest()
			cb(response, err)
		})
	})

	return id
}

func (s *Socket) abortJob(id uint64, err error, cb jobs.Callback[*protocol.Packet]) {
	if cb != nil && id != protocol.NoJob {
		s.jobManager.Resolve(id, nil, err)
	}
}

func (s *Socket) getBuffer() *bytes.Buffer {
	buf, _ := s.bufferPool.Get().(*bytes.Buffer)
	if buf == nil {
		return new(bytes.Buffer)
	}

	buf.Reset()

	return buf
}

func (s *Socket) putBuffer(buf *bytes.Buffer) {
	if buf.Cap() <= 128*1024 { // Don't pool excessively large buffers
		s.bufferPool.Put(buf)
	}
}
