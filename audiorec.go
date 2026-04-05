// Package audiorec is the top-level public API for the audiorec library.
// It re-exports the most commonly used types from sub-packages so library
// users can import a single package for typical usage.
//
// Example:
//
//	import "github.com/pmoust/audiorec"
//
//	mic := audiorec.NewMicCapture(audiorec.CaptureConfig{})
//	sess, _ := audiorec.NewSession(audiorec.SessionConfig{
//	    Tracks: []audiorec.Track{{Source: mic, Path: "mic.wav", Label: "mic"}},
//	})
//	_ = sess.Run(ctx)
package audiorec

import (
	"github.com/pmoust/audiorec/backend/malgo"
	"github.com/pmoust/audiorec/backend/sck"
	"github.com/pmoust/audiorec/session"
	"github.com/pmoust/audiorec/source"
)

// Re-exports from source.
type (
	Source     = source.Source
	Format     = source.Format
	Frame      = source.Frame
	DeviceInfo = source.DeviceInfo
	Kind       = source.Kind
)

const (
	KindMic         = source.Mic
	KindSystemAudio = source.SystemAudio
)

var (
	ErrPermissionDenied   = source.ErrPermissionDenied
	ErrDeviceNotFound     = source.ErrDeviceNotFound
	ErrDeviceDisconnected = source.ErrDeviceDisconnected
	ErrUnsupportedFormat  = source.ErrUnsupportedFormat
	ErrUnsupportedOS      = source.ErrUnsupportedOS
)

// Re-exports from session.
type (
	Session       = session.Session
	SessionConfig = session.Config
	Track         = session.Track
	Event         = session.Event
	Stats         = session.Stats
)

func NewSession(cfg SessionConfig) (*Session, error) { return session.New(cfg) }

// Backend constructors. These are thin wrappers so library callers don't
// need to import sub-packages for the common case.
type CaptureConfig = malgo.CaptureConfig

func NewMicCapture(cfg CaptureConfig) *malgo.Capture { return malgo.NewCapture(cfg) }

func FindCaptureConfig(query string, channels int) (CaptureConfig, error) {
	return malgo.FindCaptureConfig(query, channels)
}

// NewSystemAudioCapture returns a Source for system audio on the current
// platform. On macOS it uses ScreenCaptureKit; on Linux it uses a malgo
// capture targeting the default sink's monitor source. Callers that need
// explicit device selection should construct backends directly.
func NewSystemAudioCapture() Source {
	return newSystemAudioDefault()
}

// EnumerateMalgoDevices is a convenience re-export.
func EnumerateMalgoDevices() ([]DeviceInfo, error) { return malgo.Enumerate() }

// newSystemAudioDefault is provided by platform-specific files below.
var _ = sck.NewSystemAudio // ensure sck is reachable on both platforms (stub on non-darwin)
