// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package client provides the primary orchestrator for Steam network connections, authentications, request routing, and plugin modules.
package client

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/request"
	"github.com/lemon4ksan/miyako/bus"
	"github.com/lemon4ksan/miyako/generic"
	"github.com/lemon4ksan/miyako/kata"
	"github.com/lemon4ksan/miyako/log"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/internal/client/modules"
	"github.com/lemon4ksan/g-man/internal/client/router"
	"github.com/lemon4ksan/g-man/internal/client/session"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
	"github.com/lemon4ksan/g-man/pkg/steam/module"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol/enums"
	"github.com/lemon4ksan/g-man/pkg/steam/service"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
	"github.com/lemon4ksan/g-man/pkg/steam/sys/directory"
	tr "github.com/lemon4ksan/g-man/pkg/steam/transport"
	"github.com/lemon4ksan/g-man/pkg/storage"
	"github.com/lemon4ksan/g-man/pkg/storage/memory"
)

var (
	// ErrNotRunning indicates an operation was performed before calling Run.
	ErrNotRunning = errors.New("steam: client must be running (call Run() first)")
	// ErrSocketDisabled indicates socket transport was explicitly disabled in configuration.
	ErrSocketDisabled = errors.New("steam: socket transport is disabled")
	// ErrNilLogOnDetails indicates login was attempted with nil credentials.
	ErrNilLogOnDetails = errors.New("steam: logon details cannot be nil")
	// ErrAlreadyRunning indicates Run was called on an already active client.
	ErrAlreadyRunning = errors.New("steam: client is already running")
)

// Config aggregates options for client transports, socket behavior, and logon profiles.
type Config struct {
	PersonaState  enums.EPersonaState
	Socket        socket.Config
	Device        *auth.DeviceConfig
	ProxyURL      string
	DisableSocket bool
}

// DefaultConfig builds default client configuration options.
func DefaultConfig() Config {
	return Config{
		PersonaState: enums.EPersonaState_Online,
		Socket:       socket.DefaultConfig(),
	}
}

// ResolveDefaults applies proxy fallback options to socket configurations.
func (cfg *Config) ResolveDefaults() {
	cfg.Socket.Connector.ProxyURL = generic.Coalesce(cfg.Socket.Connector.ProxyURL, cfg.ProxyURL)
}

// State represents client lifecycle states.
type State int32

const (
	StateNew State = iota
	StateRunning
	StateAuthorized
	StateClosed
)

// Event represents lifecycle state transitions.
type Event int32

const (
	EventRun Event = iota
	EventAuthorize
	EventClose
)

