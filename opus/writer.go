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
	// Opus encoding path currently expects int16 PCM input. Float32 sources
	// (e.g. macOS system audio via ScreenCaptureKit) are not yet supported.
	if f.Float {
		return nil, fmt.Errorf("%w: opus writer does not yet support float32 PCM; use --format wav or --format flac for float sources", source.ErrUnsupportedFormat)
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

	// If resampling is needed, resample first.
	var pcmData []byte
	if w.needsResample {
		pcmData = w.resampleAndConvert(sf)
	} else {
		pcmData = sf.Data
	}

	// Append PCM to buffer.
	w.buf = append(w.buf, pcmData...)

	// Encode full frames (960 samples per channel).
	frameBytes := w.frameSize * w.fmt.Channels * 2 // 2 bytes per sample (int16)
	for len(w.buf) >= frameBytes {
		// Extract one frame.
		frame := w.buf[:frameBytes]
		w.buf = w.buf[frameBytes:]

		// Convert bytes to int16 slice.
		pcm := bytesToInt16(frame)

		// Encode.
		encoded := make([]byte, 4096)
		n, err := w.enc.Encode(pcm, encoded)
		if err != nil {
			return fmt.Errorf("opus: encode: %w", err)
		}

		// Write to Ogg page.
		if err := w.ogg.WritePage(w.granule, encoded[:n]); err != nil {
			return fmt.Errorf("opus: write page: %w", err)
		}

		w.granule += int64(w.frameSize)
	}

	return nil
}

// Flush writes any buffered data and fsyncs the file.
func (w *Writer) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errors.New("opus: Flush on closed writer")
	}

	// If there's a partial frame, pad with silence and encode.
	frameBytes := w.frameSize * w.fmt.Channels * 2 // 2 bytes per sample (int16)
	if len(w.buf) > 0 {
		// Pad with silence.
		padding := make([]byte, frameBytes-len(w.buf))
		w.buf = append(w.buf, padding...)

		// Convert to int16 and encode.
		pcm := bytesToInt16(w.buf)
		encoded := make([]byte, 4096)
		n, err := w.enc.Encode(pcm, encoded)
		if err != nil {
			return fmt.Errorf("opus: flush encode: %w", err)
		}

		// Write to Ogg page.
		if err := w.ogg.WritePage(w.granule, encoded[:n]); err != nil {
			return fmt.Errorf("opus: flush write page: %w", err)
		}

		w.granule += int64(w.frameSize)
		w.buf = w.buf[:0] // clear buffer
	}

	// Fsync.
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

	// Flush any buffered data (encode and write partial frame).
	frameBytes := w.frameSize * w.fmt.Channels * 2 // 2 bytes per sample (int16)
	if len(w.buf) > 0 {
		// Pad with silence.
		padding := make([]byte, frameBytes-len(w.buf))
		w.buf = append(w.buf, padding...)

		// Convert to int16 and encode.
		pcm := bytesToInt16(w.buf)
		encoded := make([]byte, 4096)
		n, err := w.enc.Encode(pcm, encoded)
		if err != nil {
			return fmt.Errorf("opus: close encode: %w", err)
		}

		// Write to Ogg page.
		if err := w.ogg.WritePage(w.granule, encoded[:n]); err != nil {
			return fmt.Errorf("opus: close write page: %w", err)
		}

		w.granule += int64(w.frameSize)
	}

	// Write EOS page.
	if err := w.ogg.WriteEOS(w.granule); err != nil {
		return fmt.Errorf("opus: write EOS: %w", err)
	}

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

// bytesToInt16 converts a byte slice to an int16 slice (little-endian).
func bytesToInt16(data []byte) []int16 {
	result := make([]int16, len(data)/2)
	for i := 0; i < len(result); i++ {
		lo := int16(data[i*2])
		hi := int16(int8(data[i*2+1])) // Sign-extend MSB
		result[i] = (hi << 8) | (lo & 0xFF)
	}
	return result
}

// resampleAndConvert resamples PCM from source rate to 48kHz using linear interpolation
// and returns it as little-endian int16 bytes.
func (w *Writer) resampleAndConvert(sf source.Frame) []byte {
	// Input: PCM at source rate, convert to float32 interleaved.
	inputSamples := w.bytesToFloat32(sf.Data, w.fmt.Channels)

	// Resample to 48kHz.
	outputSamples := w.resample(inputSamples, w.fmt.Channels)

	// Convert float32 back to int16 bytes (little-endian).
	return w.float32ToBytes(outputSamples)
}

// bytesToFloat32 converts byte slice to float32 interleaved PCM.
func (w *Writer) bytesToFloat32(data []byte, _ int) []float32 {
	result := make([]float32, 0, len(data)/2)
	for i := 0; i < len(data); i += 2 {
		lo := int16(data[i])
		hi := int16(int8(data[i+1]))
		sample := (hi << 8) | (lo & 0xFF)
		result = append(result, float32(sample)/32768.0)
	}
	return result
}

// resample resamples float32 samples from source rate to 48kHz using
// linear interpolation. The fractional position w.resamplePos carries
// across calls so there are no phase discontinuities at frame boundaries.
func (w *Writer) resample(input []float32, channels int) []float32 {
	if !w.needsResample {
		return input
	}

	inputCount := len(input) / channels
	invRatio := 1.0 / w.resampleRatio // input-sample step per output sample
	output := make([]float32, 0, int(float64(inputCount)*w.resampleRatio+1)*channels)

	pos := w.resamplePos
	for {
		inIdx := int(pos)
		if inIdx >= inputCount-1 {
			break
		}
		frac := float32(pos - float64(inIdx))
		for ch := range channels {
			s0 := input[inIdx*channels+ch]
			s1 := input[(inIdx+1)*channels+ch]
			output = append(output, s0+frac*(s1-s0))
		}
		pos += invRatio
	}
	// Carry fractional position for the next call.
	w.resamplePos = pos - float64(inputCount)

	return output
}

// float32ToBytes converts float32 samples back to int16 bytes (little-endian).
func (w *Writer) float32ToBytes(samples []float32) []byte {
	result := make([]byte, len(samples)*2)
	for i, sample := range samples {
		// Clamp to int16 range.
		val := int16(sample * 32767.0)
		result[i*2] = byte(val & 0xFF)
		result[i*2+1] = byte((val >> 8) & 0xFF)
	}
	return result
}
