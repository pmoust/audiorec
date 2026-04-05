// Package malgo implements audiorec backends backed by miniaudio via the
// malgo Go bindings. It provides microphone capture on both macOS and
// Linux, plus Linux system-audio capture via PulseAudio/PipeWire monitor
// sources.
//
// This backend requires cgo. Pure-Go builds are not supported.
package malgo

import (
	"fmt"
	"strings"

	ma "github.com/gen2brain/malgo"
	"github.com/pmoust/audiorec/source"
)

// Enumerate lists audio capture devices visible to the malgo backend.
// On Linux this includes both microphones and monitor sources (.monitor
// suffix); callers can partition by name. On macOS this includes only
// capture devices (microphones) — system audio on macOS is handled by the
// sck backend, not this one.
func Enumerate() ([]source.DeviceInfo, error) {
	ctx, err := ma.InitContext(nil, ma.ContextConfig{}, func(msg string) {})
	if err != nil {
		return nil, fmt.Errorf("malgo: init context: %w", err)
	}
	defer func() {
		_ = ctx.Uninit()
		ctx.Free()
	}()

	devs, err := ctx.Devices(ma.Capture)
	if err != nil {
		return nil, fmt.Errorf("malgo: list capture devices: %w", err)
	}

	out := make([]source.DeviceInfo, 0, len(devs))
	for _, d := range devs {
		info := source.DeviceInfo{
			ID:      d.ID.String(),
			Name:    d.Name(),
			Kind:    classifyKind(d.Name()),
			Default: d.IsDefault == 1,
		}
		out = append(out, info)
	}
	return out, nil
}

// classifyKind heuristically classifies a capture device by name. PulseAudio
// and PipeWire expose monitor sources with a ".monitor" suffix on their
// sink name. Everything else is treated as a microphone. On macOS, malgo
// never reports loopback devices, so everything from Enumerate is a mic.
func classifyKind(name string) source.Kind {
	if len(name) >= 8 && name[len(name)-8:] == ".monitor" {
		return source.SystemAudio
	}
	return source.Mic
}

// FindCaptureConfig looks up a capture device by its ID or Name (name match
// is case-insensitive, either exact or substring). Returns a CaptureConfig
// with the matching ma.DeviceID populated. Returns ErrDeviceNotFound if no
// device matches.
func FindCaptureConfig(query string, channels int) (CaptureConfig, error) {
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

	lowerQ := strings.ToLower(query)
	var chosen *ma.DeviceInfo
	// First pass: exact ID match, then exact case-insensitive name match.
	for i := range devs {
		if devs[i].ID.String() == query {
			chosen = &devs[i]
			break
		}
	}
	if chosen == nil {
		for i := range devs {
			if strings.EqualFold(devs[i].Name(), query) {
				chosen = &devs[i]
				break
			}
		}
	}
	// Second pass: case-insensitive substring on name.
	if chosen == nil {
		for i := range devs {
			if strings.Contains(strings.ToLower(devs[i].Name()), lowerQ) {
				chosen = &devs[i]
				break
			}
		}
	}
	if chosen == nil {
		return CaptureConfig{}, fmt.Errorf("%w: %q (run 'audiorec devices' to list)", source.ErrDeviceNotFound, query)
	}
	id := chosen.ID
	return CaptureConfig{DeviceID: &id, Channels: channels}, nil
}
