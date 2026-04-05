package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pmoust/audiorec/source"
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
