// Package flac implements a streaming FLAC writer that satisfies the
// session.Writer interface.
//
// Unlike the wav package, the FLAC encoder does not support 32-bit float
// PCM. If the source.Format passed to Create has Float=true, Create
// returns source.ErrUnsupportedFormat. Use the wav package for float
// PCM tracks.
package flac

import (
	"fmt"
	"os"
	"sync"

	"github.com/pmoust/audiorec/source"
)

// Writer streams FLAC frames to a file. It is not safe for concurrent
// use from multiple goroutines except where documented; the session
// package serializes all calls on a single writer goroutine.
type Writer struct {
	mu     sync.Mutex
	f      *os.File
	fmt    source.Format
	closed bool
	// encoder fields added in commit 2
}

// Create opens path for writing and initializes a FLAC encoder with the
// stream metadata derived from f. Returns source.ErrUnsupportedFormat if
// f.Float is true (FLAC does not encode float PCM).
func Create(path string, f source.Format) (*Writer, error) {
	return nil, fmt.Errorf("flac: not yet implemented")
}

// WriteFrame appends a frame to the FLAC stream.
func (w *Writer) WriteFrame(f source.Frame) error {
	return fmt.Errorf("flac: not implemented")
}

// Flush writes any buffered data and fsyncs the file.
func (w *Writer) Flush() error {
	return fmt.Errorf("flac: not implemented")
}

// Close finalizes the FLAC stream. Idempotent.
func (w *Writer) Close() error {
	return fmt.Errorf("flac: not implemented")
}
