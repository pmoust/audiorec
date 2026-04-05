//go:build linux

package malgo

import (
	"fmt"
	"strings"

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

// Ensure classifyKind's suffix check stays in sync with callers.
var _ = strings.HasSuffix
