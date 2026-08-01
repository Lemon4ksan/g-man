// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package connector manages transport dialing, active connection state, and resilient exponential backoff reconnect cycles.
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

	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/miyako/log"

	"github.com/lemon4ksan/g-man/internal/framer"
	"github.com/lemon4ksan/g-man/internal/network"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
)

type reconnectKeyType struct{}

var reconnectKey = reconnectKeyType{}

type connectorError struct {
	msg       string
	retriable bool
}

func (e *connectorError) Error() string     { return e.msg }
func (e *connectorError) IsRetriable() bool { return e.retriable }

var (
	// ErrClosed indicates an operation was attempted on a permanently closed Connector.
	ErrClosed = &connectorError{msg: "connector: instance is permanently closed", retriable: false}
	// ErrDisconnected indicates sending failed because no active transport connection exists.
	ErrDisconnected = &connectorError{msg: "connector: not connected to any CM server", retriable: true}
	// ErrAlreadyConnecting indicates a dial attempt is already actively in progress.
	ErrAlreadyConnecting = &connectorError{msg: "connector: connection attempt already in progress", retriable: true}
	// ErrUnsupportedType indicates an unregistered transport protocol was specified.
	ErrUnsupportedType = &connectorError{msg: "connector: unsupported transport protocol", retriable: false}
	// ErrReconnectionFailed indicates all exponential backoff reconnect attempts were exhausted.
	ErrReconnectionFailed = &connectorError{
		msg:       "connector: reconnection failed after maximum attempts",
		retriable: false,
	}
)

// Config configures dialers, timeouts, proxy URLs, and reconnect strategies for socket connections.
type Config struct {
	FastClient      *fast.Client
	Dialers         map[string]Dialer
	ReconnectPolicy ReconnectPolicy
	ConnectTimeout  time.Duration
	ProxyURL        string
	Headers         http.Header
}

// DefaultConfig constructs a standard configuration for Connection Manager connections.
func DefaultConfig() Config {
	return Config{
		Dialers:         DefaultDialers(),
		ReconnectPolicy: DefaultReconnectPolicy(),
		ConnectTimeout:  20 * time.Second,
	}
}

// CMServer specifies host endpoint, protocol type, and load metrics for a Steam Connection Manager.
type CMServer struct {
	Endpoint string
	Type     string
	Load     float64
	Realm    string
}

// Dialer defines the signature for establishing network connections to Steam CM servers.
type Dialer func(ctx context.Context, logger log.Logger, endpoint, proxyURL string, headers http.Header) (network.Connection, error)

// DefaultDialers returns default dialer implementations for TCP and WebSocket protocols.
func DefaultDialers() map[string]Dialer {
	return BuildDialers(nil)
}

// BuildDialers constructs protocol dialers using fastClient if provided.
func BuildDialers(fastClient *fast.Client) map[string]Dialer {
	return map[string]Dialer{
		"tcp": func(ctx context.Context, l log.Logger, s, p string, _ http.Header) (network.Connection, error) {
			if fastClient != nil {
				return network.NewTCPWithDialer(ctx, l, s, p, framer.SteamFramer{}, fastClient.DialContext)
			}

			return network.NewTCP(ctx, l, s, p, framer.SteamFramer{})
		},
		"netfilter": func(ctx context.Context, l log.Logger, s, p string, _ http.Header) (network.Connection, error) {
			if fastClient != nil {
				return network.NewTCPWithDialer(ctx, l, s, p, framer.SteamFramer{}, fastClient.DialContext)
			}

			return network.NewTCP(ctx, l, s, p, framer.SteamFramer{})
		},
		"websockets": func(ctx context.Context, l log.Logger, s, p string, h http.Header) (network.Connection, error) {
			u := url.URL{Scheme: "wss", Host: s, Path: "/cmsocket/"}
			if fastClient != nil {
				return network.NewWSWithFastClient(ctx, l, u.String(), p, h, fastClient)
			}

			return network.NewWS(ctx, l, u.String(), p, h)
		},
	}
}

