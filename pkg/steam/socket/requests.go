// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socket

import (
	"bytes"
	"context"
	"fmt"

	"github.com/lemon4ksan/g-man/pkg/jobs"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/internal/session"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/protocol"
	"google.golang.org/protobuf/proto"
)

// SendConfig contains parameters for sending a message.
type SendConfig struct {
	Callback jobs.Callback[*protocol.Packet]
	Token    string
}

// SendOption defines a functional option for SendConfig.
type SendOption func(*SendConfig)

// WithCallback adds a callback to asynchronously wait for a response to the sent packet.
func WithCallback(cb jobs.Callback[*protocol.Packet]) SendOption {
	return func(c *SendConfig) {
		c.Callback = cb
	}
}

func WithToken(token string) SendOption {
	return func(c *SendConfig) {
		c.Token = token
	}
}

// PayloadBuilder describes a function that assembles a binary packet body with the required headers.
type PayloadBuilder func(sess session.Session, buf *bytes.Buffer, sourceJobID uint64, token string) error

// Proto creates a PayloadBuilder to send a Protobuf message.
func Proto(eMsg protocol.EMsg, req proto.Message) PayloadBuilder {
	return func(sess session.Session, buf *bytes.Buffer, sourceJobID uint64, token string) (err error) {
		pkt := newPacket(sess, eMsg, sourceJobID, true, "", token)
		pkt.Payload, err = proto.Marshal(req)
		if err != nil {
			return
		}
		return pkt.SerializeTo(buf)
	}
}

// Unified creates a PayloadBuilder to call the Unified Service (e.g. "Player.GetGameBadgeLevels#1").
func Unified(method string, req proto.Message) PayloadBuilder {
	return func(sess session.Session, buf *bytes.Buffer, sourceJobID uint64, token string) (err error) {
		pkt := newPacket(sess, protocol.EMsg_ServiceMethodCallFromClient, sourceJobID, true, method, token)
		pkt.Payload, err = proto.Marshal(req)
		if err != nil {
			return
		}
		return pkt.SerializeTo(buf)
	}
}

// Raw creates a PayloadBuilder to send a plain byte array (e.g. for encryption).
func Raw(eMsg protocol.EMsg, payload []byte) PayloadBuilder {
	return func(sess session.Session, buf *bytes.Buffer, sourceJobID uint64, _ string) error {
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
	return func(sess session.Session, buf *bytes.Buffer, sourceJobID uint64, token string) (err error) {
		isProto := targetName != ""
		pkt := newPacket(sess, eMsg, sourceJobID, isProto, targetName, token)
		pkt.Payload = payload
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
		return ErrClosed
	}

	jobID := s.registerJob(ctx, cfg.Callback)

	buf := s.bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer func() {
		if buf.Cap() <= 64*1024 {
			s.bufferPool.Put(buf)
		}
	}()

	if err := build(sess, buf, jobID, cfg.Token); err != nil {
		if cfg.Callback != nil {
			s.jobManager.Resolve(jobID, nil, err)
		}
		return fmt.Errorf("build failed: %w", err)
	}

	if err := sess.Send(ctx, buf.Bytes()); err != nil {
		if cfg.Callback != nil {
			s.jobManager.Resolve(jobID, nil, err)
		}
		return fmt.Errorf("send failed: %w", err)
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

// newPacket is a factory method that initializes a protocol.Packet with
// correct headers (Proto vs Extended) based on the session state and message type.
func newPacket(sess session.Session, eMsg protocol.EMsg, jobID uint64, isProto bool, jobName, token string) *protocol.Packet {
	var steamID uint64
	var sessionID int32
	if sess != nil {
		steamID = sess.SteamID()
		sessionID = sess.SessionID()
	}

	// ClientHello is a special case: it must be sent with 0 IDs even if
	// the session objects exist (e.g., during reconnection).
	if eMsg == protocol.EMsg_ClientHello {
		steamID, sessionID = 0, 0
	}

	pkt := &protocol.Packet{EMsg: eMsg, IsProto: isProto}
	if isProto {
		hdr := protocol.NewMsgHdrProtoBuf(eMsg, steamID, sessionID)
		hdr.Proto.JobidSource = &jobID
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

// registerJob assigns a unique Job ID to the request and registers a callback
// in the job manager. Returns protocol.NoJob (0) if no callback is provided.
func (s *Socket) registerJob(ctx context.Context, cb jobs.Callback[*protocol.Packet]) uint64 {
	if cb == nil {
		return protocol.NoJob
	}
	id := s.jobManager.NextID()
	_ = s.jobManager.Add(id, func(response *protocol.Packet, err error) {
		var eMsg protocol.EMsg
		if response != nil {
			eMsg = response.EMsg
		}
		defer s.recoverPanic(eMsg)
		cb(response, err)
	}, jobs.WithContext[*protocol.Packet](ctx))
	return id
}
