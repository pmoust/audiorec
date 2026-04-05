//go:build !darwin

// Package sck provides a macOS ScreenCaptureKit-backed system-audio Source.
// On non-darwin builds, the package exports the same types but every
// operation returns ErrUnsupportedOS.
package sck

import (
	"context"

	"github.com/pmoust/audiorec/source"
)

// SystemAudioConfig configures per-app audio capture on macOS 13+.
// Leave both slices empty for the default "capture everything" behavior.
// IncludeBundleIDs and ExcludeBundleIDs are mutually exclusive.
//
// Note: pre-macOS 14.4, ScreenCaptureKit's SCContentFilter applied
// application filtering to visual content but did not fully isolate
// audio. On macOS 14.4 and later, audio capture respects the filter.
// On older versions, audiorec still constructs the filter but the
// captured audio may include system-wide sound from unrelated apps.
type SystemAudioConfig struct {
	IncludeBundleIDs []string
	ExcludeBundleIDs []string
}

// Capture is a stub on non-darwin platforms.
type Capture struct{}

// NewSystemAudio returns a stub Source on non-darwin platforms.
func NewSystemAudio() *Capture { return NewSystemAudioWithConfig(SystemAudioConfig{}) }

// NewSystemAudioWithConfig returns a stub Source on non-darwin platforms.
// The config is ignored; non-darwin builds return ErrUnsupportedOS from Start.
func NewSystemAudioWithConfig(cfg SystemAudioConfig) *Capture { return &Capture{} }

func (c *Capture) Format() source.Format       { return source.Format{} }
func (c *Capture) Start(context.Context) error { return source.ErrUnsupportedOS }
func (c *Capture) Frames() <-chan source.Frame {
	ch := make(chan source.Frame)
	close(ch)
	return ch
}
func (c *Capture) Err() error   { return source.ErrUnsupportedOS }
func (c *Capture) Close() error { return nil }
