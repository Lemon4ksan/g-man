package steam

import (
	"context"
	"errors"

	"github.com/lemon4ksan/g-man/pkg/log"
	"github.com/lemon4ksan/g-man/pkg/steam/api"
	"github.com/lemon4ksan/g-man/pkg/steam/bus"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
	"github.com/lemon4ksan/g-man/pkg/steam/socket"
)

var (
	ErrClientClosed = errors.New("steam: client is closed")
)

// InitContext provides the module with access to the necessary client resources
// during the initialization phase, without exposing lifecycle management methods (Close, Connect).
type InitContext interface {
	// Bus provides access to the event bus for subscribing/publishing internal messages.
	Bus() *bus.Bus

	// Logger returns the configured logger.
	Logger() log.Logger

	// Proto returns the UnifiedClient for making requests over the CM Socket.
	Proto() *api.UnifiedClient

	// RegisterPacketHandler registers a handler for low-level EMsg (TCP/UDP).
	RegisterPacketHandler(eMsg protocol.EMsg, handler socket.Handler)

	// RegisterServiceHandler registers a handler for Protobuf services (Unified Services).
	RegisterServiceHandler(method string, handler socket.Handler)

	// GetModule allows you to find another module if there are dependencies between them.
	GetModule(name string) Module

	// UnregisterPacketHandler removes the handler from socket for freeing memory.
	UnregisterPacketHandler(eMsg protocol.EMsg)

	// UnregisterServiceHandler removes the service handler from socket for freeing memory.
	UnregisterServiceHandler(method string)
}

type AuthContext interface {
	// Community returns an authorized community client.
	Community() *api.CommunityClient

	// SteamID returns the steam id of the authorized user.
	SteamID() uint64
}

// Module defines the contract for pluggable extensions (e.g., Trade, Chat, GC).
type Module interface {
	Name() string

	// Init is called during client creation. Use this to register packet handlers
	// and subscribe to bus events.
	Init(init InitContext) error

	// Start is called when the client starts running. Use this to launch
	// background tasks (tickers, pollers). The context is cancelled when the client closes.
	Start(ctx context.Context) error
}

// ModuleAuth defines the contract for pluggable extensions that require authorized clients.
type ModuleAuth interface {
	Module

	// StartAuthed is called after a successful Steam login and WebSession creation.
	StartAuthed(ctx context.Context, auth AuthContext) error
}
