// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package connector

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/network"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
)

type reconnectKeyType struct{}

var reconnectKey = reconnectKeyType{}

// connectorError implements the api.RetriableError interface for socket network errors.
type connectorError struct {
	msg       string
	retriable bool
}

func (e *connectorError) Error() string     { return e.msg }
func (e *connectorError) IsRetriable() bool { return e.retriable }

var (
	// ErrClosed is returned when sending a message with a closed connector.
	ErrClosed = &connectorError{msg: "connector: instance is permanently closed", retriable: false}

	// ErrDisconnected is returned when sending a message but the transport is not active.
	ErrDisconnected = &connectorError{msg: "connector: not connected to any CM server", retriable: true}

	// ErrAlreadyConnecting is returned if a connection attempt is already in progress.
	ErrAlreadyConnecting = &connectorError{msg: "connector: connection attempt already in progress", retriable: true}

	// ErrUnsupportedType is returned when the transport protocol (e.g. "udp") is not registered.
	ErrUnsupportedType = &connectorError{msg: "connector: unsupported transport protocol", retriable: false}

	// ErrReconnectionFailed is emitted when the reconnect loop exhausts all attempts.
	ErrReconnectionFailed = &connectorError{
		msg:       "connector: reconnection failed after maximum attempts",
		retriable: false,
	}
)

// Config aggregates configuration for the connector's behavior.
type Config struct {
	// Dialers maps protocol types (such as "tcp" or "websockets") to their dialing functions.
	Dialers map[string]Dialer
	// ReconnectPolicy defines the strategy for recovering from connection drops.
	ReconnectPolicy ReconnectPolicy
	// ConnectTimeout is the maximum duration allowed to establish a raw socket.
	ConnectTimeout time.Duration
	// ProxyURL is the proxy server URL used for routing connection traffic.
	ProxyURL string
	// Headers defines optional HTTP headers used during the initial WebSocket handshake.
	Headers http.Header
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
	// Endpoint is the primary connection address (host:port).
	Endpoint string
	// Type defines the protocol transport type (such as "tcp" or "websockets").
	Type string
	// Load is the server load metric reported by Steam directory.
	Load float64
	// Realm is the Steam server realm (such as "steamglobal").
	Realm string
}

// Dialer defines a function for establishing various network connections.
type Dialer func(ctx context.Context, logger log.Logger, endpoint, proxyURL string, headers http.Header) (network.Connection, error)

// DefaultDialers provides implementations for TCP and WebSockets.
func DefaultDialers() map[string]Dialer {
	return map[string]Dialer{
		"tcp": func(ctx context.Context, l log.Logger, s, p string, _ http.Header) (network.Connection, error) {
			return network.NewTCP(ctx, l, s, p, SteamFramer{})
		},
		"netfilter": func(ctx context.Context, l log.Logger, s, p string, _ http.Header) (network.Connection, error) {
			return network.NewTCP(ctx, l, s, p, SteamFramer{})
		},
		"websockets": func(ctx context.Context, l log.Logger, s, p string, h http.Header) (network.Connection, error) {
			u := url.URL{Scheme: "wss", Host: s, Path: "/cmsocket/"}
			return network.NewWS(ctx, l, u.String(), p, h)
		},
	}
}

// ReconnectPolicy defines the strategy for recovering from network drops.
type ReconnectPolicy struct {
	// MaxAttempts is the maximum number of reconnect retries allowed before failing.
	MaxAttempts int
	// InitialBackoff is the starting delay before the first reconnection attempt.
	InitialBackoff time.Duration
	// MaxBackoff is the maximum retry delay boundary.
	MaxBackoff time.Duration
	// BackoffFactor is the multiplier used to increase the retry delay exponentially.
	BackoffFactor float64
	// ServerSelector selects a CMServer from the pool during reconnect cycles.
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

			return servers[rand.IntN(len(servers))] //nolint:gosec
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
	incoming chan *protocol.InboundMessage

	conn            network.Connection
	isConnecting    atomic.Bool
	reconnectCancel context.CancelFunc
	lastServer      CMServer
	servers         []CMServer
}

// New initializes a new Connector with a lifecycle tied to the provided context.
func New(cfg Config, logger log.Logger) *Connector {
	ctx, cancel := context.WithCancel(context.Background())

	c := &Connector{
		cfg:      cfg,
		ctx:      ctx,
		cancel:   cancel,
		incoming: make(chan *protocol.InboundMessage, 100),
		logger:   logger.With(log.Component("connector")),
		servers:  make([]CMServer, 0),
	}

	return c
}

// UpdateLogger updates the logger used by the connector.
func (c *Connector) UpdateLogger(logger log.Logger) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.logger = logger.With(log.Component("connector"))
}

// Done returns a channel that is closed if the connector is permanently closed.
func (c *Connector) Done() <-chan struct{} {
	return c.ctx.Done()
}

