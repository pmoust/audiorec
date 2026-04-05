package malgo

import (
	"context"
	"testing"
	"time"
)

// TestCapture_OpenClose opens the default mic, reads for 200ms, closes.
// Skipped if no audio server available.
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
		t.Errorf("expected at least one frame")
	}
	if err := cap.Err(); err != nil {
		t.Errorf("Err: %v", err)
	}
}
