// Package opus implements an OggOpus writer that satisfies the session.Writer interface.
// It encodes PCM audio to Opus format and writes to OggOpus container.
package opus

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/pmoust/audiorec/ogg"
	"github.com/pmoust/audiorec/source"
	goopus "gopkg.in/hraban/opus.v2"
)

// Writer streams Opus-encoded frames to an OggOpus file.
type Writer struct {
	mu            sync.Mutex
	f             *os.File
	ogg           *ogg.PageWriter
	enc           *goopus.Encoder
	fmt           source.Format
	buf           []byte // PCM accumulator
	frameSize     int    // 960 samples per channel per Opus frame
	granule       int64  // running PCM sample count at 48kHz
	closed        bool
	needsResample bool
	resampleRatio float64
	resamplePos   float64
	resampleBuf   []float32 // intermediate buffer for resampler
	bitrate       int
}

// Option is a functional option for configuring the Writer.
type Option func(*Writer)

// WithBitrate sets the Opus encoding bitrate in bits per second.
func WithBitrate(bps int) Option {
	return func(w *Writer) {
		w.bitrate = bps
	}
}

// Create opens path for writing and initializes an OggOpus encoder.
func Create(path string, f source.Format, opts ...Option) (*Writer, error) {
	// Validate channels (Opus channel mapping family 0 supports mono/stereo).
	if f.Channels < 1 || f.Channels > 2 {
		return nil, fmt.Errorf("%w: opus supports 1-2 channels, got %d", source.ErrUnsupportedFormat, f.Channels)
	}

	// Open file.
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("opus.Create: %w", err)
	}

	w := &Writer{
		f:         file,
		fmt:       f,
		frameSize: 960, // 20ms at 48kHz
		bitrate:   48000,
	}

	// Apply options.
	for _, opt := range opts {
		opt(w)
	}

	// Create Ogg page writer.
	w.ogg = ogg.NewPageWriter(file)

	// Create Opus encoder at 48kHz.
	enc, err := goopus.NewEncoder(48000, f.Channels, goopus.AppVoIP)
	if err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("opus.Create: encoder: %w", err)
	}
	w.enc = enc

	// Set bitrate.
	if err := enc.SetBitrate(w.bitrate); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("opus.Create: set bitrate: %w", err)
	}

	// Configure resampler if source rate != 48kHz.
	if f.SampleRate != 48000 {
		w.needsResample = true
		w.resampleRatio = float64(48000) / float64(f.SampleRate)
		w.resampleBuf = make([]float32, 0, w.frameSize*2)
	}

	// Write header pages: OpusHead and OpusTags.
	if err := writeOpusHead(w.ogg, f.Channels); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("opus.Create: write OpusHead: %w", err)
	}

	if err := writeOpusTags(w.ogg); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("opus.Create: write OpusTags: %w", err)
	}

	// Sync to ensure headers are on disk.
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("opus.Create: sync: %w", err)
	}

	return w, nil
}

// WriteFrame appends a frame to the OggOpus stream.
func (w *Writer) WriteFrame(sf source.Frame) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errors.New("opus: WriteFrame on closed writer")
	}

	nSamples := sf.NumFrames
	if nSamples == 0 {
		return nil
	}

	// TODO: Handle resampling if needed.
	// TODO: Append to buffer, accumulate frames, encode when full.
	// TODO: Write Ogg pages.

	return nil
}

// Flush writes any buffered data and fsyncs the file.
func (w *Writer) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errors.New("opus: Flush on closed writer")
	}

	// TODO: Pad partial frame, encode, write.
	// TODO: Fsync.

	if err := w.f.Sync(); err != nil {
		return fmt.Errorf("opus: flush sync: %w", err)
	}
	return nil
}

// Close finalizes the OggOpus stream. Idempotent.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true

	// Flush any buffered data.
	// TODO: Write EOS page.

	// Sync and close file.
	syncErr := w.f.Sync()
	fileErr := w.f.Close()

	return errors.Join(syncErr, fileErr)
}

// writeOpusHead writes the OpusHead header page.
func writeOpusHead(pw *ogg.PageWriter, channels int) error {
	// OpusHead (RFC 7845): 19 bytes
	// 0-7: magic "OpusHead"
	// 8: version (1)
	// 9: channels
	// 10-11: pre-skip (3840 samples = 80ms at 48kHz, little-endian)
	// 12-15: input sample rate (informational, little-endian)
	// 16-17: output gain (0, little-endian)
	// 18: channel mapping family (0 for mono/stereo)

	head := make([]byte, 19)
	copy(head[0:8], "OpusHead")
	head[8] = 1                     // version
	head[9] = byte(channels)        // channels
	head[10], head[11] = 0x00, 0x0F // pre-skip = 3840 (little-endian)
	// input sample rate (48000 Hz, little-endian)
	head[12], head[13], head[14], head[15] = 0x80, 0xBB, 0x00, 0x00
	// output gain (0, little-endian)
	head[16], head[17] = 0x00, 0x00
	head[18] = 0 // channel mapping family 0

	return pw.WriteBOS(0, head)
}

// writeOpusTags writes the OpusTags header page.
func writeOpusTags(pw *ogg.PageWriter) error {
	// OpusTags (RFC 7845): vendor string + comment count
	// 0-7: magic "OpusTags"
	// 8-11: vendor length (little-endian)
	// 12-...: vendor string
	// ...-...: comment count (4 bytes, little-endian)

	vendor := "audiorec"
	tags := make([]byte, 8+4+len(vendor)+4)
	offset := 0

	copy(tags[offset:offset+8], "OpusTags")
	offset += 8

	// Vendor length (little-endian).
	tags[offset], tags[offset+1], tags[offset+2], tags[offset+3] = byte(len(vendor)), 0, 0, 0
	offset += 4

	copy(tags[offset:offset+len(vendor)], vendor)
	offset += len(vendor)

	// Comment count (0).
	tags[offset], tags[offset+1], tags[offset+2], tags[offset+3] = 0, 0, 0, 0

	return pw.WritePage(0, tags)
}