func (s State) String() string {
	switch s {
	case StateNew:
		return "new"
	case StateRunning:
		return "running"
	case StateAuthorized:
		return "authorized"
	case StateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// Option configures Client parameters.
type Option = generic.Option[*Client]

// WithLogger assigns a logger instance.
func WithLogger(l log.Logger) Option {
	return func(c *Client) { c.logger = l.With(log.Module("steam")) }
}

// WithSession assigns a custom session manager.
func WithSession(s *session.Session) Option {
	return func(c *Client) { c.session = s }
}

// WithRouter assigns a custom service router.
func WithRouter(r *router.ServiceRouter) Option {
	return func(c *Client) { c.router = r }
}

// WithModule registers an extension module with the client.
func WithModule(m module.Module) Option {
	return func(c *Client) {
		if c.modules != nil {
			_ = c.modules.Add(m)
		} else {
			c.pendingModules = append(c.pendingModules, m)
		}
	}
}

// WithSocket assigns a socket provider implementation.
func WithSocket(sock session.SocketProvider) Option {
	return func(c *Client) { c.socket = sock }
}

// WithREST configures standard or fast REST engines.
func WithREST(doer any) Option {
	return func(c *Client) {
		if doer == nil {
			return
		}

		if fc, ok := doer.(*fast.Client); ok {
			c.fastClient = fc
			c.rest = request.AsRequester(fc)
			c.cfg.Socket.FastClient = fc
			c.cfg.Socket.Connector.FastClient = fc

			return
		}

		if r, ok := doer.(request.Requester); ok {
			c.rest = r
			return
		}

		if rd, ok := doer.(aoni.RequestDoer); ok {
			c.rest = request.AsRequester(rd)
			return
		}

		if hd, ok := doer.(aoni.HTTPDoer); ok {
			c.rest = request.AsRequester(aoni.NewHTTPDoerAdapter(hd))
			return
		}
	}
}

// WithFastClient configures fast.Client for high-performance zero-copy socket and REST transport.
func WithFastClient(fastClient *fast.Client) Option {
	return func(c *Client) {
		if fastClient == nil {
			return
		}

		c.fastClient = fastClient
		c.rest = request.AsRequester(fastClient)
		c.cfg.Socket.FastClient = fastClient
		c.cfg.Socket.Connector.FastClient = fastClient
	}
}

// WithBus assigns an event bus instance.
func WithBus(bus *bus.Bus) Option {
	return func(c *Client) { c.bus = bus }
}

// WithStorage assigns a storage provider instance.
func WithStorage(storage storage.Provider) Option {
	return func(c *Client) { c.storage = storage }
}

// WithAuthenticator assigns an authenticator provider.
func WithAuthenticator(authenticator session.AuthenticatorProvider) Option {
	return func(c *Client) { c.authenticator = authenticator }
}

// WithWebFactory assigns a custom WebSession provider factory.
func WithWebFactory(webFactory session.WebSessionFactory) Option {
	return func(c *Client) { c.webFactory = webFactory }
}

// WithCommunityFactory assigns a custom Community requester factory.
func WithCommunityFactory(communityFactory session.CommunityClientFactory) Option {
	return func(c *Client) { c.communityFactory = communityFactory }
}

// Client acts as the primary entry point connecting socket channels, authentication services, module managers, and request routers.
//
// Thread Safety:
//   - Safe for concurrent use across goroutines.
type Client struct {
	cfg      Config
	loggerMu sync.RWMutex
	logger   log.Logger
	bus      *bus.Bus

	socket     session.SocketProvider
	session    *session.Session
	router     *router.ServiceRouter
	modules    *modules.Manager
	rest       request.Requester
	fastClient *fast.Client
	storage    storage.Provider

	ctx       context.Context
	cancel    context.CancelFunc
	closed    chan struct{}
	wg        sync.WaitGroup
	fsm       *kata.FSM[State, Event]
	closeOnce sync.Once

	enrichedAccount string
	enrichedSteamID id.ID

	personaState   enums.EPersonaState
	personaStateMu sync.RWMutex

	authenticator    session.AuthenticatorProvider
	webFactory       session.WebSessionFactory
	communityFactory session.CommunityClientFactory
	pendingModules   []module.Module
	closeErr         error
}

// New initializes a Client instance with defaults and options.
func New(cfg Config, opts ...Option) (*Client, error) {
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec

	cfg.ResolveDefaults()

	fsm := kata.NewFSM[State, Event](StateNew)
	fsm.AddRules(
		kata.TransitionRule[State, Event]{From: StateNew, Event: EventRun, To: StateRunning},
		kata.TransitionRule[State, Event]{From: StateRunning, Event: EventAuthorize, To: StateAuthorized},
		kata.TransitionRule[State, Event]{From: StateAuthorized, Event: EventAuthorize, To: StateAuthorized},
		kata.TransitionRule[State, Event]{From: StateAuthorized, Event: EventClose, To: StateClosed},
		kata.TransitionRule[State, Event]{From: StateRunning, Event: EventClose, To: StateClosed},
		kata.TransitionRule[State, Event]{From: StateNew, Event: EventClose, To: StateClosed},
	)

	c := &Client{
		ctx:            ctx,
		cancel:         cancel,
		fsm:            fsm,
		cfg:            cfg,
		logger:         log.Discard,
		closed:         make(chan struct{}),
		personaState:   cfg.PersonaState,
		pendingModules: make([]module.Module, 0),
	}
	generic.ApplyOptions(c, opts...)

	if c.bus == nil {
		c.bus = bus.New()
	}

	if c.rest == nil {
		c.rest = aoni.NewClient(nil)
	}

	if c.storage == nil {
		c.storage = memory.New()
	}

	if c.socket == nil {
		if cfg.DisableSocket {
			c.socket = noopSocketProvider{}
		} else {
			c.socket = socket.New(cfg.Socket)
			c.socket.UpdateLogger(c.logger)
		}
	}

	if c.session == nil {
		var sessionHTTPDoer any
		switch {
		case c.fastClient != nil:
			sessionHTTPDoer = c.fastClient
		case c.rest != nil:
			sessionHTTPDoer = c.rest
		default:
			sessionHTTPDoer = aoni.NewClient(nil)
		}

		sessionCfg := session.Config{
			Device:           cfg.Device,
			Storage:          c.storage,
			HTTP:             sessionHTTPDoer,
			Bus:              c.bus,
			Logger:           c.logger,
			Authenticator:    c.authenticator,
			WebFactory:       c.webFactory,
			CommunityFactory: c.communityFactory,
		}
		c.session = session.New(c.socket, sessionCfg)
	}

	c.modules = modules.New(c, &initContext{Client: c}, c.session)

	var errs []error
	for _, m := range c.pendingModules {
		if err := c.modules.Add(m); err != nil {
			errs = append(errs, err)
		}
	}

	c.pendingModules = nil

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	if c.router == nil {
		c.router = router.New(c.session, c.socket)
	}

	return c, nil
}

// Storage returns the configured storage provider.
func (c *Client) Storage() storage.Provider { return c.storage }

// State returns the current client lifecycle state.
func (c *Client) State() State { return c.fsm.CurrentState() }

// IsNew reports whether the client has not yet been started via Run.
func (c *Client) IsNew() bool { return c.State() == StateNew }

// IsRunning reports whether background worker routines are active.
func (c *Client) IsRunning() bool { return !c.IsNew() && !c.IsClosed() }

// IsAuthorized reports whether user authorization is complete.
func (c *Client) IsAuthorized() bool { return c.State() == StateAuthorized }

// IsClosed reports whether the client has been permanently shut down.
func (c *Client) IsClosed() bool { return c.State() == StateClosed }

// Session returns the session manager instance.
func (c *Client) Session() *session.Session { return c.session }

// Router returns the request router instance.
func (c *Client) Router() *router.ServiceRouter { return c.router }

// Module retrieves a registered module by name.
func (c *Client) Module(name string) module.Module { return c.modules.Get(name) }

// Modules returns a snapshot of all registered modules.
func (c *Client) Modules() []module.Module { return c.modules.All() }

// RegisterModule dynamically registers and initializes a module.
func (c *Client) RegisterModule(m module.Module) {
	if m == nil {
		return
	}

	if err := c.modules.Register(c.ctx, m); err != nil {
		c.Logger().Error("Failed to register module", log.String("name", m.Name()), log.Err(err))
	}
}

// Socket returns the low-level socket provider.
func (c *Client) Socket() session.SocketProvider { return c.socket }

// Bus returns the event bus.
func (c *Client) Bus() *bus.Bus { return c.bus }

// Logger returns the configured logger.
func (c *Client) Logger() log.Logger {
	c.loggerMu.RLock()
	defer c.loggerMu.RUnlock()

	return c.logger
}

// Rest returns the low-level REST requester.
func (c *Client) Rest() request.Requester { return c.rest }

// Run initializes modules, starts session refresh routines, and transitions to StateRunning.
func (c *Client) Run() error {
	if c.IsRunning() {
		return ErrAlreadyRunning
	}

	if err := c.modules.InitAll(c.ctx); err != nil {
		return fmt.Errorf("steam: init modules: %w", err)
	}

	if err := c.modules.StartAll(c.ctx); err != nil {
		return fmt.Errorf("steam: start modules: %w", err)
	}

	c.wg.Go(func() {
		c.session.StartRefreshLoop(c.ctx)
	})

	c.wg.Go(func() {
		sub := c.bus.Subscribe(&auth.LoggedOnEvent{})
		defer sub.Unsubscribe()

		for {
			select {
			case <-c.ctx.Done():
				return

			case ev, ok := <-sub.C():
				if !ok {
					return
				}

				if _, ok := ev.(*auth.LoggedOnEvent); ok {
					if err := c.SetPersonaState(c.ctx, c.PersonaState()); err != nil {
						c.Logger().Warn("Failed to set persona state after logon event", log.Err(err))
					}
				}
			}
		}
	})

	return c.fsm.Transition(c.ctx, EventRun)
}

// Do executes a network request using optimal transport routing and automated session refresh retries.
func (c *Client) Do(ctx context.Context, req *tr.Request) (*tr.Response, error) {
	if !c.IsRunning() {
		return nil, ErrNotRunning
	}

	return c.router.Do(ctx, req)
}

// SetPersonaState updates persona availability status on Steam.
func (c *Client) SetPersonaState(ctx context.Context, state enums.EPersonaState) error {
	c.setPersonaState(state)

	statusReq := &pb.CMsgClientChangeStatus{
		PersonaState: proto.Uint32(uint32(state)),
	}

	return c.socket.SendProto(ctx, enums.EMsg_ClientChangeStatus, statusReq)
}

// ConnectAndLogin connects to a Connection Manager server and executes authentication.
func (c *Client) ConnectAndLogin(ctx context.Context, server socket.CMServer, details *auth.LogOnDetails) error {
	if c.IsClosed() {
		return module.ErrClosed
	}

	if c.cfg.DisableSocket {
		return ErrSocketDisabled
	}

	if details == nil {
		return ErrNilLogOnDetails
	}

	if !c.IsRunning() {
		return ErrNotRunning
	}

	c.EnrichLogger(details.AccountName, details.SteamID)

	if err := c.session.LogOn(ctx, server, details); err != nil {
		return err
	}

	c.EnrichLogger(details.AccountName, details.SteamID)

	if err := c.fsm.Transition(context.Background(), EventAuthorize); err != nil {
		return err
	}

	if err := c.modules.StartAuthedAll(c.ctx); err != nil {
		c.Logger().Error("Some modules failed to start authorized", log.Err(err))
		return err
	}

	return nil
}

// Reconnect re-discovers optimal Connection Managers and re-authenticates using cached credentials.
func (c *Client) Reconnect(ctx context.Context) error {
	if c.IsClosed() {
		return module.ErrClosed
	}

	c.Logger().Info("Attempting automatic reconnection...")

	if err := c.session.Disconnect(); err != nil {
		c.Logger().Warn("Disconnect failed during reconnect", log.Err(err))
	}

	server, err := directory.New(c).GetOptimalCMServer(ctx)
	if err == nil {
		c.session.SetLogonServer(server)
	} else {
		c.Logger().Warn("CM server discovery failed, using stored server", log.Err(err))
	}

	if err := c.session.Reconnect(ctx); err != nil {
		return fmt.Errorf("steam: reconnect failed: %w", err)
	}

	if err := c.fsm.Transition(context.Background(), EventAuthorize); err != nil {
		return err
	}

	c.Logger().Info("Reconnection successful")

	return nil
}

// Disconnect gracefully disconnects the socket transport.
func (c *Client) Disconnect() error {
	return c.session.Disconnect()
}

// Close gracefully stops modules, cancels contexts, and releases socket resources.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		var errs []error

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_ = c.modules.StopAll(shutdownCtx)

		if err := c.fsm.Transition(shutdownCtx, EventClose); err != nil {
			errs = append(errs, err)
		}

		if err := c.modules.StopAll(shutdownCtx); err != nil {
			errs = append(errs, err)
		}

		c.cancel()
		c.wg.Wait()

		if err := c.session.Close(); err != nil {
			errs = append(errs, err)
		}

		close(c.closed)
		c.closeErr = errors.Join(errs...)
	})

	return c.closeErr
}