// C returns a channel for incoming network data.
func (c *Connector) C() <-chan *protocol.InboundMessage {
	return c.incoming
}

// IsConnected reports weather the connection is established.
func (c *Connector) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn != nil
}

func (c *Connector) cancelReconnect() {
	c.mu.Lock()
	if r := c.reconnectCancel; r != nil {
		r()

		c.reconnectCancel = nil
	}

	c.mu.Unlock()
}

// Connect establishes a connection to a specific CM server.
// If an active connection exists, it is closed before the new one is opened.
//
// It returns [ErrAlreadyConnecting] if a connection attempt is already in progress,
// or [ErrUnsupportedType] if the requested server protocol is not registered.
func (c *Connector) Connect(ctx context.Context, server CMServer) error {
	if ctx.Value(reconnectKey) == nil {
		c.cancelReconnect()
	}

	if !c.isConnecting.CompareAndSwap(false, true) {
		return ErrAlreadyConnecting
	}

	defer c.isConnecting.Store(false)

	dialer, ok := c.cfg.Dialers[server.Type]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnsupportedType, server.Type)
	}

	conn, err := dialer(ctx, c.getLogger(), server.Endpoint, c.cfg.ProxyURL, c.cfg.Headers)
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

	go c.monitorConnection(conn)

	c.getLogger().Info("Transport connected", log.String("endpoint", server.Endpoint), log.Int64("conn_id", conn.ID()))

	return nil
}

// SetEncryptionKey attempts to enable symmetric encryption on the active transport.
func (c *Connector) SetEncryptionKey(key []byte) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if enc, ok := c.conn.(network.Encryptable); ok {
		return enc.SetCipher(NewSteamCipher(key))
	}

	return false
}

// Send transmits binary data through the currently active connection.
//
// It returns [ErrClosed] if the connector is closed, or [ErrDisconnected] if
// there is no active Connection established.
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

// monitorConnection pipes events from the network connection into the connector.
func (c *Connector) monitorConnection(conn network.Connection) {
	msgChan := conn.Messages()
	errChan := conn.Errors()

	for {
		select {
		case msg, ok := <-msgChan:
			if !ok {
				msgChan = nil
				continue
			}

			inbound := &protocol.InboundMessage{
				Data:       msg,
				ReceivedAt: time.Now(),
				Transport:  protocol.MapConnectionToTransport(conn.Name()),
			}

			select {
			case c.incoming <- inbound:
			case <-c.ctx.Done():
				return
			}

		case err, ok := <-errChan:
			if !ok {
				errChan = nil
				continue
			}

			c.getLogger().Error("Transport error", log.Err(err))

		case <-conn.Closed():
			c.handleDisconnect(conn)
			return
		case <-c.ctx.Done():
			return
		}
	}
}

func (c *Connector) handleDisconnect(closedConn network.Connection) {
	c.mu.Lock()
	if c.conn != closedConn {
		c.mu.Unlock()
		return
	}

	c.conn = nil
	policy := c.cfg.ReconnectPolicy

	if c.ctx.Err() != nil || policy.MaxAttempts <= 0 {
		c.mu.Unlock()
		return
	}

	if c.reconnectCancel != nil {
		c.reconnectCancel()
	}

	reconCtx, cancel := context.WithCancel(c.ctx)
	c.reconnectCancel = cancel
	c.mu.Unlock()

	go c.reconnectLoop(reconCtx)
}

// reconnectLoop manages exponential backoff and server selection during outages.
func (c *Connector) reconnectLoop(ctx context.Context) {
	c.mu.RLock()
	policy := c.cfg.ReconnectPolicy
	backoff := policy.InitialBackoff
	last := c.lastServer
	c.mu.RUnlock()

	c.getLogger().Info("Reconnection loop started")

	for att := 1; att <= policy.MaxAttempts; att++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		c.mu.RLock()
		target := policy.ServerSelector(c.servers)
		c.mu.RUnlock()

		if target.Endpoint == "" {
			target = last
		}

		dialCtx, dialCancel := context.WithTimeout(ctx, c.cfg.ConnectTimeout)
		dialCtx = context.WithValue(dialCtx, reconnectKey, true)
		err := c.Connect(dialCtx, target)

		dialCancel()

		if err == nil {
			c.getLogger().Info("Reconnection successful", log.Int("attempts", att))
			return
		}

		c.getLogger().Warn("Reconnection attempt failed", log.Err(err), log.Int("attempt", att))

		timer := time.NewTimer(backoff)
		select {
		case <-timer.C:
			backoff = min(time.Duration(float64(backoff)*policy.BackoffFactor), policy.MaxBackoff)
		case <-ctx.Done():
			timer.Stop()
			return
		}
	}

	c.getLogger().Error("Reconnection failed permanently", log.Err(ErrReconnectionFailed))
}

func (c *Connector) getLogger() log.Logger {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.logger
}
