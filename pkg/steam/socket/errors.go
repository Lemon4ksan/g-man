package socket

import (
	"errors"
	"fmt"
	"github.com/lemon4ksan/g-man/pkg/steam/protocol"
)

var (
	// ErrClosed is returned when an operation is attempted on a Socket that 
	// has been permanently shut down via Close().
	ErrClosed = errors.New("socket: instance is permanently closed")

	// ErrDisconnected is returned when sending a message requires an active 
	// session, but the socket is currently disconnected.
	ErrDisconnected = errors.New("socket: not connected to any CM server")

	// ErrAlreadyConnecting is returned if Connect() is called while a 
	// connection attempt is already in progress.
	ErrAlreadyConnecting = errors.New("socket: connection attempt already in progress")

	// ErrAlreadyConnected is returned if Connect() is called on a socket 
	// that already has an active session.
	ErrAlreadyConnected = errors.New("socket: already connected")

	// ErrUnsupportedType is returned when the provided CMServer.Type does 
	// not have a registered dialer in the configuration.
	ErrUnsupportedType = errors.New("socket: unsupported transport protocol")

	// ErrDecompressionLimit is returned when a Multi-message payload 
	// exceeds the safety threshold (default 100MB) to prevent OOM attacks.
	ErrDecompressionLimit = errors.New("socket: decompression limit exceeded")
)

// SteamError represents a protocol-level error returned by the Steam CM.
// It maps the EResult code to a human-readable format.
type SteamError struct {
	EMsg    protocol.EMsg
	EResult protocol.EResult
	Message string
}

func (e *SteamError) Error() string {
	return fmt.Sprintf("steam error: %s (result: %d) %s", e.EMsg, e.EResult, e.Message)
}

// JobError wraps a failure that occurred during an asynchronous RPC call.
// It includes the JobID to help correlate the failure with the original request.
type JobError struct {
	JobID uint64
	Cause error
}

func (e *JobError) Error() string {
	return fmt.Sprintf("job %d failed: %v", e.JobID, e.Cause)
}

func (e *JobError) Unwrap() error { return e.Cause }