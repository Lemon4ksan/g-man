// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mock

import (
	"context"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/client/session"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	"github.com/stretchr/testify/mock"

	"google.golang.org/protobuf/proto"
)

type Socket struct {
	session.SocketProvider
	mock.Mock
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
