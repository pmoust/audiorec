// Package source defines the core types and interface that every audio
// capture backend implements. This is the contract that decouples the
// session orchestration layer from any specific platform API.
package source

import (
	"errors"
	"time"
)

// Format describes a PCM stream. audiorec never resamples in v1, so every
// backend reports whatever its device natively delivers.
type Format struct {
	SampleRate    int  `json:"sample_rate"`
	Channels      int  `json:"channels"`
	BitsPerSample int  `json:"bits_per_sample"`
	Float         bool `json:"float"`
}

// BytesPerFrame returns the number of bytes for one sample across all channels.
func (f Format) BytesPerFrame() int {
	return f.Channels * (f.BitsPerSample / 8)
}

// Frame is one chunk of interleaved PCM samples from a backend.
// The session copies Data before handing it to the wav writer, so backends
// may reuse the underlying buffer after the receiver reads the next Frame.
type Frame struct {
	Data      []byte    // interleaved PCM
	NumFrames int       // samples per channel in this buffer
	Timestamp time.Time // capture time of first sample in this frame
}

// Kind is the logical role of a device.
type Kind int

const (
	Mic Kind = iota
	SystemAudio
)

func (k Kind) String() string {
	switch k {
	case Mic:
		return "mic"
	case SystemAudio:
		return "system"
	default:
		return "unknown"
	}
}

// DeviceInfo is what CLI enumeration prints and what --mic / --system flags
// resolve against.
type DeviceInfo struct {
	ID      string // backend-stable identifier
	Name    string // human label
	Kind    Kind
	Default bool // is this the OS default for its Kind?
	Format  Format
}

// Exported typed errors. Backends wrap with fmt.Errorf("%w: detail", ...) so
// callers can use errors.Is.
var (
	ErrPermissionDenied   = errors.New("audiorec: permission denied")
	ErrDeviceNotFound     = errors.New("audiorec: device not found")
	ErrDeviceDisconnected = errors.New("audiorec: device disconnected")
	ErrUnsupportedFormat  = errors.New("audiorec: unsupported format")
	ErrUnsupportedOS      = errors.New("audiorec: backend not supported on this OS")
	ErrBackendFailure     = errors.New("audiorec: backend failure")
)
