//go:build !darwin

// Package sck provides a macOS ScreenCaptureKit-backed system-audio Source.
// On non-darwin builds, the package exports the same types but every
// operation returns ErrUnsupportedOS.
package sck

import (
	"context"

	"github.com/pmoust/audiorec/source"
)

// Capture is a stub on non-darwin platforms.
type Capture struct{}

// NewSystemAudio returns a stub Source on non-darwin platforms.
func NewSystemAudio() *Capture { return &Capture{} }

func (c *Capture) Format() source.Format       { return source.Format{} }
func (c *Capture) Start(context.Context) error { return source.ErrUnsupportedOS }
func (c *Capture) Frames() <-chan source.Frame {
	ch := make(chan source.Frame)
	close(ch)
	return ch
}
func (c *Capture) Err() error   { return source.ErrUnsupportedOS }
func (c *Capture) Close() error { return nil }
