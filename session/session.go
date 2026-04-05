// Package session orchestrates one or more audio Sources into crash-safe
// WAV files. A Session takes a set of Tracks, starts every Source, and
// spawns one writer goroutine per track plus a shared flush ticker.
//
// Partial failure semantics: when one Source ends with an error, its track
// is finalized cleanly and other tracks keep recording. Run returns a joined
// error for all per-track failures at the end.
package session

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/pmoust/audiorec/internal/logging"
	"github.com/pmoust/audiorec/source"
)

// Track couples one Source with the WAV file it writes to.
type Track struct {
	Source source.Source
	Path   string
	Label  string // used in logs and Stats; must be unique within a Session
}

// EventKind categorizes OnEvent callbacks.
type EventKind int

const (
	EventStarted EventKind = iota
	EventFlushed
	EventFrameDropped
	EventTrackEnded
	EventError
)

// Event is delivered to Config.OnEvent when non-nil.
type Event struct {
	Kind  EventKind
	Track string
	Err   error
	Extra map[string]any
}

// Config is the input to New.
type Config struct {
	Tracks        []Track
	FlushInterval time.Duration // default 2s
	Logger        *slog.Logger  // nil => no-op
	OnEvent       func(Event)   // optional
}

// Session is created by New and executed with Run.
type Session struct {
	cfg Config
	log *slog.Logger
}

// New validates cfg and returns a Session ready to Run. It does NOT start
// any sources — Run does that.
func New(cfg Config) (*Session, error) {
	if len(cfg.Tracks) == 0 {
		return nil, errors.New("session: at least one Track is required")
	}
	labels := make(map[string]struct{}, len(cfg.Tracks))
	for i, t := range cfg.Tracks {
		if t.Source == nil {
			return nil, fmt.Errorf("session: track %d has nil Source", i)
		}
		if t.Path == "" {
			return nil, fmt.Errorf("session: track %d has empty Path", i)
		}
		if t.Label == "" {
			return nil, fmt.Errorf("session: track %d has empty Label", i)
		}
		if _, dup := labels[t.Label]; dup {
			return nil, fmt.Errorf("session: duplicate label %q", t.Label)
		}
		labels[t.Label] = struct{}{}
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 2 * time.Second
	}
	return &Session{cfg: cfg, log: logging.OrNop(cfg.Logger)}, nil
}
