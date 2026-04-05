package session

import (
	"context"
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
	for i := 0; i < expectedBytes; i++ {
		if b[44+i] != fill {
			t.Errorf("%s: byte %d: got %#x want %#x", path, i, b[44+i], fill)
			return
		}
	}
}
