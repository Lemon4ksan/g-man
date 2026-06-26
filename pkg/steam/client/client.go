// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package client provides the central coordination hub for the Steam client subsystem.
// It manages connection lifecycle, authentication transitions, API request routing,
// event dispatching, and dynamic module execution.
//
// The core component is [Client], which integrates network socket transport,
// authentication sessions via [session.Session], and custom client modules.
//
// Basic usage:
//
//	cfg := client.DefaultConfig()
//	c, err := client.New(cfg)
//	if err != nil {
//		log.Fatal(err)
//	}
//	if err := c.Run(); err != nil {
//		log.Fatal(err)
//	}
//	defer c.Close()
package client

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/miyako/bus"
	"github.com/lemon4ksan/miyako/generic"
	"github.com/lemon4ksan/miyako/kata"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/g-man/pkg/log"
	pb "github.com/lemon4ksan/g-man/pkg/protobuf/steam"
	"github.com/lemon4ksan/g-man/pkg/steam/auth"
	"github.com/lemon4ksan/g-man/pkg/steam/client/modules"
	"github.com/lemon4ksan/g-man/pkg/steam/client/router"
	"github.com/lemon4ksan/g-man/pkg/steam/client/session"
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
	// ErrNotRunning is returned when executing operations on a [Client] that is not running.
	ErrNotRunning = errors.New("steam: client must be running (call Run() first)")

	// ErrSocketDisabled is returned when attempting socket operations while socket transport is disabled.
	ErrSocketDisabled = errors.New("steam: socket transport is disabled")

	// ErrNilLogOnDetails is returned when [Client.ConnectAndLogin] receives nil credentials.
	ErrNilLogOnDetails = errors.New("steam: logon details cannot be nil")
)

// Config aggregates configuration parameters for all core subsystems of [Client].
// Use [DefaultConfig] to initialize a configuration with standard settings.
type Config struct {
	// PersonaState defines the initial [enums.EPersonaState] on logon.
	PersonaState enums.EPersonaState
	// Socket holds the configuration for the [socket.Config] connection.
	Socket socket.Config
	// Device specifies the [auth.DeviceConfig] used during credential verification.
	Device *auth.DeviceConfig
	// ProxyURL defines a global proxy URL affecting all transport traffic.
	ProxyURL string
	// DisableSocket disables the socket transport layer, forcing WebAPI-only mode.
	DisableSocket bool
}

// DefaultConfig returns the baseline [Config] with standard defaults.
func DefaultConfig() Config {
	return Config{
		PersonaState: enums.EPersonaState_Online,
		Socket:       socket.DefaultConfig(),
	}
}

// State represents the lifecycle state of the [Client].
type State int32

const (
	// StateNew indicates the [Client] is initialized but not yet running.
	StateNew State = iota
	// StateRunning indicates the [Client] background loops are active.
	StateRunning
	// StateAuthorized indicates the [Client] is fully authorized and ready.
	StateAuthorized
	// StateClosed indicates the [Client] has been permanently shut down.
	StateClosed
)

// Event represents a transition trigger in the [Client] lifecycle.
type Event int32

const (
	// EventRun triggers a [Client] state transition from New to Running.
	EventRun Event = iota
	// EventAuthorize triggers a [Client] state transition from Running to Authorized.
	EventAuthorize
	// EventClose triggers a [Client] state transition to Closed from any active state.
	EventClose
)

// String returns a human-readable representation of [State].
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

// Option defines a functional configuration option for [Client].
type Option = generic.Option[*Client]

// WithLogger sets a custom [log.Logger] for [Client].
func WithLogger(l log.Logger) Option {
	return func(c *Client) { c.logger = l.With(log.Module("steam")) }
}

// WithSession sets a custom [session.Session] for [Client].
func WithSession(s *session.Session) Option {
	return func(c *Client) { c.session = s }
}

// WithRouter sets a custom [router.ServiceRouter] for [Client].
func WithRouter(r *router.ServiceRouter) Option {
	return func(c *Client) { c.router = r }
}

