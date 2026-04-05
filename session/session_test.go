package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pmoust/audiorec/source"
	"github.com/pmoust/audiorec/wav"
)

func TestNew_RequiresAtLeastOneTrack(t *testing.T) {
	_, err := New(Config{})
	if err == nil {
		t.Fatalf("expected error for empty Tracks")
	}
}

func TestNew_AppliesDefaultFlushInterval(t *testing.T) {
	fs := newFakeSource(source.Format{SampleRate: 48000, Channels: 1, BitsPerSample: 16}, nil)
	s, err := New(Config{
		Tracks: []Track{{Source: fs, Path: t.TempDir() + "/a.wav", Label: "a"}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s.cfg.FlushInterval != 2*time.Second {
		t.Errorf("default flush interval: got %v want 2s", s.cfg.FlushInterval)
	}
}

func TestNew_RejectsDuplicateLabels(t *testing.T) {
	fs1 := newFakeSource(source.Format{SampleRate: 48000, Channels: 1, BitsPerSample: 16}, nil)
	fs2 := newFakeSource(source.Format{SampleRate: 48000, Channels: 1, BitsPerSample: 16}, nil)
	_, err := New(Config{
		Tracks: []Track{
			{Source: fs1, Path: t.TempDir() + "/a.wav", Label: "mic"},
			{Source: fs2, Path: t.TempDir() + "/b.wav", Label: "mic"},
		},
	})
	if err == nil {
		t.Fatalf("expected error for duplicate labels")
	}
}

func TestRun_TwoTracksHappyPath(t *testing.T) {
	dir := t.TempDir()

	f := source.Format{SampleRate: 48000, Channels: 1, BitsPerSample: 16}
	micFs := newFakeSource(f, makeFrames(5, 20, 0xAA))
	sysFs := newFakeSource(f, makeFrames(3, 40, 0xBB))

	s, err := New(Config{
		FlushInterval: 50 * time.Millisecond,
		Tracks: []Track{
			{Source: micFs, Path: filepath.Join(dir, "mic.wav"), Label: "mic"},
			{Source: sysFs, Path: filepath.Join(dir, "sys.wav"), Label: "sys"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// mic.wav: 5 frames * 20 bytes = 100 bytes of PCM
	assertWavDataSize(t, filepath.Join(dir, "mic.wav"), 100, 0xAA)
	// sys.wav: 3 frames * 40 bytes = 120 bytes of PCM
	assertWavDataSize(t, filepath.Join(dir, "sys.wav"), 120, 0xBB)
}

func assertWavDataSize(t *testing.T, path string, expectedBytes int, fill byte) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if len(b) < 44+expectedBytes {
		t.Fatalf("%s: file too short: %d < %d", path, len(b), 44+expectedBytes)
	}
	// Byte 40..44 is the data chunk size little-endian.
	got := int(b[40]) | int(b[41])<<8 | int(b[42])<<16 | int(b[43])<<24
	if got != expectedBytes {
		t.Errorf("%s: data size: got %d want %d", path, got, expectedBytes)
	}
	for i := range expectedBytes {
		if b[44+i] != fill {
			t.Errorf("%s: byte %d: got %#x want %#x", path, i, b[44+i], fill)
			return
		}
	}
}

func TestRun_PartialFailure_OneTrackDiesOtherContinues(t *testing.T) {
	dir := t.TempDir()
	f := source.Format{SampleRate: 48000, Channels: 1, BitsPerSample: 16}

	dying := newFakeSource(f, makeFrames(2, 20, 0xAA))
	dying.endErr = source.ErrDeviceDisconnected

	surviving := newFakeSource(f, makeFrames(10, 20, 0xBB))

	s, err := New(Config{
		FlushInterval: 50 * time.Millisecond,
		Tracks: []Track{
			{Source: dying, Path: filepath.Join(dir, "dying.wav"), Label: "dying"},
			{Source: surviving, Path: filepath.Join(dir, "surv.wav"), Label: "surv"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runErr := s.Run(ctx)
	if runErr == nil {
		t.Fatalf("expected partial-failure error, got nil")
	}
	if !errors.Is(runErr, source.ErrDeviceDisconnected) {
		t.Errorf("expected errors.Is(ErrDeviceDisconnected), got %v", runErr)
	}

	assertWavDataSize(t, filepath.Join(dir, "dying.wav"), 40, 0xAA) // 2 frames × 20
	assertWavDataSize(t, filepath.Join(dir, "surv.wav"), 200, 0xBB) // 10 frames × 20
}

func TestRun_StartupFailure_RollsBackCleanly(t *testing.T) {
	dir := t.TempDir()
	f := source.Format{SampleRate: 48000, Channels: 1, BitsPerSample: 16}

	good := newFakeSource(f, makeFrames(10, 20, 0xAA))
	bad := newFakeSource(f, nil)
	bad.startErr = errors.New("simulated device-open failure")

	s, err := New(Config{
		Tracks: []Track{
			{Source: good, Path: filepath.Join(dir, "good.wav"), Label: "good"},
			{Source: bad, Path: filepath.Join(dir, "bad.wav"), Label: "bad"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	runErr := s.Run(context.Background())
	if runErr == nil {
		t.Fatalf("expected startup error")
	}

	// Neither wav file should exist on disk — rollback should have cleaned up.
	if _, err := os.Stat(filepath.Join(dir, "good.wav")); !os.IsNotExist(err) {
		t.Errorf("good.wav should not exist after rollback; err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "bad.wav")); !os.IsNotExist(err) {
		t.Errorf("bad.wav should not exist after rollback; err=%v", err)
	}
}

func TestRun_WavCreateFailure_RollsBackCleanly(t *testing.T) {
	dir := t.TempDir()
	f := source.Format{SampleRate: 48000, Channels: 1, BitsPerSample: 16}

	good := newFakeSource(f, makeFrames(5, 20, 0xAA))
	other := newFakeSource(f, makeFrames(5, 20, 0xBB))

	// Second track points at a directory that does not and cannot exist,
	// causing wav.Create to fail in Phase 2 (after both sources have
	// been successfully Start()ed in Phase 1).
	badPath := filepath.Join(dir, "definitely-missing-subdir", "second.wav")
	goodPath := filepath.Join(dir, "first.wav")

	s, err := New(Config{
		Tracks: []Track{
			{Source: good, Path: goodPath, Label: "first"},
			{Source: other, Path: badPath, Label: "second"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	runErr := s.Run(context.Background())
	if runErr == nil {
		t.Fatalf("expected Run to fail due to wav.Create on track 'second'")
	}

	// After Phase-2 rollback, the first wav should have been removed.
	if _, err := os.Stat(goodPath); !os.IsNotExist(err) {
		t.Errorf("first.wav should not exist after Phase-2 rollback; stat err=%v", err)
	}
	if _, err := os.Stat(badPath); !os.IsNotExist(err) {
		t.Errorf("second.wav should not exist; stat err=%v", err)
	}
}

// flakyWriter wraps a real wav.Writer but fails Flush() every call
// after the first N successful calls, simulating a mid-session disk
// error. All other methods forward to the real writer.
type flakyWriter struct {
	inner          Writer
	flushesAllowed int
	flushCallCount int
	mu             sync.Mutex
}

func (f *flakyWriter) WriteFrame(fr source.Frame) error { return f.inner.WriteFrame(fr) }

func (f *flakyWriter) Flush() error {
	f.mu.Lock()
	f.flushCallCount++
	count := f.flushCallCount
	f.mu.Unlock()
	if count > f.flushesAllowed {
		return errors.New("flakyWriter: simulated flush failure")
	}
	return f.inner.Flush()
}

func (f *flakyWriter) Close() error { return f.inner.Close() }

func TestRun_FlushTickerError_EmitsEventErrorAndContinues(t *testing.T) {
	dir := t.TempDir()
	f := source.Format{SampleRate: 48000, Channels: 1, BitsPerSample: 16}

	// Enough frames to run for multiple flush intervals.
	src := newFakeSource(f, makeFrames(20, 20, 0xAA))
	src.delay = 10 * time.Millisecond // ~200ms total runtime

	var errorEvents int32
	var flakyW *flakyWriter

	factory := func(path string, format source.Format) (Writer, error) {
		real, err := wav.Create(path, format)
		if err != nil {
			return nil, err
		}
		flakyW = &flakyWriter{inner: real, flushesAllowed: 1}
		return flakyW, nil
	}

	s, err := New(Config{
		Tracks:        []Track{{Source: src, Path: filepath.Join(dir, "out.wav"), Label: "flaky"}},
		FlushInterval: 20 * time.Millisecond,
		WriterFactory: factory,
		OnEvent: func(ev Event) {
			if ev.Kind == EventError && ev.Track == "flaky" {
				atomic.AddInt32(&errorEvents, 1)
			}
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// Run may return an error or nil; the key assertion is on event emission.
	_ = s.Run(ctx)

	if n := atomic.LoadInt32(&errorEvents); n < 1 {
		t.Errorf("expected at least 1 EventError, got %d", n)
	}
	// Frames before the first flush failure should still have been written.
	// We don't assert on file content directly since the flaky writer's
	// Close() path may also error — the key assertion is event emission.
}

func TestRun_Segmentation_RotatesEveryInterval(t *testing.T) {
	dir := t.TempDir()
	f := source.Format{SampleRate: 48000, Channels: 1, BitsPerSample: 16}

	// Produce frames over ~300ms; with 100ms segments we expect 3-4 rotations.
	src := newFakeSource(f, makeFrames(30, 20, 0xAA))
	src.delay = 10 * time.Millisecond

	var rotations int32
	s, err := New(Config{
		Tracks: []Track{
			{Source: src, Path: filepath.Join(dir, "mic.wav"), Label: "mic"},
		},
		SegmentDuration: 100 * time.Millisecond,
		FlushInterval:   50 * time.Millisecond,
		OnEvent: func(ev Event) {
			if ev.Kind == EventSegmentRotated && ev.Track == "mic" {
				atomic.AddInt32(&rotations, 1)
			}
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Expect at least 2 segment files on disk.
	matches, err := filepath.Glob(filepath.Join(dir, "mic-*.wav"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) < 2 {
		t.Errorf("expected at least 2 segment files, got %d: %v", len(matches), matches)
	}

	// Every segment file must be a valid (non-empty) WAV.
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil {
			t.Errorf("stat %s: %v", m, err)
			continue
		}
		if info.Size() <= 44 {
			t.Errorf("%s too small (%d bytes); header-only", m, info.Size())
		}
	}

	// Rotation events should have fired.
	if n := atomic.LoadInt32(&rotations); n < 1 {
		t.Errorf("expected at least 1 rotation event, got %d", n)
	}

	// Unrotated path should NOT exist (no mic.wav, only mic-001.wav etc).
	if _, err := os.Stat(filepath.Join(dir, "mic.wav")); !os.IsNotExist(err) {
		t.Errorf("mic.wav (unsegmented name) should not exist; err=%v", err)
	}
}

func TestRun_WritesManifestJSON(t *testing.T) {
	dir := t.TempDir()
	f := source.Format{SampleRate: 48000, Channels: 1, BitsPerSample: 16}

	micSrc := newFakeSource(f, makeFrames(5, 20, 0xAA))
	sysSrc := newFakeSource(f, makeFrames(3, 40, 0xBB))

	s, err := New(Config{
		FlushInterval: 50 * time.Millisecond,
		Tracks: []Track{
			{Source: micSrc, Path: filepath.Join(dir, "mic.wav"), Label: "mic"},
			{Source: sysSrc, Path: filepath.Join(dir, "system.wav"), Label: "system"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	manifestPath := filepath.Join(dir, "manifest.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}

	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, raw)
	}

	if m.Version != ManifestVersion {
		t.Errorf("version: got %d want %d", m.Version, ManifestVersion)
	}
	if !m.EndedAt.After(m.StartedAt) {
		t.Errorf("ended_at not after started_at: %v .. %v", m.StartedAt, m.EndedAt)
	}
	if m.DurationSeconds <= 0 || m.DurationSeconds > 5 {
		t.Errorf("duration_seconds out of range: %v", m.DurationSeconds)
	}
	if len(m.Tracks) != 2 {
		t.Fatalf("tracks: got %d want 2", len(m.Tracks))
	}

	labels := []string{m.Tracks[0].Label, m.Tracks[1].Label}
	if labels[0] != "mic" || labels[1] != "system" {
		t.Errorf("track order: got %v want [mic system]", labels)
	}

	for _, tr := range m.Tracks {
		if tr.FramesWritten <= 0 {
			t.Errorf("%s: frames_written=%d, want > 0", tr.Label, tr.FramesWritten)
		}
		if tr.BytesWritten <= 0 {
			t.Errorf("%s: bytes_written=%d, want > 0", tr.Label, tr.BytesWritten)
		}
		if filepath.IsAbs(tr.Path) {
			t.Errorf("%s: path should be basename, got %q", tr.Label, tr.Path)
		}
		if tr.Path != tr.Label+".wav" {
			t.Errorf("%s: path=%q want %s.wav", tr.Label, tr.Path, tr.Label)
		}
		if tr.Format.SampleRate != 48000 {
			t.Errorf("%s: sample_rate=%d want 48000", tr.Label, tr.Format.SampleRate)
		}
		if tr.Error != nil {
			t.Errorf("%s: unexpected error: %v", tr.Label, *tr.Error)
		}
	}
}
