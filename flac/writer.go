// Package flac implements a streaming FLAC writer that satisfies the
// session.Writer interface.
//
// Unlike the wav package, the FLAC encoder does not support 32-bit float
// PCM. If the source.Format passed to Create has Float=true, Create
// returns source.ErrUnsupportedFormat. Use the wav package for float
// PCM tracks.
package flac

import (
	"errors"
	"fmt"
	"os"
	"sync"

	maflac "github.com/mewkiz/flac"
	"github.com/mewkiz/flac/frame"
	"github.com/mewkiz/flac/meta"
	"github.com/pmoust/audiorec/source"
)

// Writer streams FLAC frames to a file. It is not safe for concurrent
// use from multiple goroutines except where documented; the session
// package serializes all calls on a single writer goroutine.
type Writer struct {
	mu        sync.Mutex
	f         *os.File
	fmt       source.Format
	enc       *maflac.Encoder
	closed    bool
	blockSize int
}

// Create opens path for writing and initializes a FLAC encoder with the
// stream metadata derived from f. Returns source.ErrUnsupportedFormat if
// f.Float is true (FLAC does not encode float PCM).
func Create(path string, f source.Format) (*Writer, error) {
	// Reject float PCM.
	if f.Float {
		return nil, fmt.Errorf("%w: FLAC does not support float PCM", source.ErrUnsupportedFormat)
	}

	// Reject unsupported bit depths. FLAC supports 4, 8, 12, 16, 20, 24, 32.
	// We only accept 16 since that's what our backends deliver.
	if f.BitsPerSample != 16 {
		return nil, fmt.Errorf("%w: FLAC writer only supports 16-bit PCM, got %d", source.ErrUnsupportedFormat, f.BitsPerSample)
	}

	// Validate channels.
	if f.Channels <= 0 || f.Channels > 8 {
		return nil, fmt.Errorf("%w: channels must be in [1,8]", source.ErrUnsupportedFormat)
	}

	// Validate sample rate.
	if f.SampleRate <= 0 || f.SampleRate > 655350 {
		return nil, fmt.Errorf("%w: sample rate must be in [1, 655350]", source.ErrUnsupportedFormat)
	}

	// Open file.
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("flac.Create: %w", err)
	}

	// Build StreamInfo.
	blockSize := 4096
	info := &meta.StreamInfo{
		BlockSizeMin:  uint16(blockSize),
		BlockSizeMax:  uint16(blockSize),
		SampleRate:    uint32(f.SampleRate),
		NChannels:     uint8(f.Channels),
		BitsPerSample: uint8(f.BitsPerSample),
	}

	// Create encoder with a non-Closer wrapper to prevent the encoder
	// from closing our file handle. We manage the file lifecycle ourselves.
	nopCloser := &nopWriteSeeker{file}
	enc, err := maflac.NewEncoder(nopCloser, info)
	if err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("flac.Create: encoder: %w", err)
	}

	w := &Writer{
		f:         file,
		fmt:       f,
		enc:       enc,
		blockSize: blockSize,
	}

	// Sync to ensure FLAC header is on disk before returning.
	if err := file.Sync(); err != nil {
		_ = enc.Close()
		_ = file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("flac.Create: sync: %w", err)
	}

	return w, nil
}

// WriteFrame appends a frame to the FLAC stream.
func (w *Writer) WriteFrame(sf source.Frame) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errors.New("flac: WriteFrame on closed writer")
	}

	nSamples := sf.NumFrames
	if nSamples == 0 {
		return nil
	}

	// Deinterleave 16-bit little-endian PCM into [][]int32.
	subframes := make([]*frame.Subframe, w.fmt.Channels)
	for ch := range w.fmt.Channels {
		samples := make([]int32, nSamples)
		for i := range nSamples {
			// Read interleaved 16-bit little-endian sample for channel ch.
			offset := (i*w.fmt.Channels + ch) * 2
			lo := int32(sf.Data[offset])
			hi := int32(int8(sf.Data[offset+1])) // Sign-extend the MSB.
			samples[i] = (hi << 8) | (lo & 0xFF)
		}
		subframes[ch] = &frame.Subframe{
			SubHeader: frame.SubHeader{
				Pred: frame.PredVerbatim,
			},
			Samples:  samples,
			NSamples: nSamples,
		}
	}

	// Build frame header.
	channelAssign := channelAssignment(w.fmt.Channels)
	hdr := frame.Header{
		HasFixedBlockSize: false,
		BlockSize:         uint16(nSamples),
		SampleRate:        uint32(w.fmt.SampleRate),
		Channels:          channelAssign,
		BitsPerSample:     uint8(w.fmt.BitsPerSample),
	}

	// Build frame.
	fr := &frame.Frame{
		Header:    hdr,
		Subframes: subframes,
	}

	// Write to encoder.
	if err := w.enc.WriteFrame(fr); err != nil {
		return fmt.Errorf("flac: write frame: %w", err)
	}

	return nil
}

// Flush writes any buffered data and fsyncs the file.
// Note: The mewkiz encoder writes frames immediately, so this is primarily
// for fsync safety. The FLAC StreamInfo block is at the start and not updated.
func (w *Writer) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errors.New("flac: Flush on closed writer")
	}

	if err := w.f.Sync(); err != nil {
		return fmt.Errorf("flac: flush sync: %w", err)
	}
	return nil
}

// Close finalizes the FLAC stream. Idempotent.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true

	// Close encoder (flushes any buffered state).
	encErr := w.enc.Close()

	// Sync and close file.
	syncErr := w.f.Sync()
	fileErr := w.f.Close()

	return errors.Join(encErr, syncErr, fileErr)
}

// channelAssignment maps channel count to the appropriate FLAC channel assignment.
func channelAssignment(numChannels int) frame.Channels {
	switch numChannels {
	case 1:
		return frame.ChannelsMono
	case 2:
		return frame.ChannelsLR
	case 3:
		return frame.ChannelsLRC
	case 4:
		return frame.ChannelsLRLsRs
	case 5:
		return frame.ChannelsLRCLsRs
	case 6:
		return frame.ChannelsLRCLfeLsRs
	case 7:
		return frame.ChannelsLRCLfeCsSlSr
	case 8:
		return frame.ChannelsLRCLfeLsRsSlSr
	default:
		// Fallback: should not happen given Create validation.
		return frame.Channels(numChannels - 1)
	}
}

// nopWriteSeeker wraps an io.WriteSeeker but doesn't implement io.Closer,
// so the mewkiz encoder won't close our file handle. We manage that ourselves.
type nopWriteSeeker struct {
	ws interface {
		Write([]byte) (int, error)
		Seek(int64, int) (int64, error)
	}
}

func (n *nopWriteSeeker) Write(p []byte) (int, error) { return n.ws.Write(p) }
func (n *nopWriteSeeker) Seek(offset int64, whence int) (int64, error) {
	return n.ws.Seek(offset, whence)
}