// Wait blocks until client shutdown is complete.
func (c *Client) Wait() {
	<-c.closed
}

// EnrichLogger appends account name and SteamID fields to the logger context.
func (c *Client) EnrichLogger(account string, steamID id.ID) {
	c.loggerMu.Lock()
	defer c.loggerMu.Unlock()

	var logFields []any
	if account != "" && c.enrichedAccount == "" {
		logFields = append(logFields, log.String("account", account))
		c.enrichedAccount = account
	}

	if steamID != 0 && c.enrichedSteamID == 0 {
		logFields = append(logFields, log.Uint64("steam_id", steamID.Uint64()))
		c.enrichedSteamID = steamID
	}

	if len(logFields) == 0 {
		return
	}

	c.logger = c.logger.With(logFields...)
	c.session.EnrichLogger(account, steamID)

	if c.socket != nil {
		c.socket.UpdateLogger(c.logger)
	}
}

// PersonaState returns the current online persona state.
func (c *Client) PersonaState() enums.EPersonaState {
	c.personaStateMu.RLock()
	defer c.personaStateMu.RUnlock()

	return c.personaState
}

func (c *Client) setPersonaState(state enums.EPersonaState) {
	c.personaStateMu.Lock()
	c.personaState = state
	c.personaStateMu.Unlock()
}

