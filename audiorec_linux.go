//go:build linux

package audiorec

import (
	"context"

	"github.com/pmoust/audiorec/backend/malgo"
)

// newSystemAudioDefault returns a Source for the Linux default monitor
// source. It verifies a monitor device exists via enumeration; if none
// does, it returns a Source whose Start fails with ErrDeviceNotFound.
// Otherwise it returns a malgo.Capture with nil DeviceID, relying on
// miniaudio to pick the default capture — on PipeWire/PulseAudio systems
// this is typically the default monitor when no mic is active.
func newSystemAudioDefault() Source {
	if _, err := malgo.DefaultSystemAudioDevice(); err != nil {
		return &failingSource{err: err}
	}
	return malgo.NewCapture(malgo.CaptureConfig{Channels: 2})
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
