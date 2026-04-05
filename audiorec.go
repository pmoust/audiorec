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
	ErrBackendFailure     = source.ErrBackendFailure
)

// Re-exports from session.
type (
	Session       = session.Session
	SessionConfig = session.Config
	Track         = session.Track
	Event         = session.Event
)

func NewSession(cfg SessionConfig) (*Session, error) { return session.New(cfg) }

// CaptureConfig is a re-export of malgo.CaptureConfig so library callers
// don't need to import backend/malgo for the common case of constructing
// a microphone or monitor capture.
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

// SystemAudioConfig is a type alias for platform-specific system audio
// configuration, primarily for per-app filtering on macOS 13+.
type SystemAudioConfig = sck.SystemAudioConfig

// NewSystemAudioCaptureWithConfig returns a Source configured with
// platform-specific per-app filtering. On macOS, it uses ScreenCaptureKit
// with the given filter. On Linux, IncludeBundleIDs and ExcludeBundleIDs
// are ignored; the function returns a default system audio source (monitor
// of the default sink). Callers on Linux that pass a non-empty config will
// have those filters silently ignored.
func NewSystemAudioCaptureWithConfig(cfg SystemAudioConfig) Source {
	return newSystemAudioWithConfig(cfg)
}

// EnumerateMalgoDevices is a convenience re-export.
func EnumerateMalgoDevices() ([]DeviceInfo, error) { return malgo.Enumerate() }

// newSystemAudioDefault and newSystemAudioWithConfig are provided by
// platform-specific files below.
