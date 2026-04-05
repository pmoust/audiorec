//go:build linux

package audiorec

import (
	"context"

	"github.com/pmoust/audiorec/backend/malgo"
	_ "github.com/pmoust/audiorec/backend/sck" // compile sck stub on non-darwin
)

// newSystemAudioDefault returns a Source capturing the Linux default sink's
// monitor source (via PipeWire/PulseAudio). Returns a failingSource whose
// Start errors with ErrDeviceNotFound if no monitor exists.
func newSystemAudioDefault() Source {
	cfg, err := malgo.DefaultSystemAudioCaptureConfig(2)
	if err != nil {
		return &failingSource{err: err}
	}
	return malgo.NewCapture(cfg)
}

// newSystemAudioWithConfig ignores the config on Linux and returns the
// default system audio source. Per-app filtering is not supported on Linux.
func newSystemAudioWithConfig(cfg SystemAudioConfig) Source {
	return newSystemAudioDefault()
}

type failingSource struct {
	err error
	ch  chan Frame
}

func (f *failingSource) Format() Format                { return Format{} }
func (f *failingSource) Start(_ context.Context) error { return f.err }
func (f *failingSource) Frames() <-chan Frame {
	if f.ch == nil {
		f.ch = make(chan Frame)
		close(f.ch)
	}
	return f.ch
}
func (f *failingSource) Err() error   { return f.err }
func (f *failingSource) Close() error { return nil }