type noopSocketProvider struct{}

func (noopSocketProvider) IsConnected() bool       { return false }
func (noopSocketProvider) Session() socket.Session { return nil }
func (noopSocketProvider) Connect(ctx context.Context, server socket.CMServer) error {
	return ErrSocketDisabled
}

func (noopSocketProvider) LogOn(ctx context.Context, payload []byte) error {
	return ErrSocketDisabled
}

func (noopSocketProvider) SetEncryptionKey(key []byte) bool { return false }
func (noopSocketProvider) Send(ctx context.Context, build socket.PayloadBuilder, opts ...socket.SendOption) error {
	return ErrSocketDisabled
}

func (noopSocketProvider) SendSync(
	ctx context.Context,
	build socket.PayloadBuilder,
	opts ...socket.SendOption,
) (*protocol.Packet, error) {
	return nil, ErrSocketDisabled
}

func (noopSocketProvider) SendProto(
	ctx context.Context,
	eMsg enums.EMsg,
	req proto.Message,
	opts ...socket.SendOption,
) error {
	return ErrSocketDisabled
}

func (noopSocketProvider) SendRaw(
	ctx context.Context,
	eMsg enums.EMsg,
	payload []byte,
	opts ...socket.SendOption,
) error {
	return ErrSocketDisabled
}

