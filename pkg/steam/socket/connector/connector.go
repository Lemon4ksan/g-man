// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package connector manages raw network connectivity to Steam CM servers.

It abstracts the underlying transport (TCP or WebSockets) and provides
automatic reconnection logic. Its primary responsibility is to maintain
a "live pipe" and emit raw, decrypted NetMessages into a Go channel.

Key features:
  - Exponential backoff on connection failure.
  - Pluggable dialers for different protocols.
  - Encryption handshake handling.
  - Transport-agnostic data transmission.
*/
package connector

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/socket/network"
)

var (
	// ErrClosed is returned when sending a message with a closed connector.
	ErrClosed = errors.New("connector: instance is permanently closed")

	// ErrDisconnected is returned when sending a message but the transport is not active.
	ErrDisconnected = errors.New("connector: not connected to any CM server")

	// ErrAlreadyConnecting is returned if a connection attempt is already in progress.
	ErrAlreadyConnecting = errors.New("connector: connection attempt already in progress")

	// ErrUnsupportedType is returned when the transport protocol (e.g. "udp") is not registered.
	ErrUnsupportedType = errors.New("connector: unsupported transport protocol")

	// ErrReconnectionFailed is emitted when the reconnect loop exhausts all attempts.
	ErrReconnectionFailed = errors.New("connector: reconnection failed after maximum attempts")
)

// Config aggregates configuration for the connector's behavior.
type Config struct {
	Dialers         map[string]Dialer
	ReconnectPolicy ReconnectPolicy
	ConnectTimeout  time.Duration
}

// DefaultConfig returns a standard configuration for Steam CM connections.
func DefaultConfig() Config {
	return Config{
		Dialers:         DefaultDialers(),
		ReconnectPolicy: DefaultReconnectPolicy(),
		ConnectTimeout:  20 * time.Second,
	}
}

// CMServer represents a Steam Connection Manager server endpoint.
type CMServer struct {
	Endpoint string
	Type     string
	Load     float64
	Realm    string
}

// Dialer defines a function for establishing various network connections.
type Dialer func(ctx context.Context, nh network.Handler, logger log.Logger, endpoint string) (network.Connection, error)

// DefaultDialers provides implementations for TCP and WebSockets.
func DefaultDialers() map[string]Dialer {
	return map[string]Dialer{
		"tcp": func(ctx context.Context, nh network.Handler, l log.Logger, s string) (network.Connection, error) {
			return network.NewTCP(ctx, nh, l, s)
		},
		"websockets": func(ctx context.Context, nh network.Handler, l log.Logger, s string) (network.Connection, error) {
			return network.NewWS(ctx, nh, l, s, nil)
		},
	}
}

// ReconnectPolicy defines the strategy for recovering from network drops.
type ReconnectPolicy struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	BackoffFactor  float64
	ServerSelector func([]CMServer) CMServer
}

// DefaultReconnectPolicy provides a standard exponential backoff strategy.
func DefaultReconnectPolicy() ReconnectPolicy {
	return ReconnectPolicy{
		MaxAttempts:    10,
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     30 * time.Second,
		BackoffFactor:  2.0,
		ServerSelector: func(servers []CMServer) CMServer {
			if len(servers) == 0 {
				return CMServer{}
			}

			return servers[0]
		},
	}
}

// Connector manages the lifecycle of a single Steam CM connection.
// It acts as a resilient proxy that handles automatic reconnections and frames routing.
type Connector struct {
	cfg    Config
	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
	closed atomic.Bool

	logger   log.Logger
	incoming chan []byte

	conn         network.Connection
	isConnecting atomic.Bool
	lastServer   CMServer
	servers      []CMServer
}

type DataHandler interface {
	// OnNetMessage is called when a complete message is framed and received.
	OnNetMessage(msg network.NetMessage)
}

// New initializes a new Connector with a lifecycle tied to the provided context.
func New(cfg Config, logger log.Logger) *Connector {
	ctx, cancel := context.WithCancel(context.Background())

	c := &Connector{
		cfg:      cfg,
		ctx:      ctx,
		cancel:   cancel,
		incoming: make(chan []byte, 100),
		logger:   logger.With(log.Component("connector")),
		servers:  make([]CMServer, 0),
	}

	return c
}