// ReconnectPolicy configures automatic retry backoffs and server selection strategies.
type ReconnectPolicy struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	BackoffFactor  float64
	ServerSelector func([]CMServer) CMServer
}

// DefaultReconnectPolicy provides an exponential backoff policy with randomized server selection.
func DefaultReconnectPolicy() ReconnectPolicy {
	return ReconnectPolicy{
		MaxAttempts:    0,
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     30 * time.Second,
		BackoffFactor:  2.0,
		ServerSelector: func(servers []CMServer) CMServer {
			if len(servers) == 0 {
				return CMServer{}
			}

			return servers[rand.IntN(len(servers))]
		},
	}
}

// Connector maintains active socket connection states and executes automatic reconnections on transport failures.
//
// Thread Safety:
//   - Safe for concurrent use across all public methods.
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
	onReconnect     func(ctx context.Context)
}

// New constructs a Connector instance tied to background lifecycle context.
func New(cfg Config, logger log.Logger) *Connector {
	ctx, cancel := context.WithCancel(context.Background())

	if cfg.Dialers == nil {
		cfg.Dialers = BuildDialers(cfg.FastClient)
	}

	return &Connector{
		cfg:      cfg,
		ctx:      ctx,
		cancel:   cancel,
		incoming: make(chan *protocol.InboundMessage, 100),
		logger:   logger.With(log.Component("connector")),
		servers:  make([]CMServer, 0),
	}
}

// UpdateLogger thread-safely updates the logger context.
func (c *Connector) UpdateLogger(logger log.Logger) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.logger = logger.With(log.Component("connector"))
}

// Done returns a channel closed when the connector is permanently shutdown.
func (c *Connector) Done() <-chan struct{} { return c.ctx.Done() }

// C returns the receive channel streaming inbound network messages.
func (c *Connector) C() <-chan *protocol.InboundMessage { return c.incoming }

// IsConnected reports whether an active connection exists.
func (c *Connector) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.conn != nil
}

// Connect dials a specific Connection Manager server.
func (c *Connector) Connect(ctx context.Context, server CMServer) error {
	c.mu.RLock()
	alreadyConnected := c.conn != nil && c.lastServer.Endpoint == server.Endpoint
	c.mu.RUnlock()

	if alreadyConnected {
		return nil
	}

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

// SetEncryptionKey enables symmetric AES encryption on the active transport.
func (c *Connector) SetEncryptionKey(key []byte) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if enc, ok := c.conn.(network.Encryptable); ok {
		return enc.SetCipher(framer.NewSteamCipher(key))
	}

	return false
}

// Send transmits binary data over the active socket connection.
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

// SetOnReconnect registers a callback function executed after a successful auto-reconnection cycle.
func (c *Connector) SetOnReconnect(fn func(ctx context.Context)) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.onReconnect = fn
}

// UpdateServers updates the pool of candidate Connection Manager servers used during reconnect cycles.
func (c *Connector) UpdateServers(servers []CMServer) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.servers = servers
}

// Disconnect gracefully terminates the active connection.
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

// Close permanently shuts down the connector.
func (c *Connector) Close() error {
	c.cancel()
	c.closed.Store(true)

	return c.Disconnect()
}

func (c *Connector) cancelReconnect() {
	c.mu.Lock()
	if r := c.reconnectCancel; r != nil {
		r()

		c.reconnectCancel = nil
	}

	c.mu.Unlock()
}

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

	if c.ctx.Err() != nil || policy.MaxAttempts < 0 {
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

func (c *Connector) reconnectLoop(ctx context.Context) {
	c.mu.RLock()
	policy := c.cfg.ReconnectPolicy
	backoff := policy.InitialBackoff
	last := c.lastServer
	c.mu.RUnlock()

	c.getLogger().Info("Reconnection loop started")

	for att := 1; policy.MaxAttempts == 0 || att <= policy.MaxAttempts; att++ {
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

			c.mu.RLock()
			onRecon := c.onReconnect
			c.mu.RUnlock()

			if onRecon != nil {
				go onRecon(c.ctx)
			}

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
