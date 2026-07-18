// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package router implements a network transport router for the Steam client.
package router

import (
	"context"
	"errors"
	"fmt"

	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
)

// ErrNoActiveClient is returned by [ServiceRouter.Do] when no active transport client is available.
var ErrNoActiveClient = errors.New("router: no active client for target transport")

// SessionRefresher defines the interface for managing session tokens and targets.
// It is utilized by [ServiceRouter] to handle automatic session updates when expired.
type SessionRefresher interface {
	// Refresh updates session credentials using the provided context.
	Refresh(ctx context.Context) error
	// Unified returns the unified HTTP WebAPI service client.
	Unified() *service.Client
	// Socket returns the low-level socket service client.
	Socket() *service.Client
}

// StateProvider defines network connectivity check operations used by [ServiceRouter].
type StateProvider interface {
	// IsConnected reports whether the network socket is actively connected.
	IsConnected() bool
}

// TransportType represents the selected network channel for request execution.
type TransportType int

const (
	// TransportWebAPI routes requests over HTTPS WebAPI.
	TransportWebAPI TransportType = iota
	// TransportSocket routes requests over the active socket connection.
	TransportSocket
)

// RouteMatcher determines the optimal [TransportType] for a given [tr.Request].
type RouteMatcher func(req *tr.Request) TransportType

// ServiceRouter encapsulates transport selection and automatic token refresh workflows.
// It routes requests over TCP/WebSockets or HTTP based on target compatibility and connectivity.
// Use [New] to construct a new router.
type ServiceRouter struct {
	refresher SessionRefresher
	state     StateProvider
	matcher   RouteMatcher
}

// New creates a new [ServiceRouter] with standard defaults.
// If sess or sock is nil, subsequent requests may cause runtime panics.
func New(sess SessionRefresher, sock StateProvider) *ServiceRouter {
	router := &ServiceRouter{
		refresher: sess,
		state:     sock,
	}
	router.matcher = router.DefaultRouteMatcher

	return router
}

// SetRouteMatcher configures a custom [RouteMatcher] for transport selection.
// Falls back to [ServiceRouter.DefaultRouteMatcher] if the provided matcher is nil.
func (r *ServiceRouter) SetRouteMatcher(matcher RouteMatcher) {
	if matcher == nil {
		r.matcher = r.DefaultRouteMatcher
	} else {
		r.matcher = matcher
	}
}

// DefaultRouteMatcher determines the transport type based on socket connectivity and target requirements.
// Returns [TransportSocket] if the socket is connected and the target implements [tr.SocketTarget].
// Otherwise, falls back to [TransportWebAPI].
func (r *ServiceRouter) DefaultRouteMatcher(req *tr.Request) TransportType {
	_, isSocketCompatible := req.Target().(tr.SocketTarget)
	if r.state.IsConnected() && isSocketCompatible {
		return TransportSocket
	}

	return TransportWebAPI
}

// Do executes a network request using the optimal transport type.
// Automatically attempts a [SessionRefresher.Refresh] if the request fails with [service.ErrSessionExpired].
// Returns [ErrNoActiveClient] if the resolved service client is nil.
// Aborts request execution if the context ctx is canceled.
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
