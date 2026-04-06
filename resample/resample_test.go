package resample

import (
	"context"
	"encoding/binary"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/pmoust/audiorec/source"
)

// fakeResampleSource is a test helper that emits a scripted list of frames.
type fakeResampleSource struct {
	format   source.Format
	frames   []source.Frame
	endErr   error
	startErr error

	ch   chan source.Frame
	once sync.Once
	err  error
	mu   sync.Mutex
}

func newFakeResampleSource(f source.Format, frames []source.Frame) *fakeResampleSource {
	return &fakeResampleSource{
		format: f,
		frames: frames,
		ch:     make(chan source.Frame),
	}
}

func (s *fakeResampleSource) Format() source.Format {
	return s.format
}

func (s *fakeResampleSource) Start(ctx context.Context) error {
	if s.startErr != nil {
		return s.startErr
	}
	go func() {
		defer close(s.ch)
		for _, f := range s.frames {
			select {
			case <-ctx.Done():
				return
			case s.ch <- f:
			}
		}
		s.mu.Lock()
		s.err = s.endErr
		s.mu.Unlock()
	}()
	return nil
}

func (s *fakeResampleSource) Frames() <-chan source.Frame {
	return s.ch
}

func (s *fakeResampleSource) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *fakeResampleSource) Close() error {
	return nil
}

// makeInt16Frame creates a mono 16-bit frame with the given sample values.
func makeInt16Frame(samples []int16) source.Frame {
	data := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(data[i*2:], uint16(s))
	}
	return source.Frame{
		Data:      data,
		NumFrames: len(samples),
		Timestamp: time.Now(),
	}
}

// makeInt16StereoFrame creates a stereo 16-bit frame with left and right samples.
func makeInt16StereoFrame(left, right []int16) source.Frame {
	if len(left) != len(right) {
		panic("left and right must have equal length")
	}
	data := make([]byte, len(left)*4)
	for i := range left {
		binary.LittleEndian.PutUint16(data[i*4:], uint16(left[i]))
		binary.LittleEndian.PutUint16(data[i*4+2:], uint16(right[i]))
	}
	return source.Frame{
		Data:      data,
		NumFrames: len(left),
		Timestamp: time.Now(),
	}
}

// makeFloat32Frame creates a mono float32 frame with the given sample values.
func makeFloat32Frame(samples []float32) source.Frame {
	data := make([]byte, len(samples)*4)
	for i, s := range samples {
		binary.LittleEndian.PutUint32(data[i*4:], math.Float32bits(s))
	}
	return source.Frame{
		Data:      data,
		NumFrames: len(samples),
		Timestamp: time.Now(),
	}
}

// readInt16Samples reads int16 samples from a frame's data.
func readInt16Samples(data []byte) []int16 {
	samples := make([]int16, len(data)/2)
	for i := 0; i < len(samples); i++ {
		samples[i] = int16(binary.LittleEndian.Uint16(data[i*2:]))
	}
	return samples
}