// Done returns a channel that is closed if the connector is permanently closed.
func (c *Connector) Done() <-chan struct{} {
	return c.ctx.Done()
}

// C returns a channel for incoming network data.
func (c *Connector) C() <-chan []byte {
	return c.incoming
}

// IsConnected reports weather the connection is established.
func (c *Connector) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn != nil
}

// Connect establishes a connection to a specific CM server.
// If an active connection exists, it is closed before the new one is opened.
func (c *Connector) Connect(ctx context.Context, server CMServer) error {
	if !c.isConnecting.CompareAndSwap(false, true) {
		return ErrAlreadyConnecting
	}

	defer c.isConnecting.Store(false)

	dialer, ok := c.cfg.Dialers[server.Type]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnsupportedType, server.Type)
	}

	conn, err := dialer(ctx, c, c.logger, server.Endpoint)
	if err != nil {
		return err
	}

	c.mu.Lock()
	if c.conn != nil {
		_ = c.conn.Close()
	}

	c.conn = conn
	c.lastServer = server
	c.mu.Unlock()

	c.logger.Info("Transport connected", log.String("endpoint", server.Endpoint), log.Int64("conn_id", conn.ID()))

	return nil
}

// SetEncryptionKey attempts to enable symmetric encryption on the active transport.
func (c *Connector) SetEncryptionKey(key []byte) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if enc, ok := c.conn.(network.Encryptable); ok {
		return enc.SetEncryptionKey(key)
	}

	return false
}

// Send transmits binary data through the currently active connection.
func (c *Connector) Send(ctx context.Context, data []byte) error {
	if c.closed.Load() {
		return ErrClosed
	}

	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil {
		return ErrDisconnected
	}

	return conn.Send(ctx, data)
}

// UpdateServers refreshes the internal CM server list used for reconnection selection.
func (c *Connector) UpdateServers(servers []CMServer) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.servers = servers
}

// Disconnect gracefully closes the active connection and prevents automatic reconnection
// until Connect() is called manually again.
func (c *Connector) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil
	}

	err := c.conn.Close()
	c.conn = nil

	return err
}

// Close permanently shuts down the connector and cancels all background tasks.
func (c *Connector) Close() error {
	c.cancel()
	c.closed.Store(true)
	return c.Disconnect()
}

// OnNetMessage routes raw incoming messages to the registered data handler.
func (c *Connector) OnNetMessage(msg network.NetMessage) {
	select {
	case c.incoming <- msg:
	case <-c.ctx.Done():
	}
}

// OnNetError logs underlying transport errors.
func (c *Connector) OnNetError(err error) {
	c.logger.Error("Transport error", log.Err(err))
}

// OnNetClose handles connection loss by notifying the bus and initiating the reconnect loop.
func (c *Connector) OnNetClose() {
	c.mu.Lock()
	c.conn = nil
	policy := c.cfg.ReconnectPolicy
	c.mu.Unlock()

	if c.ctx.Err() == nil && policy.MaxAttempts > 0 {
		go c.reconnectLoop()
	}
}

// reconnectLoop manages exponential backoff and server selection during outages.
func (c *Connector) reconnectLoop() {
	c.mu.RLock()
	policy := c.cfg.ReconnectPolicy
	backoff := policy.InitialBackoff
	last := c.lastServer
	c.mu.RUnlock()

	c.logger.Info("Reconnection loop started")

	for att := 1; att <= policy.MaxAttempts; att++ {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		c.mu.RLock()
		target := policy.ServerSelector(c.servers)
		c.mu.RUnlock()

		if target.Endpoint == "" {
			target = last
		}

		dialCtx, dialCancel := context.WithTimeout(c.ctx, c.cfg.ConnectTimeout)
		err := c.Connect(dialCtx, target)

		dialCancel()

		if err == nil {
			c.logger.Info("Reconnection successful", log.Int("attempts", att))
			return
		}

		c.logger.Warn("Reconnection attempt failed", log.Err(err), log.Int("attempt", att))

		timer := time.NewTimer(backoff)
		select {
		case <-timer.C:
			backoff = min(time.Duration(float64(backoff)*policy.BackoffFactor), policy.MaxBackoff)
		case <-c.ctx.Done():
			timer.Stop()
			return
		}
	}

	c.logger.Error("Reconnection failed permanently", log.Err(ErrReconnectionFailed))
}
