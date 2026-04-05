package session

import (
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
