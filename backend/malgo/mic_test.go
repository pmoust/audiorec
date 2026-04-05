package malgo

import (
	"context"
	"errors"
	"testing"
	"time"

	ma "github.com/gen2brain/malgo"
	"github.com/pmoust/audiorec/source"
)

// TestCapture_OpenClose opens the default mic, reads for 300ms, closes.
// This is a soft smoke test: it skips when the platform returns no frames
// (e.g., a headless CI runner that exposes CoreAudio/PipeWire device
// handles but has no microphone delivering samples). Deterministic
// hardware-free coverage of the full capture lifecycle is provided by
// TestCapture_NullBackend_EndToEnd.
func TestCapture_OpenClose(t *testing.T) {
	cap := NewCapture(CaptureConfig{Channels: 1})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	if err := cap.Start(ctx); err != nil {
		t.Skipf("capture unavailable: %v", err)
	}
	defer cap.Close()

	var frames int
	for f := range cap.Frames() {
		if len(f.Data) == 0 {
			t.Errorf("empty frame")
		}
		frames++
	}
	t.Logf("received %d frames, format=%+v, drops=%d", frames, cap.Format(), cap.Drops())
	if frames == 0 {
		t.Skip("no frames delivered; no real microphone on this host")
	}
	if err := cap.Err(); err != nil {
		t.Errorf("Err: %v", err)
	}
}

func TestMapError(t *testing.T) {
	cases := []struct {
		name  string
		input error
		want  error // sentinel to check via errors.Is, or nil for "should not match any sentinel"
	}{
		{"nil passes through", nil, nil},
		{"permission denied", errors.New("miniaudio: permission denied"), source.ErrPermissionDenied},
		{"not authorized", errors.New("ma_device_init: not authorized"), source.ErrPermissionDenied},
		{"device not found", errors.New("device not found"), source.ErrDeviceNotFound},
		{"no such device", errors.New("alsa: no such device"), source.ErrDeviceNotFound},
		{"disconnected", errors.New("audio device disconnected"), source.ErrDeviceDisconnected},
		{"unrelated error", errors.New("something else entirely"), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := mapError(c.input)
			if c.want == nil {
				if got == nil && c.input == nil {
					return // nil input → nil output is expected
				}
				// For non-nil "unrelated" input, got should be non-nil but not match any sentinel.
				if errors.Is(got, source.ErrPermissionDenied) ||
					errors.Is(got, source.ErrDeviceNotFound) ||
					errors.Is(got, source.ErrDeviceDisconnected) {
					t.Errorf("mapError(%v) unexpectedly matched a sentinel: %v", c.input, got)
				}
				return
			}
			if !errors.Is(got, c.want) {
				t.Errorf("mapError(%v) = %v; want errors.Is %v", c.input, got, c.want)
			}
		})
	}
}

// backendNullFixed works around a malgo v0.11.24 enum bug: the Go
// Backend enum at enumerations.go skips ma_backend_custom (C value 13),
// so malgo.BackendNull resolves to 13 (which is actually ma_backend_custom
// in C) rather than 14 (ma_backend_null). Passing malgo.BackendNull to
// ma.InitContext therefore fails with "No backend" because the C side
// interprets it as a request for the custom backend with no callbacks.
// This constant forces the real null backend regardless of that mismatch
// and should be removed once malgo fixes its enum upstream.
const backendNullFixed ma.Backend = 14

// TestCapture_NullBackend_EndToEnd exercises the full Capture lifecycle
// (Start → frame delivery → ctx cancel → stopDevice → Err) against
// miniaudio's null backend. The null backend has no hardware or OS-audio
// requirements, making this test deterministic on every runner including
// headless CI.
func TestCapture_NullBackend_EndToEnd(t *testing.T) {
	cfg := CaptureConfig{
		Channels: 1,
		Backends: []ma.Backend{backendNullFixed},
	}
	cap := NewCapture(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	if err := cap.Start(ctx); err != nil {
		t.Fatalf("Start with null backend: %v", err)
	}

	// Drain frames until the channel closes (ctx timeout will cancel).
	var frames int
	var totalBytes int
	for f := range cap.Frames() {
		frames++
		totalBytes += len(f.Data)
	}
	if err := cap.Err(); err != nil {
		t.Errorf("Err after clean shutdown: %v", err)
	}
	if frames == 0 {
		t.Errorf("expected at least one frame from null backend; got 0")
	}
	if totalBytes == 0 {
		t.Errorf("expected non-zero bytes; got 0")
	}
	t.Logf("received %d frames, %d bytes, format=%+v", frames, totalBytes, cap.Format())

	// Ensure Close is safe after ctx cancellation teardown.
	if err := cap.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}
