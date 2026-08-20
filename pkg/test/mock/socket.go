// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mock

import (
	"context"

	"time"

	"github.com/stretchr/testify/mock"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/internal/client/session"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	"github.com/lemon4ksan/foundation/async/log"
)

type Socket struct {
	session.SocketProvider
	mock.Mock
	onReconnect func(ctx context.Context)
}

func (m *Socket) OnDefault() *Socket {
	m.On("RegisterMsgHandler", mock.Anything, mock.Anything).Return().Maybe()
	m.On("RegisterServiceHandler", mock.Anything, mock.Anything).Return().Maybe()
	m.On("UpdateLogger", mock.Anything).Return().Maybe()
	m.On("UpdateServers", mock.Anything).Return().Maybe()
	m.On("Disconnect").Return(nil).Maybe()
	m.On("Close").Return(nil).Maybe()
	m.On("IsConnected").Return(false).Maybe()

	return m
}

func (m *Socket) SetOnReconnect(fn func(ctx context.Context)) {
	m.onReconnect = fn
}

func (m *Socket) IsConnected() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *Socket) UpdateLogger(logger log.Logger) {
	m.Called(logger)
}

func (m *Socket) UpdateServers(servers []socket.CMServer) {
	m.Called(servers)
}

func (m *Socket) Send(ctx context.Context, build socket.PayloadBuilder, opts ...socket.SendOption) error {
	args := m.Called(ctx, build, opts)
	return args.Error(0)
}

func (m *Socket) SendSync(ctx context.Context, build socket.PayloadBuilder, opts ...socket.SendOption) (*protocol.Packet, error) {
	args := m.Called(ctx, build, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*protocol.Packet), args.Error(1)
}

func (m *Socket) RegisterMsgHandler(eMsg enums.EMsg, handler socket.Handler) {
	m.Called(eMsg, handler)
}

func (m *Socket) RegisterServiceHandler(method string, handler socket.Handler) {
	m.Called(method, handler)
}

func (m *Socket) Disconnect() error {
	args := m.Called()
	return args.Error(0)
}

func (m *Socket) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *Socket) Connect(ctx context.Context, server socket.CMServer) error {
	args := m.Called(ctx, server)
	return args.Error(0)
}

func (m *Socket) SetEncryptionKey(key []byte) bool {
	args := m.Called(key)
	return args.Bool(0)
}

func (m *Socket) StartHeartbeat(d time.Duration) error {
	args := m.Called(d)
	return args.Error(0)
}

func (m *Socket) Session() socket.Session {
	args := m.Called()
	sess, _ := args.Get(0).(socket.Session)

	return sess
}

func (m *Socket) SendProto(
	ctx context.Context,
	eMsg enums.EMsg,
	req proto.Message,
	opts ...socket.SendOption,
) error {
	args := m.Called(ctx, eMsg, req, opts)
	return args.Error(0)
}

func (m *Socket) SendRaw(
	ctx context.Context,
	eMsg enums.EMsg,
	payload []byte,
	opts ...socket.SendOption,
) error {
	args := m.Called(ctx, eMsg, payload, opts)
	return args.Error(0)
}
