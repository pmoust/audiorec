// Note on backpressure: Session does NOT drop frames. If a backend delivers
// frames faster than the writer can consume, the backend itself is responsible
// for dropping on its internal ring buffer and reporting via EventFrameDropped.
// The session simply writes every frame it receives on Frames().
//
// Package session orchestrates one or more audio Sources into crash-safe
// WAV files. A Session takes a set of Tracks, starts every Source, and
// spawns one writer goroutine per track plus a shared flush ticker.
//
// Partial failure semantics: when one Source ends with an error, its track
// is finalized cleanly and other tracks keep recording. Run returns a joined
// error for all per-track failures at the end.
package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/pmoust/audiorec/internal/logging"
	"github.com/pmoust/audiorec/source"
	"github.com/pmoust/audiorec/wav"
)

// Track couples one Source with the WAV file it writes to.
type Track struct {
	Source source.Source
	Path   string
	Label  string // used in logs; must be unique within a Session
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

// Run starts every Source in cfg.Tracks, creates a wav.Writer per track,
// and blocks until every source's Frames() channel has closed. Cancel ctx
// to stop the session cleanly.
//
// Startup is all-or-nothing: if any Source fails to Start, already-started
// sources are Closed and any created wav files are removed before Run
// returns the error.
//
// Partial failure: if one source ends with an error, its wav is finalized
// and the supervisor keeps waiting on the others. Run returns a joined
// error via errors.Join.
func (s *Session) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Phase 1: Start every source. Track which ones started for rollback.
	started := make([]bool, len(s.cfg.Tracks))
	for i, tr := range s.cfg.Tracks {
		if err := tr.Source.Start(ctx); err != nil {
			// Rollback: close already-started sources.
			for j := range i {
				if started[j] {
					_ = s.cfg.Tracks[j].Source.Close()
				}
			}
			return fmt.Errorf("session: start %q: %w", tr.Label, err)
		}
		started[i] = true
	}

	// Phase 2: Create wav writers. Sources' Format() is now stable.
	writers := make([]*wav.Writer, len(s.cfg.Tracks))
	for i, tr := range s.cfg.Tracks {
		w, err := wav.Create(tr.Path, tr.Source.Format())
		if err != nil {
			// Rollback: close writers created so far, all sources, remove files.
			for j := range i {
				_ = writers[j].Close()
				_ = os.Remove(s.cfg.Tracks[j].Path)
			}
			for _, src := range s.cfg.Tracks {
				_ = src.Source.Close()
			}
			return fmt.Errorf("session: create wav %q: %w", tr.Label, err)
		}
		writers[i] = w
	}

	s.emit(Event{Kind: EventStarted})

	// Phase 3: Writer goroutines + flush ticker + supervisor.
	var wg sync.WaitGroup
	errCh := make(chan error, len(s.cfg.Tracks))

	for i := range s.cfg.Tracks {
		wg.Add(1)
		go s.runWriter(i, writers[i], &wg, errCh)
	}

	stopTicker := make(chan struct{})
	go s.runFlushTicker(writers, stopTicker)

	// Wait for all writers to exit.
	wg.Wait()
	close(stopTicker)
	close(errCh)

	// Close every source (harmless if already ended).
	for _, tr := range s.cfg.Tracks {
		_ = tr.Source.Close()
	}

	// Join all per-track errors.
	var errs []error
	for e := range errCh {
		if e != nil {
			errs = append(errs, e)
		}
	}
	return errors.Join(errs...)
}

// runWriter drains frames from one source into one wav writer. Exits when
// the source's channel closes. Collects the source's Err() and any write
// error into errCh.
func (s *Session) runWriter(i int, w *wav.Writer, wg *sync.WaitGroup, errCh chan<- error) {
	defer wg.Done()
	tr := s.cfg.Tracks[i]
	tlog := logging.WithTrack(s.log, tr.Label)

	var writeErr error
	for f := range tr.Source.Frames() {
		if err := w.WriteFrame(f); err != nil {
			writeErr = err
			tlog.Error("write frame failed", "err", err)
			s.emit(Event{Kind: EventError, Track: tr.Label, Err: err})
			// Drain remaining frames so the source goroutine can exit.
			for range tr.Source.Frames() {
			}
			break
		}
	}
	// Final flush + close happens here on the same goroutine → no races.
	if closeErr := w.Close(); closeErr != nil && writeErr == nil {
		writeErr = closeErr
	}

	srcErr := tr.Source.Err()
	s.emit(Event{Kind: EventTrackEnded, Track: tr.Label, Err: firstNonNil(srcErr, writeErr)})

	switch {
	case srcErr != nil && writeErr != nil:
		errCh <- fmt.Errorf("track %q: %w (write: %w)", tr.Label, srcErr, writeErr)
	case srcErr != nil:
		errCh <- fmt.Errorf("track %q: %w", tr.Label, srcErr)
	case writeErr != nil:
		errCh <- fmt.Errorf("track %q: %w", tr.Label, writeErr)
	default:
		errCh <- nil
	}
}

func firstNonNil(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

// runFlushTicker periodically calls Flush on every writer. Exits when stop
// is closed.
func (s *Session) runFlushTicker(writers []*wav.Writer, stop <-chan struct{}) {
	t := time.NewTicker(s.cfg.FlushInterval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			for i, w := range writers {
				if err := w.Flush(); err != nil {
					s.log.Debug("flush failed", "track", s.cfg.Tracks[i].Label, "err", err)
					s.emit(Event{Kind: EventError, Track: s.cfg.Tracks[i].Label, Err: err})
				}
			}
			s.emit(Event{Kind: EventFlushed})
		}
	}
}

func (s *Session) emit(ev Event) {
	if s.cfg.OnEvent != nil {
		s.cfg.OnEvent(ev)
	}
}
