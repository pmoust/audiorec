//go:build darwin

package sck

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation -framework ScreenCaptureKit -framework CoreMedia

#include <stdint.h>
#include <stdlib.h>
#include "sck_bridge.h"

extern void audiorecSCKAudioCallback(float* data, int numFrames, int channels, int sampleRate, void* user);

// audiorec_sck_create_id takes the Go-side registry id as a uintptr_t and
// forwards it to sck_capture_create as a void*. Doing the integer→void*
// cast on the C side avoids a Go-side unsafe.Pointer(uintptr) conversion
// that go vet flags under unsafe.Pointer rule 6 — even though the integer
// is never a real Go pointer, bare go vet cannot be silenced with comments.
static inline sck_capture_t* audiorec_sck_create_id(uintptr_t id) {
    return sck_capture_create((sck_audio_cb)audiorecSCKAudioCallback, (void*)id);
}
*/
import "C"

import (
	"context"
	"fmt"
	"sync"
	"time"
	"unsafe"

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

// Capture is the darwin ScreenCaptureKit-backed system-audio Source.
type Capture struct {
	mu      sync.Mutex
	handle  *C.sck_capture_t
	frames  chan source.Frame
	format  source.Format
	config  SystemAudioConfig
	err     error
	started bool
	closed  bool
	closeCh chan struct{}
}

// NewSystemAudio returns an un-started macOS system-audio Source. The
// first Start call will trigger the Screen Recording permission prompt
// if the user has not granted it; a denial surfaces as ErrPermissionDenied.
func NewSystemAudio() *Capture {
	return NewSystemAudioWithConfig(SystemAudioConfig{})
}

// NewSystemAudioWithConfig returns a Source configured with the given
// filter. Empty IncludeBundleIDs and ExcludeBundleIDs means "capture
// all system audio" (same as NewSystemAudio()).
func NewSystemAudioWithConfig(cfg SystemAudioConfig) *Capture {
	return &Capture{config: cfg}
}

// Registry maps C user pointers (uintptr values) back to Go Capture
// pointers so the audio callback can route frames to the right Source.
var (
	registryMu sync.Mutex
	registry   = map[uintptr]*Capture{}
	nextID     uintptr
)

func register(c *Capture) uintptr {
	registryMu.Lock()
	defer registryMu.Unlock()
	nextID++
	id := nextID
	registry[id] = c
	return id
}

func unregister(id uintptr) {
	registryMu.Lock()
	delete(registry, id)
	registryMu.Unlock()
}

func lookup(id uintptr) *Capture {
	registryMu.Lock()
	defer registryMu.Unlock()
	return registry[id]
}

//export audiorecSCKAudioCallback
func audiorecSCKAudioCallback(data *C.float, numFrames C.int, channels C.int, sampleRate C.int, user unsafe.Pointer) {
	id := uintptr(user)
	c := lookup(id)
	if c == nil {
		return
	}
	nf := int(numFrames)
	ch := int(channels)
	if nf <= 0 || ch <= 0 {
		return
	}
	// One-time format publish.
	c.mu.Lock()
	if c.format.SampleRate == 0 {
		c.format = source.Format{
			SampleRate:    int(sampleRate),
			Channels:      ch,
			BitsPerSample: 32,
			Float:         true,
		}
	}
	c.mu.Unlock()

	nSamples := nf * ch
	byteLen := nSamples * 4 // float32
	// Use C.GoBytes to safely convert the C float* data to a Go byte slice.
	buf := C.GoBytes(unsafe.Pointer(data), C.int(byteLen))

	f := source.Frame{
		Data:      buf,
		NumFrames: nf,
		Timestamp: time.Now(),
	}
	select {
	case c.frames <- f:
	default:
		// Drop-oldest.
		select {
		case <-c.frames:
		default:
		}
		select {
		case c.frames <- f:
		default:
		}
	}
}

func (c *Capture) Format() source.Format {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.format
}

func (c *Capture) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return fmt.Errorf("sck: already started")
	}
	c.mu.Unlock()

	c.frames = make(chan source.Frame, 32)
	c.closeCh = make(chan struct{})

	id := register(c)
	c.handle = C.audiorec_sck_create_id(C.uintptr_t(id))
	if c.handle == nil {
		unregister(id)
		return fmt.Errorf("sck: create failed")
	}

	// Build C string array from c.config.IncludeBundleIDs or ExcludeBundleIDs.
	var cBundleIDs **C.char
	var count C.int
	var include C.int
	var cstrs []*C.char

	if len(c.config.IncludeBundleIDs) > 0 {
		ids := c.config.IncludeBundleIDs
		include = 1
		cstrs = make([]*C.char, len(ids))
		for i, s := range ids {
			cstrs[i] = C.CString(s)
		}
		cBundleIDs = (**C.char)(unsafe.Pointer(&cstrs[0]))
		count = C.int(len(ids))
	} else if len(c.config.ExcludeBundleIDs) > 0 {
		ids := c.config.ExcludeBundleIDs
		include = 0
		cstrs = make([]*C.char, len(ids))
		for i, s := range ids {
			cstrs[i] = C.CString(s)
		}
		cBundleIDs = (**C.char)(unsafe.Pointer(&cstrs[0]))
		count = C.int(len(ids))
	}

	rc := C.sck_capture_start_filtered(c.handle, cBundleIDs, count, include)

	// Free C strings after the call returns.
	for _, s := range cstrs {
		C.free(unsafe.Pointer(s))
	}

	if rc != 0 {
		code := int(C.sck_capture_last_error_code(c.handle))
		C.sck_capture_destroy(c.handle)
		c.handle = nil
		unregister(id)
		return mapSCKError(code)
	}

	c.mu.Lock()
	c.started = true
	c.mu.Unlock()

	// Watcher: ctx cancel or Close triggers teardown.
	go func() {
		select {
		case <-ctx.Done():
		case <-c.closeCh:
		}
		c.teardown(id)
	}()
	return nil
}

func (c *Capture) teardown(id uintptr) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	handle := c.handle
	ch := c.frames
	c.handle = nil
	c.mu.Unlock()

	if handle != nil {
		C.sck_capture_stop(handle)
		C.sck_capture_destroy(handle)
	}
	unregister(id)
	if ch != nil {
		close(ch)
	}
}

func (c *Capture) Frames() <-chan source.Frame { return c.frames }

func (c *Capture) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *Capture) Close() error {
	c.mu.Lock()
	ch := c.closeCh
	c.mu.Unlock()
	if ch != nil {
		select {
		case <-ch:
		default:
			close(ch)
		}
	}
	return nil
}

func mapSCKError(code int) error {
	switch code {
	case 1:
		return fmt.Errorf("%w: macOS Screen Recording not granted", source.ErrPermissionDenied)
	case 2:
		return fmt.Errorf("%w: no shareable content (no displays)", source.ErrDeviceNotFound)
	case 3:
		return fmt.Errorf("%w: SCStream init failed", source.ErrBackendFailure)
	case 4:
		return fmt.Errorf("%w: SCStream start failed", source.ErrBackendFailure)
	default:
		return fmt.Errorf("%w: sck error code %d", source.ErrBackendFailure, code)
	}
}
