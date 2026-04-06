// Package wav implements a crash-safe streaming WAV writer.
//
// Rationale: standard WAV files put total-length fields in the header, which
// are normally finalized at Close. If a recording process dies mid-session,
// those fields stay at zero and most players reject the file even though the
// PCM data on disk is intact. This writer fixes that by rewriting the length
// fields periodically via Flush(), so a kill -9 at worst loses the tail
// samples produced since the last flush.
//
// The writer is not safe for concurrent use from multiple goroutines except
// where documented. The session package serializes all calls to a given
// Writer on a single writer goroutine.
package wav

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/pmoust/audiorec/source"
)

const (
	HeaderSize    = 76
	DataSizeOff   = 72
	riffSizeOff   = 4
	ds64BodyOff   = 20
	maxUint32Val  = 0xFFFFFFFF
	wavFormatPCM  = 1
	wavFormatIEEE = 3 // WAVE_FORMAT_IEEE_FLOAT
)

// Writer appends PCM frames to a WAV file and periodically rewrites the
// header so the file remains playable if the process crashes.
type Writer struct {
	mu           sync.Mutex
	f            *os.File
	fmt          source.Format
	bytesWritten int64 // PCM bytes appended since Create
	closed       bool
}

// Create opens path for writing and writes an initial valid (empty) WAV
// header. The file is fsync'd before return so an immediate crash still
// leaves a parseable zero-length WAV.
func Create(path string, f source.Format) (*Writer, error) {
	if err := validateFormat(f); err != nil {
		return nil, err
	}
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("wav.Create: %w", err)
	}
	w := &Writer{f: file, fmt: f}
	if err := w.writeHeader(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("wav.Create: sync: %w", err)
	}
	return w, nil
}

func validateFormat(f source.Format) error {
	if f.SampleRate <= 0 {
		return fmt.Errorf("%w: sample rate must be positive", source.ErrUnsupportedFormat)
	}
	if f.Channels <= 0 || f.Channels > 8 {
		return fmt.Errorf("%w: channels must be in [1,8]", source.ErrUnsupportedFormat)
	}
	if f.BitsPerSample != 16 && f.BitsPerSample != 32 {
		return fmt.Errorf("%w: bits per sample must be 16 or 32", source.ErrUnsupportedFormat)
	}
	if f.Float && f.BitsPerSample != 32 {
		return fmt.Errorf("%w: float PCM must be 32-bit", source.ErrUnsupportedFormat)
	}
	return nil
}

// writeHeader writes the 76-byte RF64-compatible PCM header at offset 0. The
// caller must hold w.mu. Includes a ds64 chunk and writes RF64 magic if the file
// exceeds 4GB. Length fields reflect w.bytesWritten.
func (w *Writer) writeHeader() error {
	var h [HeaderSize]byte

	// Determine if this is an RF64 file (>4GB).
	isRF64 := w.bytesWritten > maxUint32Val

	// RIFF/RF64 chunk ID and size
	if isRF64 {
		copy(h[0:4], "RF64")
		binary.LittleEndian.PutUint32(h[4:8], maxUint32Val)
	} else {
		copy(h[0:4], "RIFF")
		binary.LittleEndian.PutUint32(h[4:8], uint32(68+w.bytesWritten))
	}

	// WAVE format
	copy(h[8:12], "WAVE")

	// ds64 chunk for RF64 support
	copy(h[12:16], "ds64")
	binary.LittleEndian.PutUint32(h[16:20], 24) // ds64 chunk body size

	// ds64 chunk body: 64-bit sizes (always populated, even under 4GB)
	binary.LittleEndian.PutUint64(h[20:28], uint64(68+w.bytesWritten)) // riffSize64
	binary.LittleEndian.PutUint64(h[28:36], uint64(w.bytesWritten))    // dataSize64
	sampleCount := w.bytesWritten / int64(w.fmt.BytesPerFrame())
	binary.LittleEndian.PutUint64(h[36:44], uint64(sampleCount)) // sampleCount64

	// fmt  chunk
	copy(h[44:48], "fmt ")
	binary.LittleEndian.PutUint32(h[48:52], 16) // PCM fmt chunk size

	format := uint16(wavFormatPCM)
	if w.fmt.Float {
		format = wavFormatIEEE
	}
	binary.LittleEndian.PutUint16(h[52:54], format)
	binary.LittleEndian.PutUint16(h[54:56], uint16(w.fmt.Channels))
	binary.LittleEndian.PutUint32(h[56:60], uint32(w.fmt.SampleRate))

	byteRate := uint32(w.fmt.SampleRate * w.fmt.Channels * (w.fmt.BitsPerSample / 8))
	binary.LittleEndian.PutUint32(h[60:64], byteRate)

	blockAlign := uint16(w.fmt.Channels * (w.fmt.BitsPerSample / 8))
	binary.LittleEndian.PutUint16(h[64:66], blockAlign)
	binary.LittleEndian.PutUint16(h[66:68], uint16(w.fmt.BitsPerSample))

	// data chunk
	copy(h[68:72], "data")
	if isRF64 {
		binary.LittleEndian.PutUint32(h[72:76], maxUint32Val)
	} else {
		binary.LittleEndian.PutUint32(h[72:76], uint32(w.bytesWritten))
	}

	if _, err := w.f.WriteAt(h[:], 0); err != nil {
		return fmt.Errorf("wav: write header: %w", err)
	}
	return nil
}

// Close finalizes the file with a last header rewrite + fsync. Idempotent.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	// Best-effort final header. If it fails we still try to close the file.
	hdrErr := w.writeHeader()
	syncErr := w.f.Sync()
	closeErr := w.f.Close()
	return errors.Join(hdrErr, syncErr, closeErr)
}

// WriteFrame appends f.Data to the file and updates the in-memory byte
// counter. It does NOT rewrite the header — call Flush periodically for
// crash safety. A single Write syscall per frame.
//
// Returns an error if Data is not a multiple of the format's block align,
// which would corrupt the stream.
func (w *Writer) WriteFrame(f source.Frame) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errors.New("wav: WriteFrame on closed writer")
	}
	blockAlign := w.fmt.BytesPerFrame()
	if blockAlign > 0 && len(f.Data)%blockAlign != 0 {
		return fmt.Errorf("wav: frame data length %d not a multiple of block align %d",
			len(f.Data), blockAlign)
	}
	n, err := w.f.WriteAt(f.Data, HeaderSize+w.bytesWritten)
	if err != nil {
		return fmt.Errorf("wav: write frame: %w", err)
	}
	w.bytesWritten += int64(n)
	return nil
}

// Flush rewrites the WAV header length fields to reflect all PCM data
// written so far, then fsyncs the file. After a successful Flush, the file
// on disk is a valid playable WAV even if the process dies immediately
// afterward.
//
// Flush is the ONLY operation that touches the header — there is a single
// code path for header updates.
func (w *Writer) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errors.New("wav: Flush on closed writer")
	}
	if err := w.writeHeader(); err != nil {
		return err
	}
	if err := w.f.Sync(); err != nil {
		return fmt.Errorf("wav: flush sync: %w", err)
	}
	return nil
}
