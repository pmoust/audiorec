//go:build linux

package malgo

import (
	"fmt"
	"strings"

	ma "github.com/gen2brain/malgo"
	"github.com/pmoust/audiorec/source"
)

// DefaultSystemAudioDevice returns the DeviceInfo for the Linux default
// sink's monitor source. PulseAudio and PipeWire (via pulse compat) both
// expose sink monitors as "<sink-name>.monitor" capture devices, so we
// resolve the default by listing devices and picking the one whose name
// ends in ".monitor" and whose underlying sink is the default.
//
// Heuristic: prefer the device with IsDefault=true among those with kind
// SystemAudio. If none is marked default, return the first SystemAudio
// device. If there are no monitor devices at all, return ErrDeviceNotFound.
func DefaultSystemAudioDevice() (source.DeviceInfo, error) {
	devs, err := Enumerate()
	if err != nil {
		return source.DeviceInfo{}, err
	}
	var first *source.DeviceInfo
	for i := range devs {
		d := devs[i]
		if d.Kind != source.SystemAudio {
			continue
		}
		if d.Default {
			return d, nil
		}
		if first == nil {
			first = &d
		}
	}
	if first != nil {
		return *first, nil
	}
	return source.DeviceInfo{}, fmt.Errorf("%w: no monitor source; pass --system explicitly", source.ErrDeviceNotFound)
}

// DefaultSystemAudioCaptureConfig returns a CaptureConfig populated with the
// DeviceID of the Linux default sink's monitor source. Returns ErrDeviceNotFound
// if no monitor device exists (pass --system explicitly in that case).
func DefaultSystemAudioCaptureConfig(channels int) (CaptureConfig, error) {
	ctx, err := ma.InitContext(nil, ma.ContextConfig{}, func(string) {})
	if err != nil {
		return CaptureConfig{}, fmt.Errorf("malgo: init context: %w", err)
	}
	defer func() {
		_ = ctx.Uninit()
		ctx.Free()
	}()

	devs, err := ctx.Devices(ma.Capture)
	if err != nil {
		return CaptureConfig{}, fmt.Errorf("malgo: list capture devices: %w", err)
	}

	// Prefer the device marked default among monitor sources; fall back to
	// the first monitor.
	var chosen *ma.DeviceInfo
	for i := range devs {
		if classifyKind(devs[i].Name()) != source.SystemAudio {
			continue
		}
		if devs[i].IsDefault == 1 {
			chosen = &devs[i]
			break
		}
		if chosen == nil {
			chosen = &devs[i]
		}
	}
	if chosen == nil {
		return CaptureConfig{}, fmt.Errorf("%w: no monitor source; pass --system explicitly", source.ErrDeviceNotFound)
	}

	// Copy the DeviceID out of the slice element so the returned pointer
	// remains valid after this function's `devs` slice goes out of scope.
	id := chosen.ID
	return CaptureConfig{DeviceID: &id, Channels: channels}, nil
}

// Ensure classifyKind's suffix check stays in sync with callers.
var _ = strings.HasSuffix
