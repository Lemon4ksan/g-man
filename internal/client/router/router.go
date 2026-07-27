// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package router implements dynamic transport selection and automated session recovery for Steam API requests.
package router

import (
	"context"
	"errors"
	"fmt"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
)

// ErrNoActiveClient indicates that no transport client is connected or available for the targeted route.
var ErrNoActiveClient = errors.New("router: no active client for target transport")

// SessionRefresher manages token refresh workflows and provides active service clients.
type SessionRefresher interface {
	Refresh(ctx context.Context) error
	Unified() *service.Client
	Socket() *service.Client
}

// StateProvider reports low-level network connectivity states.
type StateProvider interface {
	IsConnected() bool
}

// TransportType identifies the physical network channel selected for request delivery.
type TransportType int

const (
	// TransportWebAPI routes requests over HTTPS WebAPI.
	TransportWebAPI TransportType = iota
	// TransportSocket routes requests over an active TCP/WebSocket connection.
	TransportSocket
)

// RouteMatcher inspects a request to determine the appropriate transport type.
type RouteMatcher func(req *tr.Request) TransportType

// ServiceRouter routes outbound Steam network requests across WebAPI or Socket channels, transparently handling session updates.
type ServiceRouter struct {
	refresher SessionRefresher
	state     StateProvider
	matcher   RouteMatcher
}

// New constructs a ServiceRouter with default route matching logic.
func New(sess SessionRefresher, sock StateProvider) *ServiceRouter {
	router := &ServiceRouter{
		refresher: sess,
		state:     sock,
	}
	router.matcher = router.DefaultRouteMatcher

	return router
}

// SetRouteMatcher overrides the default route selection logic.
// If matcher is nil, resets to DefaultRouteMatcher.
func (r *ServiceRouter) SetRouteMatcher(matcher RouteMatcher) {
	if matcher == nil {
		r.matcher = r.DefaultRouteMatcher
	} else {
		r.matcher = matcher
	}
}

// DefaultRouteMatcher selects TransportSocket if the socket is connected and the target supports socket transport; otherwise defaults to TransportWebAPI.
func (r *ServiceRouter) DefaultRouteMatcher(req *tr.Request) TransportType {
	_, isSocketCompatible := req.Target().(tr.SocketTarget)
	if r.state.IsConnected() && isSocketCompatible {
		return TransportSocket
	}

	return TransportWebAPI
}

// Do executes a network request using the optimal transport.
// If execution fails with service.ErrSessionExpired, Do attempts a single automated session refresh and retries the request.
//
// Returns:
//   - *tr.Response on successful execution.
//   - ErrNoActiveClient if the selected transport client is nil.
//   - Context error if ctx is cancelled during request execution or refresh.
func (r *ServiceRouter) Do(ctx context.Context, req *tr.Request) (*tr.Response, error) {
	resp, err := r.perform(ctx, req)
	if err != nil && errors.Is(err, service.ErrSessionExpired) {
		if refreshErr := r.refresher.Refresh(ctx); refreshErr != nil {
			return nil, fmt.Errorf("router: auto-refresh failed: %w", refreshErr)
		}

		return r.perform(ctx, req)
	}

	return resp, err
}

func (r *ServiceRouter) perform(ctx context.Context, req *tr.Request) (*tr.Response, error) {
	var selected service.Doer

	switch r.matcher(req) {
	case TransportSocket:
		selected = r.refresher.Socket()
	case TransportWebAPI:
		selected = r.refresher.Unified()
		ctx = protocol.WithTransportType(ctx, protocol.TransportWebAPI)
	}

	if selected == nil {
		return nil, ErrNoActiveClient
	}

	if c, ok := selected.(*service.Client); ok && c == nil {
		return nil, ErrNoActiveClient
	}

	return selected.Do(ctx, req)
}
