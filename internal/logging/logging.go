// Package logging provides a tiny facade over log/slog so the audiorec
// library, backends, and CLI all log with a consistent set of attributes.
//
// Callers pass a *slog.Logger into constructors; a nil logger is replaced
// with a no-op so tests and library users who don't care about logs don't
// need to construct anything.
package logging

import (
	"io"
	"log/slog"
)

// NopLogger returns a logger that drops all records. Used as the default
// when a caller passes nil.
func NopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// OrNop returns l if non-nil, otherwise a no-op logger.
func OrNop(l *slog.Logger) *slog.Logger {
	if l == nil {
		return NopLogger()
	}
	return l
}

// WithBackend returns a derived logger tagged with backend=name.
func WithBackend(l *slog.Logger, name string) *slog.Logger {
	return OrNop(l).With("backend", name)
}

// WithTrack returns a derived logger tagged with track=label.
func WithTrack(l *slog.Logger, label string) *slog.Logger {
	return OrNop(l).With("track", label)
}
