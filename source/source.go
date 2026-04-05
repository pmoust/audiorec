package source

import "context"

// Source is the single contract every audio capture backend implements.
//
// Lifecycle:
//   1. Caller constructs a Source (backend-specific constructor).
//   2. Caller calls Start(ctx). On success, Format() is stable and Frames()
//      begins emitting frames.
//   3. Caller ranges over Frames() until it closes.
//   4. After Frames() closes, caller inspects Err() for the termination reason.
//   5. Caller calls Close() to release device resources (idempotent).
//
// Cancellation: cancelling the ctx passed to Start is the normal stop path.
// The backend drains its buffers, closes Frames(), and Err() returns nil.
type Source interface {
	// Format returns the PCM format. Stable only after Start() returns nil.
	// Calling before Start is undefined.
	Format() Format

	// Start begins capture. Negotiates device format with the OS; after this
	// returns nil, Format() is frozen and Frames() will begin emitting.
	// Returns a typed error (ErrPermissionDenied, ErrDeviceNotFound, ...) or
	// a wrapped error.
	Start(ctx context.Context) error

	// Frames returns the channel of captured audio frames. Closed when capture
	// ends (for any reason). Backends must close this channel exactly once.
	Frames() <-chan Frame

	// Err returns the reason capture ended. nil means clean shutdown via
	// ctx cancellation. Valid to call only after Frames() has closed.
	Err() error

	// Close releases device resources. Idempotent. Safe to call whether or
	// not Start was called. Does not need to be called if Start returned
	// an error, but is harmless.
	Close() error
}
