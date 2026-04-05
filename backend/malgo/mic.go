package malgo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	ma "github.com/gen2brain/malgo"
	"github.com/pmoust/audiorec/source"
)

// CaptureConfig configures a malgo-backed capture Source.
type CaptureConfig struct {
	DeviceID *ma.DeviceID // nil => OS default
	Channels int          // 0 => use device native (1 for mic, 2 for monitor typically)
	// SampleRate is informational only in v1 — we always request the device
	// native rate by passing 0 to miniaudio.
}

// NewCapture constructs a Source for a capture device. The returned Source
// does not touch the device until Start is called.
func NewCapture(cfg CaptureConfig) *Capture {
	return &Capture{cfg: cfg}
}

// Capture is the malgo-backed Source implementation used for microphones
// and Linux monitor sources. It is not safe for concurrent use.
type Capture struct {
	cfg CaptureConfig

	ctx    *ma.AllocatedContext
	dev    *ma.Device
	frames chan source.Frame
	format source.Format

	mu      sync.Mutex
	err     error
	started bool
	closed  bool
	drops   int64
	closeCh chan struct{}
}

func (c *Capture) Format() source.Format { return c.format }

// Start opens the device. On success, Frames() begins emitting.
func (c *Capture) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return errors.New("malgo: already started")
	}
	c.mu.Unlock()

	maCtx, err := ma.InitContext(nil, ma.ContextConfig{}, func(string) {})
	if err != nil {
		return fmt.Errorf("malgo: init context: %w", err)
	}

	devCfg := ma.DefaultDeviceConfig(ma.Capture)
	devCfg.Capture.Format = ma.FormatS16
	if c.cfg.Channels > 0 {
		devCfg.Capture.Channels = uint32(c.cfg.Channels)
	}
	if c.cfg.DeviceID != nil {
		devCfg.Capture.DeviceID = c.cfg.DeviceID.Pointer()
	}
	devCfg.SampleRate = 0 // native

	c.frames = make(chan source.Frame, 32)
	c.closeCh = make(chan struct{})

	onData := func(_, input []byte, frameCount uint32) {
		// Copy because malgo reuses this buffer on subsequent callbacks.
		buf := make([]byte, len(input))
		copy(buf, input)
		f := source.Frame{
			Data:      buf,
			NumFrames: int(frameCount),
			Timestamp: time.Now(),
		}
		select {
		case c.frames <- f:
		default:
			// Channel full: drop oldest, push newest.
			select {
			case <-c.frames:
			default:
			}
			select {
			case c.frames <- f:
			default:
			}
			c.mu.Lock()
			c.drops++
			c.mu.Unlock()
		}
	}

	dev, err := ma.InitDevice(maCtx.Context, devCfg, ma.DeviceCallbacks{Data: onData})
	if err != nil {
		_ = maCtx.Uninit()
		maCtx.Free()
		return fmt.Errorf("malgo: init device: %w", mapError(err))
	}
	if err := dev.Start(); err != nil {
		dev.Uninit()
		_ = maCtx.Uninit()
		maCtx.Free()
		return fmt.Errorf("malgo: start device: %w", mapError(err))
	}

	c.format = source.Format{
		SampleRate:    int(dev.SampleRate()),
		Channels:      int(devCfg.Capture.Channels),
		BitsPerSample: 16,
		Float:         false,
	}
	c.dev = dev
	c.ctx = maCtx
	c.started = true

	// Watcher: when the caller's ctx is cancelled, stop the device and
	// close the frames channel. This is the only goroutine we spawn.
	go func() {
		select {
		case <-ctx.Done():
		case <-c.closeCh:
		}
		c.stopDevice()
	}()

	return nil
}

// stopDevice tears down the malgo device and closes the frames channel.
// Safe to call multiple times; subsequent calls are no-ops.
func (c *Capture) stopDevice() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	dev, mctx, ch := c.dev, c.ctx, c.frames
	c.mu.Unlock()

	if dev != nil {
		dev.Uninit()
	}
	if mctx != nil {
		_ = mctx.Uninit()
		mctx.Free()
	}
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

// Drops returns the number of frames dropped due to consumer backpressure.
// Exposed primarily for tests and metrics.
func (c *Capture) Drops() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.drops
}

func (c *Capture) Close() error {
	c.mu.Lock()
	ch := c.closeCh
	c.mu.Unlock()
	if ch == nil {
		// Close called before Start; interface contract says this is safe.
		return nil
	}
	select {
	case <-ch:
		// already closing
	default:
		close(ch)
	}
	return nil
}

// mapError translates malgo errors to typed audiorec errors where possible.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	// miniaudio returns an int-based Result; malgo wraps it in an error with
	// a descriptive string. We do simple substring matching for the common
	// cases.
	msg := err.Error()
	switch {
	case strings.Contains(msg, "permission") || strings.Contains(msg, "not authorized"):
		return fmt.Errorf("%w: %v", source.ErrPermissionDenied, err)
	case strings.Contains(msg, "device not found") || strings.Contains(msg, "no such device"):
		return fmt.Errorf("%w: %v", source.ErrDeviceNotFound, err)
	case strings.Contains(msg, "disconnected"):
		return fmt.Errorf("%w: %v", source.ErrDeviceDisconnected, err)
	default:
		return err
	}
}