// WithModule adds a [module.Module] to [Client] and initializes it immediately.
func WithModule(m module.Module) Option {
	return func(c *Client) {
		if c.modules != nil {
			_ = c.modules.Add(m)
		} else {
			c.pendingModules = append(c.pendingModules, m)
		}
	}
}

// WithSocket sets a custom [session.SocketProvider] for [Client].
func WithSocket(sock session.SocketProvider) Option {
	return func(c *Client) { c.socket = sock }
}

// WithREST sets a custom [aoni.Client] for [Client].
func WithREST(rest *aoni.Client) Option {
	return func(c *Client) { c.rest = rest }
}

// WithBus sets a custom [bus.Bus] for [Client].
func WithBus(bus *bus.Bus) Option {
	return func(c *Client) { c.bus = bus }
}

// WithStorage sets a custom [storage.Provider] for [Client].
func WithStorage(storage storage.Provider) Option {
	return func(c *Client) { c.storage = storage; c.storageSet = true }
}

// WithAuthenticator sets a custom [session.AuthenticatorProvider] for [Client].
func WithAuthenticator(authenticator session.AuthenticatorProvider) Option {
	return func(c *Client) { c.authenticator = authenticator }
}

// WithWebFactory sets a custom [session.WebSessionFactory] for [Client].
func WithWebFactory(webFactory session.WebSessionFactory) Option {
	return func(c *Client) { c.webFactory = webFactory }
}

// WithCommunityFactory sets a custom [session.CommunityClientFactory] for [Client].
func WithCommunityFactory(communityFactory session.CommunityClientFactory) Option {
	return func(c *Client) { c.communityFactory = communityFactory }
}

// Client acts as the central hub connecting socket, authentication, and modules.
// It orchestrates low-level communication via [session.SocketProvider] and HTTP transport,
// manages authentication state using [session.Session], and routes requests using [router.ServiceRouter].
// Create new instances of Client using [New] or [NewReady].
type Client struct {
	cfg      Config
	loggerMu sync.RWMutex
	logger   log.Logger
	bus      *bus.Bus

	socket  session.SocketProvider
	session *session.Session
	router  *router.ServiceRouter
	modules *modules.Manager
	rest    *aoni.Client
	storage storage.Provider

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
	storageSet       bool
}

// ResolveDefaults applies default fallbacks to the [Config] fields.
func (cfg *Config) ResolveDefaults() {
	cfg.Socket.Connector.ProxyURL = generic.Coalesce(cfg.Socket.Connector.ProxyURL, cfg.ProxyURL)
}

