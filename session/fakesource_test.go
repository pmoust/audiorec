package session

import (
	"context"
	"sync"
	"time"

	"github.com/pmoust/audiorec/source"
)

// fakeSource is a controllable Source used across session tests. It emits
// a scripted list of frames with an optional inter-frame delay, then closes
// its channel with a configurable termination error.
//
// Goroutine model: Start spawns a single producer goroutine that writes
// frames to an unbuffered channel. The producer watches ctx for cancellation
// and exits cleanly.
type fakeSource struct {
	format  source.Format
	frames  []source.Frame
	delay   time.Duration // between frames
	endErr  error         // returned from Err() after channel closes
	startErr error        // returned from Start() immediately (simulates failed open)

	ch     chan source.Frame
	once   sync.Once
	closed bool
	mu     sync.Mutex
	err    error
}

func newFakeSource(f source.Format, frames []source.Frame) *fakeSource {
	return &fakeSource{format: f, frames: frames, ch: make(chan source.Frame)}
}

func (s *fakeSource) Format() source.Format { return s.format }

func (s *fakeSource) Start(ctx context.Context) error {
	if s.startErr != nil {
		return s.startErr
	}
	go func() {
		defer close(s.ch)
		for _, f := range s.frames {
			if s.delay > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(s.delay):
				}
			}
			select {
			case <-ctx.Done():
				return
			case s.ch <- f:
			}
		}
		// Scripted stream exhausted — set endErr (may be nil).
		s.mu.Lock()
		s.err = s.endErr
		s.mu.Unlock()
	}()
	return nil
}

func (s *fakeSource) Frames() <-chan source.Frame { return s.ch }

func (s *fakeSource) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *fakeSource) Close() error {
	s.once.Do(func() { s.closed = true })
	return nil
}

// makeFrames produces n identical mono-16 frames of `bytesPerFrame` bytes each,
// filled with a byte value so tests can verify the bytes landed in the file.
func makeFrames(n, bytesPerFrame int, fill byte) []source.Frame {
	out := make([]source.Frame, n)
	for i := range out {
		data := make([]byte, bytesPerFrame)
		for j := range data {
			data[j] = fill
		}
		out[i] = source.Frame{Data: data, NumFrames: bytesPerFrame / 2, Timestamp: time.Now()}
	}
	return out
}
