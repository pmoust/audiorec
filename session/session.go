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
	"path/filepath"
	"sync"
	"time"

	"github.com/pmoust/audiorec/internal/logging"
	"github.com/pmoust/audiorec/source"
	"github.com/pmoust/audiorec/wav"
)

// Writer is the subset of wav.Writer that Session consumes. Sessions
// normally write to wav.Writer via the default WriterFactory, but tests
// can inject a custom factory to exercise error paths.
type Writer interface {
	WriteFrame(source.Frame) error
	Flush() error
	Close() error
}

// WriterFactory constructs a Writer for a track. Nil Config.WriterFactory
// defaults to a thin wrapper around wav.Create.
type WriterFactory func(path string, format source.Format) (Writer, error)

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
	WriterFactory WriterFactory // nil => default (wraps wav.Create)
	OnEvent       func(Event)   // optional
}

// Session is created by New and executed with Run.
type Session struct {
	cfg Config
	log *slog.Logger
}

// trackState holds per-track counters and timestamps accumulated during Run.
type trackState struct {
	mu            sync.Mutex
	startedAt     time.Time
	endedAt       time.Time
	framesWritten int64
	bytesWritten  int64
	drops         int64
	err           error
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
	if cfg.WriterFactory == nil {
		cfg.WriterFactory = func(path string, format source.Format) (Writer, error) {
			return wav.Create(path, format)
		}
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
	// Also prepare trackStates (will be fully allocated in Phase 3).
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
	writers := make([]Writer, len(s.cfg.Tracks))
	for i, tr := range s.cfg.Tracks {
		w, err := s.cfg.WriterFactory(tr.Path, tr.Source.Format())
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
	// Record session start time and create per-track state.
	sessionStartedAt := time.Now()
	trackStates := make([]*trackState, len(s.cfg.Tracks))
	for i := range s.cfg.Tracks {
		trackStates[i] = &trackState{}
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(s.cfg.Tracks))

	for i := range s.cfg.Tracks {
		wg.Add(1)
		go s.runWriter(i, writers[i], trackStates[i], &wg, errCh)
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
	runErr := errors.Join(errs...)

	// Build and write manifest.
	sessionEndedAt := time.Now()
	trackManifests := make([]TrackManifest, len(s.cfg.Tracks))
	for i, tr := range s.cfg.Tracks {
		ts := trackStates[i]
		ts.mu.Lock()
		startedAt := ts.startedAt
		endedAt := ts.endedAt
		framesWritten := ts.framesWritten
		bytesWritten := ts.bytesWritten
		drops := ts.drops
		trackErr := ts.err
		ts.mu.Unlock()

		errStr := (*string)(nil)
		if trackErr != nil {
			es := trackErr.Error()
			errStr = &es
		}

		trackManifests[i] = TrackManifest{
			Label:         tr.Label,
			Path:          filepath.Base(tr.Path), // basename only
			Format:        tr.Source.Format(),
			StartedAt:     startedAt,
			EndedAt:       endedAt,
			FramesWritten: framesWritten,
			BytesWritten:  bytesWritten,
			Drops:         drops,
			Error:         errStr,
		}
	}

	sessionID := filepath.Base(sessionDir(s.cfg.Tracks))
	if sessionID == "" {
		sessionID = sessionStartedAt.Format("20060102-150405")
	}

	manifest := &Manifest{
		Version:   ManifestVersion,
		SessionID: sessionID,
		StartedAt: sessionStartedAt,
		EndedAt:   sessionEndedAt,
		Tracks:    trackManifests,
	}
	manifest.DurationSeconds = manifest.EndedAt.Sub(manifest.StartedAt).Seconds()

	dir := sessionDir(s.cfg.Tracks)
	if dir != "" {
		path := filepath.Join(dir, "manifest.json")
		if err := WriteManifestJSON(path, manifest); err != nil {
			s.log.Warn("manifest write failed", "err", err)
		}
	}

	return runErr
}

// runWriter drains frames from one source into one wav writer. Exits when
// the source's channel closes. Collects the source's Err() and any write
// error into errCh. Accumulates per-track counters and timestamps into ts.
func (s *Session) runWriter(i int, w Writer, ts *trackState, wg *sync.WaitGroup, errCh chan<- error) {
	defer wg.Done()
	tr := s.cfg.Tracks[i]
	tlog := logging.WithTrack(s.log, tr.Label)

	// Capture track start time.
	ts.mu.Lock()
	ts.startedAt = time.Now()
	ts.mu.Unlock()

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
		// Accumulate frame and byte counters.
		ts.mu.Lock()
		ts.framesWritten += int64(f.NumFrames)
		ts.bytesWritten += int64(len(f.Data))
		ts.mu.Unlock()
	}
	// Final flush + close happens here on the same goroutine → no races.
	if closeErr := w.Close(); closeErr != nil && writeErr == nil {
		writeErr = closeErr
	}

	// Capture track end time and gather drops.
	ts.mu.Lock()
	ts.endedAt = time.Now()
	// Try to get drops from source if it exposes Drops() method.
	if d, ok := tr.Source.(interface{ Drops() int64 }); ok {
		ts.drops = d.Drops()
	}
	ts.mu.Unlock()

	srcErr := tr.Source.Err()
	s.emit(Event{Kind: EventTrackEnded, Track: tr.Label, Err: firstNonNil(srcErr, writeErr)})

	// Store combined error as string for manifest.
	ts.mu.Lock()
	switch {
	case srcErr != nil && writeErr != nil:
		errStr := fmt.Sprintf("track %q: %v (write: %v)", tr.Label, srcErr, writeErr)
		ts.err = errors.New(errStr)
	case srcErr != nil:
		ts.err = srcErr
	case writeErr != nil:
		ts.err = writeErr
	}
	ts.mu.Unlock()

	// Return error for joining in Run.
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
func (s *Session) runFlushTicker(writers []Writer, stop <-chan struct{}) {
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