// New initializes and returns a new [Client] with the given [Config] and [Option] list.
// Returns an error if option application fails or configuration is invalid.
// Falls back to empty arrays if no functional options are provided.
func New(cfg Config, opts ...Option) (*Client, error) {
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec

	cfg.ResolveDefaults()

	fsm := kata.NewFSM[State, Event](StateNew)
	fsm.AddRules(
		kata.TransitionRule[State, Event]{From: StateNew, Event: EventRun, To: StateRunning},
		kata.TransitionRule[State, Event]{From: StateRunning, Event: EventAuthorize, To: StateAuthorized},
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

	if !c.storageSet {
		c.storage = memory.New()
	}

	if c.socket == nil {
		if cfg.DisableSocket {
			c.socket = noopSocketProvider{}
		} else {
			c.socket = socket.New(cfg.Socket, c.logger)
		}
	}

	if c.session == nil {
		sessionCfg := session.Config{
			Device:           cfg.Device,
			Storage:          c.storage,
			HTTP:             c.rest.HTTP(),
			Bus:              c.bus,
			Logger:           c.logger,
			Authenticator:    c.authenticator,
			WebFactory:       c.webFactory,
			CommunityFactory: c.communityFactory,
		}
		c.session = session.New(c.socket, sessionCfg)
	}

	c.modules = modules.New(c, &initContext{Client: c}, c.session)

	for _, m := range c.pendingModules {
		_ = c.modules.Add(m)
	}

	c.pendingModules = nil

	if c.router == nil {
		c.router = router.New(c.session, c.socket)
	}

	return c, nil
}

// Storage returns the [storage.Provider] instance configured for the [Client].
func (c *Client) Storage() storage.Provider { return c.storage }

// State returns the current lifecycle [State] of the [Client].
func (c *Client) State() State { return c.fsm.CurrentState() }

// IsAuthorized returns true if the [Client] has reached [StateAuthorized].
func (c *Client) IsAuthorized() bool { return c.State() == StateAuthorized }

// IsRunning returns true if the [Client] has reached at least [StateRunning].
func (c *Client) IsRunning() bool { return c.State() >= StateRunning }

// Session returns the active [session.Session] instance of the [Client].
func (c *Client) Session() *session.Session { return c.session }

// Router returns the [router.ServiceRouter] instance of the [Client].
func (c *Client) Router() *router.ServiceRouter { return c.router }

// Module returns the registered [module.Module] by its name.
func (c *Client) Module(name string) module.Module { return c.modules.Get(name) }

// Modules returns all registered [module.Module] instances of the [Client].
func (c *Client) Modules() []module.Module { return c.modules.All() }

// RegisterModule adds a [module.Module] to the [Client] and initializes it immediately.
// Logs an error if module registration fails.
// Does nothing if the module is nil.
func (c *Client) RegisterModule(m module.Module) {
	if m == nil {
		return
	}

	if err := c.modules.Register(c.ctx, m); err != nil {
		c.Logger().Error("Failed to register module",
			log.String("name", m.Name()),
			log.Err(err))
	}
}

// Socket returns the underlying [session.SocketProvider] of the [Client].
func (c *Client) Socket() session.SocketProvider { return c.socket }

// Bus returns the [bus.Bus] used by the [Client] for event handling.
func (c *Client) Bus() *bus.Bus { return c.bus }

// Logger returns the configured thread-safe [log.Logger] of the [Client].
func (c *Client) Logger() log.Logger {
	c.loggerMu.RLock()
	defer c.loggerMu.RUnlock()
	return c.logger
}

// Rest returns the low-level [aoni.Requester] of the [Client].
func (c *Client) Rest() aoni.Requester { return c.rest }

// Run starts all registered modules and runs the background CM session refresh loop.
// Returns an error if any module fails to initialize or start.
// Aborts execution if the context ctx is canceled.
func (c *Client) Run() error {
	if err := c.modules.InitAll(c.ctx); err != nil {
		return fmt.Errorf("steam: init modules: %w", err)
	}

	if err := c.modules.StartAll(c.ctx); err != nil {
		return fmt.Errorf("steam: start modules: %w", err)
	}

	c.wg.Go(func() {
		_ = c.session.StartRefreshLoop(c.ctx)
	})

	_ = c.fsm.Transition(c.ctx, EventRun)

	return nil
}

// Do executes a network request using the [Client].
// It selects between [session.SocketProvider] and HTTP transport, and handles silent token refresh.
// Returns [ErrNotRunning] if the client is not running.
// Aborts request processing if the context ctx is canceled.
func (c *Client) Do(ctx context.Context, req *tr.Request) (*tr.Response, error) {
	if c.State() != StateRunning && c.State() != StateAuthorized {
		return nil, ErrNotRunning
	}

	return c.router.Do(ctx, req)
}

// SetPersonaState updates the client [enums.EPersonaState] on the server.
// Returns an error if sending the status packet over [session.SocketProvider] fails or if context ctx is canceled.
func (c *Client) SetPersonaState(ctx context.Context, state enums.EPersonaState) error {
	c.setPersonaState(state)

	statusReq := &pb.CMsgClientChangeStatus{
		PersonaState: proto.Uint32(uint32(state)),
	}

	return c.socket.SendProto(ctx, enums.EMsg_ClientChangeStatus, statusReq)
}

// ConnectAndLogin connects to the CM server and performs the logon sequence.
// Returns [module.ErrClosed] if the client is closed, or [ErrSocketDisabled] if socket transport is disabled.
// Returns an error if connection, handshake, or login credentials fail.
// Returns an error if context ctx is canceled or details is nil.
func (c *Client) ConnectAndLogin(ctx context.Context, server socket.CMServer, details *auth.LogOnDetails) error {
	if c.State() == StateClosed {
		return module.ErrClosed
	}

	if c.cfg.DisableSocket {
		return ErrSocketDisabled
	}

	if details == nil {
		return ErrNilLogOnDetails
	}

	c.EnrichLogger(details.AccountName, details.SteamID)

	if err := c.session.LogOn(ctx, server, details); err != nil {
		return err
	}

	c.EnrichLogger(details.AccountName, details.SteamID)

	if err := c.SetPersonaState(ctx, c.GetPersonaState()); err != nil {
		c.Logger().Warn("Failed to send initial persona status update", log.Err(err))
	}

	_ = c.fsm.Transition(context.Background(), EventAuthorize)

	if err := c.modules.StartAuthedAll(c.ctx); err != nil {
		c.Logger().Error("Some modules failed to start authorized", log.Err(err))
		return err
	}

	return nil
}

// Reconnect re-authenticates with Steam using cached credentials after a session loss.
// Returns an error if reconnect or subsequent persona state update fails.
// Aborts reconnect sequence if context ctx is canceled.
func (c *Client) Reconnect(ctx context.Context) error {
	if c.State() == StateClosed {
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

	if err := c.SetPersonaState(ctx, c.GetPersonaState()); err != nil {
		c.Logger().Warn("Failed to set persona state after reconnect", log.Err(err))
	}

	_ = c.fsm.Transition(context.Background(), EventAuthorize)

	c.Logger().Info("Reconnection successful")

	return nil
}

// Disconnect terminates the CM connection but keeps the [Client] and its modules active.
// Returns an error if the socket disconnect operation fails.
func (c *Client) Disconnect() error {
	return c.session.Disconnect()
}

// Close shuts down the [Client], stops all modules, and releases allocated network resources.
// Can be called safely multiple times; subsequent calls return no new errors.
func (c *Client) Close() error {
	var errs []error

	c.closeOnce.Do(func() {
		if err := c.fsm.Transition(context.Background(), EventClose); err != nil {
			errs = append(errs, err)
		}

		if err := c.modules.StopAll(context.Background()); err != nil {
			errs = append(errs, err)
		}

		c.cancel()
		c.wg.Wait()

		if err := c.session.Close(); err != nil {
			errs = append(errs, err)
		}

		close(c.closed)
	})

	return errors.Join(errs...)
}

// Wait blocks the calling goroutine until the [Client] has finished its shutdown sequence.
func (c *Client) Wait() {
	<-c.closed
}

// EnrichLogger adds the account and/or steamID to the loggers of the client and all its subsystems.
func (c *Client) EnrichLogger(account string, steamID id.ID) {
	c.loggerMu.Lock()
	defer c.loggerMu.Unlock()

	var logFields []log.Field
	if account != "" && c.enrichedAccount == "" {
		logFields = append(logFields, log.String("account", account))
		c.enrichedAccount = account
	}

	if steamID != 0 && c.enrichedSteamID == 0 {
		logFields = append(logFields, log.SteamID(steamID.Uint64()))
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

// GetPersonaState returns the current persona state of the client.
func (c *Client) GetPersonaState() enums.EPersonaState {
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
func (noopSocketProvider) Disconnect() error               { return nil }
func (noopSocketProvider) Close() error                    { return nil }
func (noopSocketProvider) UpdateLogger(log.Logger)         {}
func (noopSocketProvider) UpdateServers([]socket.CMServer) {}

type initContext struct {
	Client *Client
}

func (ctx *initContext) Storage() storage.Provider        { return ctx.Client.storage }
func (ctx *initContext) Bus() *bus.Bus                    { return ctx.Client.bus }
func (ctx *initContext) Logger() log.Logger               { return ctx.Client.Logger() }
func (ctx *initContext) Service() service.Doer            { return ctx.Client }
func (ctx *initContext) Rest() aoni.Requester             { return ctx.Client.rest }
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
