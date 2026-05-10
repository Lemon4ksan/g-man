// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steam

import (
	"context"
	"errors"
	"fmt"

	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
)

type sessionRefresher interface {
	Refresh(ctx context.Context) error
	Clients() (unified, socketAPI *service.Client)
}

// ServiceRouter encapsulates transport selection and automatic retries logic.
type ServiceRouter struct {
	session sessionRefresher
	socket  SocketProvider
}

// NewServiceRouter creates a new service router.
func NewServiceRouter(sess sessionRefresher, sock SocketProvider) *ServiceRouter {
	return &ServiceRouter{
		session: sess,
		socket:  sock,
	}
}

// Do performs the request, automatically choosing between SocketProvider and HTTP,
// and handles transparent session updates if necessary.
func (r *ServiceRouter) Do(ctx context.Context, req *tr.Request) (*tr.Response, error) {
	resp, err := r.perform(ctx, req)

	if err != nil && errors.Is(err, api.ErrSessionExpired) {
		if refreshErr := r.session.Refresh(ctx); refreshErr != nil {
			return nil, fmt.Errorf("router: auto-refresh failed: %w", refreshErr)
		}

		return r.perform(ctx, req)
	}

	return resp, err
}

func (r *ServiceRouter) perform(ctx context.Context, req *tr.Request) (*tr.Response, error) {
	uClient, sClient := r.session.Clients()

	isConnected := r.socket.IsConnected()
	_, isSocketCompatible := req.Target().(tr.SocketTarget)

	var selected service.Doer

	if isConnected && isSocketCompatible {
		selected = sClient
	} else {
		selected = uClient
	}

	if selected == nil {
		return nil, errors.New("router: no available transport")
	}

	return selected.Do(ctx, req)
}