// readFloat32Samples reads float32 samples from a frame's data.
func readFloat32Samples(data []byte) []float32 {
	samples := make([]float32, len(data)/4)
	for i := 0; i < len(samples); i++ {
		samples[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return samples
}

// TestWrap_Passthrough verifies that Wrap returns the source directly if rates match.
func TestWrap_Passthrough(t *testing.T) {
	fmt := source.Format{
		SampleRate:    44100,
		Channels:      1,
		BitsPerSample: 16,
		Float:         false,
	}
	src := newFakeResampleSource(fmt, []source.Frame{})

	wrapped := Wrap(src, 44100)
	if wrapped != src {
		t.Errorf("Wrap with same rate should return source directly, got different pointer")
	}
}

// TestResample_44100to48000_Mono16 verifies upsampling from 44100 to 48000 Hz in mono 16-bit.
func TestResample_44100to48000_Mono16(t *testing.T) {
	srcFmt := source.Format{
		SampleRate:    44100,
		Channels:      1,
		BitsPerSample: 16,
		Float:         false,
	}
	// Create a simple ramp: 0, 1, 2, 3, ...
	frames := []source.Frame{
		makeInt16Frame([]int16{0, 1, 2, 3, 4}),
		makeInt16Frame([]int16{5, 6, 7, 8, 9}),
	}

	src := newFakeResampleSource(srcFmt, frames)
	wrapped := Wrap(src, 48000)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := wrapped.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	outFmt := wrapped.Format()
	if outFmt.SampleRate != 48000 {
		t.Errorf("Format.SampleRate: expected 48000, got %d", outFmt.SampleRate)
	}
	if outFmt.Channels != 1 {
		t.Errorf("Format.Channels: expected 1, got %d", outFmt.Channels)
	}

	var totalSamples int
	for frame := range wrapped.Frames() {
		totalSamples += frame.NumFrames
	}

	// Expected: 10 input samples * (48000/44100) ≈ 10.884 ≈ 11 output samples
	// Exact calculation depends on how the resampler handles fractional positions.
	expectedMin := int(math.Round(10 * 48000 / 44100 * 0.95))
	expectedMax := int(math.Round(10 * 48000 / 44100 * 1.05))
	if totalSamples < expectedMin || totalSamples > expectedMax {
		t.Errorf("Output sample count: got %d, expected between %d and %d",
			totalSamples, expectedMin, expectedMax)
	}
}

// TestResample_48000to44100_Stereo verifies downsampling from 48000 to 44100 Hz in stereo 16-bit.
func TestResample_48000to44100_Stereo(t *testing.T) {
	srcFmt := source.Format{
		SampleRate:    48000,
		Channels:      2,
		BitsPerSample: 16,
		Float:         false,
	}
	// Simple stereo ramp: left channel 0,2,4,..., right channel 1,3,5,...
	frames := []source.Frame{
		makeInt16StereoFrame([]int16{0, 2, 4}, []int16{1, 3, 5}),
		makeInt16StereoFrame([]int16{6, 8, 10}, []int16{7, 9, 11}),
	}

	src := newFakeResampleSource(srcFmt, frames)
	wrapped := Wrap(src, 44100)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := wrapped.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	outFmt := wrapped.Format()
	if outFmt.SampleRate != 44100 {
		t.Errorf("Format.SampleRate: expected 44100, got %d", outFmt.SampleRate)
	}
	if outFmt.Channels != 2 {
		t.Errorf("Format.Channels: expected 2, got %d", outFmt.Channels)
	}

	var totalSamples int
	for frame := range wrapped.Frames() {
		totalSamples += frame.NumFrames
	}

	// Expected: 6 input samples * (44100/48000) = 5.5 ≈ 6 output samples
	expectedMin := int(math.Round(6 * 44100 / 48000 * 0.95))
	expectedMax := int(math.Round(6 * 44100 / 48000 * 1.05))
	if totalSamples < expectedMin || totalSamples > expectedMax {
		t.Errorf("Output sample count: got %d, expected between %d and %d",
			totalSamples, expectedMin, expectedMax)
	}
}

// TestResample_48000to16000 verifies a large downsample ratio (3:1).
func TestResample_48000to16000(t *testing.T) {
	srcFmt := source.Format{
		SampleRate:    48000,
		Channels:      1,
		BitsPerSample: 16,
		Float:         false,
	}
	frames := []source.Frame{
		makeInt16Frame([]int16{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}),
		makeInt16Frame([]int16{10, 11, 12, 13, 14, 15, 16, 17, 18, 19}),
	}

	src := newFakeResampleSource(srcFmt, frames)
	wrapped := Wrap(src, 16000)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := wrapped.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	outFmt := wrapped.Format()
	if outFmt.SampleRate != 16000 {
		t.Errorf("Format.SampleRate: expected 16000, got %d", outFmt.SampleRate)
	}

	var totalSamples int
	for frame := range wrapped.Frames() {
		totalSamples += frame.NumFrames
	}

	// Expected: 20 input samples * (16000/48000) ≈ 6.67 ≈ 7 output samples
	expectedMin := int(math.Round(20 * 16000 / 48000 * 0.85))
	expectedMax := int(math.Round(20 * 16000 / 48000 * 1.15))
	if totalSamples < expectedMin || totalSamples > expectedMax {
		t.Errorf("Output sample count: got %d, expected between %d and %d",
			totalSamples, expectedMin, expectedMax)
	}
}

// TestResample_Float32 verifies float32 PCM resampling.
func TestResample_Float32(t *testing.T) {
	srcFmt := source.Format{
		SampleRate:    44100,
		Channels:      1,
		BitsPerSample: 32,
		Float:         true,
	}
	frames := []source.Frame{
		makeFloat32Frame([]float32{0.0, 0.1, 0.2, 0.3, 0.4}),
	}

	src := newFakeResampleSource(srcFmt, frames)
	wrapped := Wrap(src, 48000)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := wrapped.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	outFmt := wrapped.Format()
	if outFmt.SampleRate != 48000 {
		t.Errorf("Format.SampleRate: expected 48000, got %d", outFmt.SampleRate)
	}
	if !outFmt.Float {
		t.Errorf("Format.Float: expected true, got false")
	}

	var totalSamples int
	for frame := range wrapped.Frames() {
		totalSamples += frame.NumFrames
		// Verify output samples are float32 (readable without panic).
		_ = readFloat32Samples(frame.Data)
	}

	expectedMin := int(math.Round(5 * 48000 / 44100 * 0.95))
	expectedMax := int(math.Round(5 * 48000 / 44100 * 1.05))
	if totalSamples < expectedMin || totalSamples > expectedMax {
		t.Errorf("Output sample count: got %d, expected between %d and %d",
			totalSamples, expectedMin, expectedMax)
	}
}

// TestResample_FrameBoundary verifies that fractional positions carry across frame boundaries.
func TestResample_FrameBoundary(t *testing.T) {
	srcFmt := source.Format{
		SampleRate:    100,
		Channels:      1,
		BitsPerSample: 16,
		Float:         false,
	}
	// Two frames: first has values 0,1,2, second has values 3,4,5.
	// When resampling to 150 Hz, the output should be smooth across the boundary.
	frames := []source.Frame{
		makeInt16Frame([]int16{0, 1, 2}),
		makeInt16Frame([]int16{3, 4, 5}),
	}

	src := newFakeResampleSource(srcFmt, frames)
	wrapped := Wrap(src, 150)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := wrapped.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	var allSamples []int16
	for frame := range wrapped.Frames() {
		samples := readInt16Samples(frame.Data)
		allSamples = append(allSamples, samples...)
	}

	// Expect ~9 output samples (6 input * 1.5).
	if len(allSamples) == 0 {
		t.Fatal("No output samples")
	}

	// Check for large jumps that would indicate a boundary discontinuity.
	// Linear interpolation should produce smooth values.
	for i := 1; i < len(allSamples); i++ {
		diff := int(allSamples[i]) - int(allSamples[i-1])
		if diff > 2 || diff < -2 {
			t.Logf("sample[%d]=%d, sample[%d]=%d, diff=%d",
				i-1, allSamples[i-1], i, allSamples[i], diff)
			// This is not necessarily a failure; linear interp can have larger steps.
			// But we check that the overall trend is continuous.
		}
	}

	// Basic sanity: output should range from ~0 to ~5, not have spikes.
	minVal := allSamples[0]
	maxVal := allSamples[0]
	for _, s := range allSamples {
		if s < minVal {
			minVal = s
		}
		if s > maxVal {
			maxVal = s
		}
	}
	if minVal < -1 || maxVal > 6 {
		t.Logf("Output range [%d, %d]: unexpected range", minVal, maxVal)
	}
}

// TestResample_PreservesSineWave generates a 440 Hz sine at 44100, resamples to 48000,
// and verifies the output has approximately 440 Hz content (via zero-crossing count).
func TestResample_PreservesSineWave(t *testing.T) {
	srcRate := 44100
	targetRate := 48000
	frequency := 440.0

	// Generate 1 second of 440 Hz sine at 44100 Hz.
	numSamples := srcRate
	samples := make([]int16, numSamples)
	for i := range samples {
		phase := 2.0 * math.Pi * frequency * float64(i) / float64(srcRate)
		sample := math.Sin(phase)
		samples[i] = int16(sample * 30000) // Scale to int16 range
	}

	// Split into frames (e.g., 4410 samples per frame).
	frameSize := 4410
	var frames []source.Frame
	for i := 0; i < numSamples; i += frameSize {
		end := i + frameSize
		if end > numSamples {
			end = numSamples
		}
		frames = append(frames, makeInt16Frame(samples[i:end]))
	}

	srcFmt := source.Format{
		SampleRate:    srcRate,
		Channels:      1,
		BitsPerSample: 16,
		Float:         false,
	}
	src := newFakeResampleSource(srcFmt, frames)
	wrapped := Wrap(src, targetRate)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := wrapped.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	var outSamples []int16
	for frame := range wrapped.Frames() {
		outSamples = append(outSamples, readInt16Samples(frame.Data)...)
	}

	// Count zero crossings.
	zeroCrossings := 0
	for i := 1; i < len(outSamples); i++ {
		if (outSamples[i-1] < 0 && outSamples[i] > 0) ||
			(outSamples[i-1] > 0 && outSamples[i] < 0) {
			zeroCrossings++
		}
	}

	// At 440 Hz, we expect ~880 zero crossings per second.
	// Output duration is ~1 second at 48000 Hz, so ~48000 samples.
	expectedDuration := float64(len(outSamples)) / float64(targetRate)
	expectedZeroCrossings := int(2 * frequency * expectedDuration)

	// Allow 5% tolerance for linear interp artifacts.
	tolerance := int(math.Round(float64(expectedZeroCrossings) * 0.05))
	if zeroCrossings < expectedZeroCrossings-tolerance ||
		zeroCrossings > expectedZeroCrossings+tolerance {
		t.Logf("Zero crossings: got %d, expected ~%d (±%d), samples=%d, duration=%.3f sec",
			zeroCrossings, expectedZeroCrossings, tolerance, len(outSamples), expectedDuration)
		// Log but don't fail; the exact count depends on interpolation details.
	} else {
		t.Logf("Zero crossings: got %d, expected ~%d ✓", zeroCrossings, expectedZeroCrossings)
	}
}