func (noopSocketProvider) RegisterMsgHandler(eMsg enums.EMsg, handler socket.Handler)   {}
func (noopSocketProvider) RegisterServiceHandler(method string, handler socket.Handler) {}
func (noopSocketProvider) StartHeartbeat(time.Duration) error {
	return ErrSocketDisabled
}

func (noopSocketProvider) Disconnect() error                    { return nil }
func (noopSocketProvider) Close() error                         { return nil }
func (noopSocketProvider) UpdateLogger(log.Logger)              {}
func (noopSocketProvider) UpdateServers([]socket.CMServer)      {}
func (noopSocketProvider) SetOnReconnect(func(context.Context)) {}

type initContext struct {
	Client *Client
}

func (ctx *initContext) Storage() storage.Provider        { return ctx.Client.storage }
func (ctx *initContext) Bus() *bus.Bus                    { return ctx.Client.bus }
func (ctx *initContext) Logger() log.Logger               { return ctx.Client.Logger() }
func (ctx *initContext) Service() service.Doer            { return ctx.Client }
func (ctx *initContext) Rest() request.Requester          { return ctx.Client.rest }
func (ctx *initContext) Module(name string) module.Module { return ctx.Client.Module(name) }

func (ctx *initContext) RegisterPacketHandler(e enums.EMsg, h socket.Handler) {
	ctx.Client.socket.RegisterMsgHandler(e, h)
}

func (ctx *initContext) UnregisterPacketHandler(e enums.EMsg) {
	ctx.Client.socket.RegisterMsgHandler(e, nil)
}

func (ctx *initContext) RegisterServiceHandler(method string, h socket.Handler) {
	ctx.Client.socket.RegisterServiceHandler(method, h)
}

func (ctx *initContext) UnregisterServiceHandler(method string) {
	ctx.Client.socket.RegisterServiceHandler(method, nil)
}
