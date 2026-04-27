// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socket

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"syscall"

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
	return func(c *SendConfig) {
		c.Callback = cb
	}
}

// WithToken sets an access token for service method calls via the socket.
func WithToken(token string) SendOption {
	return func(c *SendConfig) {
		c.Token = token
	}
}

// PayloadBuilder describes a function that assembles a binary packet body with the required headers.
type PayloadBuilder func(sess Session, buf *bytes.Buffer, sourceJobID uint64, token string) error

// Proto creates a PayloadBuilder to send a Protobuf-encoded message.
func Proto(eMsg enums.EMsg, req proto.Message) PayloadBuilder {
	return func(sess Session, buf *bytes.Buffer, sourceJobID uint64, token string) (err error) {
		pkt := newPacket(sess, eMsg, sourceJobID, true, "", token)

		pkt.Payload, err = proto.Marshal(req)
		if err != nil {
			return fmt.Errorf("marshal proto: %w", err)
		}

		return pkt.SerializeTo(buf)
	}
}

// Unified creates a PayloadBuilder to call a Unified Service method (e.g. "Player.GetGameBadgeLevels#1").
func Unified(method string, req proto.Message) PayloadBuilder {
	return func(sess Session, buf *bytes.Buffer, sourceJobID uint64, token string) (err error) {
		pkt := newPacket(sess, enums.EMsg_ServiceMethodCallFromClient, sourceJobID, true, method, token)

		pkt.Payload, err = proto.Marshal(req)
		if err != nil {
			return fmt.Errorf("marshal unified proto: %w", err)
		}

		return pkt.SerializeTo(buf)
	}
}

// Raw creates a PayloadBuilder to send a plain byte array (e.g. for encryption handshakes).
func Raw(eMsg enums.EMsg, payload []byte) PayloadBuilder {
	return func(sess Session, buf *bytes.Buffer, sourceJobID uint64, _ string) error {
		pkt := newPacket(sess, eMsg, sourceJobID, false, "", "")
		pkt.Payload = payload

		return pkt.SerializeTo(buf)
	}
}

// DynamicRaw creates a PayloadBuilder that can send either unified or raw messages
// based on whether targetName is provided.
func DynamicRaw(eMsg enums.EMsg, targetName string, payload []byte) PayloadBuilder {
	return func(sess Session, buf *bytes.Buffer, sourceJobID uint64, token string) (err error) {
		isProto := targetName != ""
		pkt := newPacket(sess, eMsg, sourceJobID, isProto, targetName, token)
		pkt.Payload = payload

		return pkt.SerializeTo(buf)
	}
}

// Send constructs and transmits a payload to the Steam Connection Manager.
// It uses a buffer pool to minimize allocations.
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

	buf := s.getBuffer()
	defer s.putBuffer(buf)

	if err := build(sess, buf, jobID, cfg.Token); err != nil {
		s.abortJob(jobID, err, cfg.Callback)
		return fmt.Errorf("build failed: %w", err)
	}

	if err := sess.Send(ctx, buf.Bytes()); err != nil {
		if isFatalNetworkError(err) {
			s.handleRemoteClose()
		}

		s.abortJob(jobID, err, cfg.Callback)

		return fmt.Errorf("api: send failed: %w", err)
	}

	return nil
}

// SendProto is a helper for sending Protobuf messages.
func (s *Socket) SendProto(ctx context.Context, eMsg enums.EMsg, req proto.Message, opts ...SendOption) error {
	return s.Send(ctx, Proto(eMsg, req), opts...)
}

// SendUnified is a helper for calling Unified Service methods.
func (s *Socket) SendUnified(ctx context.Context, method string, req proto.Message, opts ...SendOption) error {
	return s.Send(ctx, Unified(method, req), opts...)
}

// SendRaw is a helper for sending raw byte payloads.
func (s *Socket) SendRaw(ctx context.Context, eMsg enums.EMsg, payload []byte, opts ...SendOption) error {
	return s.Send(ctx, Raw(eMsg, payload), opts...)
}

// SendSync performs a synchronous request and waits for a response from the server.
// This method blocks until a response is received or the context is cancelled.
// If the server never responds (e.g., due to a silent drop), this will block
// until the context timeout. It is highly recommended to use a context with a timeout.
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

// newPacket initializes a protocol.Packet with headers based on the session state.
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

	// ClientHello must be sent with 0 IDs even if a session exists (e.g. during reconnect)
	if sess != nil && eMsg != enums.EMsg_ClientHello {
		steamID = sess.SteamID()
		sessionID = sess.SessionID()
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

// registerJob registers a callback for an asynchronous response.
func (s *Socket) registerJob(ctx context.Context, cb jobs.Callback[*protocol.Packet]) uint64 {
	if cb == nil {
		return protocol.NoJob
	}

	combinedCtx, cancel := context.WithCancel(ctx)

	socketCtx := s.getContext()
	go func() {
		select {
		case <-combinedCtx.Done():
		case <-socketCtx.Done():
			cancel()
		case <-s.done:
			cancel()
		}
	}()

	id := s.jobManager.NextID()
	_ = s.jobManager.Add(id, func(response *protocol.Packet, err error) {
		defer cancel()

		var eMsg enums.EMsg
		if response != nil {
			eMsg = response.EMsg
		}

		defer s.recoverPanic(eMsg)

		cb(response, err)
	}, jobs.WithContext[*protocol.Packet](combinedCtx))

	return id
}

func (s *Socket) abortJob(id uint64, err error, cb jobs.Callback[*protocol.Packet]) {
	if cb != nil && id != protocol.NoJob {
		s.jobManager.Resolve(id, nil, err)
	}
}

func (s *Socket) getBuffer() *bytes.Buffer {
	buf, ok := s.bufferPool.Get().(*bytes.Buffer)
	if !ok {
		return new(bytes.Buffer)
	}

	buf.Reset()

	return buf
}

func (s *Socket) putBuffer(buf *bytes.Buffer) {
	if buf.Cap() <= 64*1024 {
		s.bufferPool.Put(buf)
	}
}

func isFatalNetworkError(err error) bool {
	if err == nil {
		return false
	}

	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case syscall.ECONNRESET, syscall.EPIPE, syscall.ECONNABORTED, syscall.ETIMEDOUT:
			return true
		}
	}

	return false
}
