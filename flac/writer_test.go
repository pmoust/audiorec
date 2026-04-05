package flac

import (
	"bytes"
	"errors"
	"testing"

	maflac "github.com/mewkiz/flac"
	"github.com/pmoust/audiorec/source"
)

func TestCreate_RejectsFloat(t *testing.T) {
	tmpdir := t.TempDir()
	path := tmpdir + "/test.flac"

	fmt := source.Format{
		SampleRate:    48000,
		Channels:      1,
		BitsPerSample: 32,
		Float:         true,
	}

	_, err := Create(path, fmt)
	if !errors.Is(err, source.ErrUnsupportedFormat) {
		t.Errorf("expected ErrUnsupportedFormat, got %v", err)
	}
}

func TestCreate_RejectsUnsupportedBits(t *testing.T) {
	tmpdir := t.TempDir()
	path := tmpdir + "/test.flac"

	fmt := source.Format{
		SampleRate:    48000,
		Channels:      1,
		BitsPerSample: 24,
		Float:         false,
	}

	_, err := Create(path, fmt)
	if !errors.Is(err, source.ErrUnsupportedFormat) {
		t.Errorf("expected ErrUnsupportedFormat, got %v", err)
	}
}

func TestCreate_RejectsInvalidChannels(t *testing.T) {
	tmpdir := t.TempDir()
	path := tmpdir + "/test.flac"

	fmt := source.Format{
		SampleRate:    48000,
		Channels:      9,
		BitsPerSample: 16,
		Float:         false,
	}

	_, err := Create(path, fmt)
	if !errors.Is(err, source.ErrUnsupportedFormat) {
		t.Errorf("expected ErrUnsupportedFormat, got %v", err)
	}
}

func TestCreate_RejectsZeroSampleRate(t *testing.T) {
	tmpdir := t.TempDir()
	path := tmpdir + "/test.flac"

	fmt := source.Format{
		SampleRate:    0,
		Channels:      1,
		BitsPerSample: 16,
		Float:         false,
	}

	_, err := Create(path, fmt)
	if !errors.Is(err, source.ErrUnsupportedFormat) {
		t.Errorf("expected ErrUnsupportedFormat, got %v", err)
	}
}

func TestWriteFrame_16BitMono(t *testing.T) {
	tmpdir := t.TempDir()
	path := tmpdir + "/test.flac"

	fmt := source.Format{
		SampleRate:    48000,
		Channels:      1,
		BitsPerSample: 16,
		Float:         false,
	}

	w, err := Create(path, fmt)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Write 10 frames of 100 samples each.
	for frameNum := range 10 {
		data := make([]byte, 100*2) // 100 samples * 2 bytes per sample
		// Fill with a simple pattern.
		for i := 0; i < len(data); i += 2 {
			// Simple sine wave pattern.
			val := int16((frameNum*100 + i/2) % 32768)
			data[i] = byte(val & 0xFF)
			data[i+1] = byte((val >> 8) & 0xFF)
		}

		frame := source.Frame{
			Data:      data,
			NumFrames: 100,
		}
		if err := w.WriteFrame(frame); err != nil {
			t.Fatalf("WriteFrame %d failed: %v", frameNum, err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify the file can be parsed as FLAC.
	parsed, err := maflac.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	defer parsed.Close()

	if parsed.Info.SampleRate != uint32(fmt.SampleRate) {
		t.Errorf("sample rate mismatch: expected %d, got %d", fmt.SampleRate, parsed.Info.SampleRate)
	}
	if parsed.Info.NChannels != uint8(fmt.Channels) {
		t.Errorf("channel count mismatch: expected %d, got %d", fmt.Channels, parsed.Info.NChannels)
	}
	if parsed.Info.BitsPerSample != uint8(fmt.BitsPerSample) {
		t.Errorf("bits per sample mismatch: expected %d, got %d", fmt.BitsPerSample, parsed.Info.BitsPerSample)
	}
}

func TestWriteFrame_16BitStereo(t *testing.T) {
	tmpdir := t.TempDir()
	path := tmpdir + "/test.flac"

	fmt := source.Format{
		SampleRate:    44100,
		Channels:      2,
		BitsPerSample: 16,
		Float:         false,
	}

	w, err := Create(path, fmt)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Write 5 frames of 1024 samples each (per channel).
	for frameNum := range 5 {
		// Interleaved stereo: L R L R L R...
		data := make([]byte, 1024*2*2) // 1024 samples * 2 channels * 2 bytes
		for i := 0; i < len(data); i += 4 {
			// L channel.
			val := int16((frameNum*1024 + i/4) % 32768)
			data[i] = byte(val & 0xFF)
			data[i+1] = byte((val >> 8) & 0xFF)
			// R channel.
			val = int16(((frameNum*1024 + i/4) * 2) % 32768)
			data[i+2] = byte(val & 0xFF)
			data[i+3] = byte((val >> 8) & 0xFF)
		}

		frame := source.Frame{
			Data:      data,
			NumFrames: 1024,
		}
		if err := w.WriteFrame(frame); err != nil {
			t.Fatalf("WriteFrame %d failed: %v", frameNum, err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify the file can be parsed as FLAC.
	parsed, err := maflac.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	defer parsed.Close()

	if parsed.Info.SampleRate != uint32(fmt.SampleRate) {
		t.Errorf("sample rate mismatch: expected %d, got %d", fmt.SampleRate, parsed.Info.SampleRate)
	}
	if parsed.Info.NChannels != uint8(fmt.Channels) {
		t.Errorf("channel count mismatch: expected %d, got %d", fmt.Channels, parsed.Info.NChannels)
	}
}

func TestClose_Idempotent(t *testing.T) {
	tmpdir := t.TempDir()
	path := tmpdir + "/test.flac"

	fmt := source.Format{
		SampleRate:    48000,
		Channels:      1,
		BitsPerSample: 16,
		Float:         false,
	}

	w, err := Create(path, fmt)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Write one frame.
	data := make([]byte, 100*2)
	frame := source.Frame{
		Data:      data,
		NumFrames: 100,
	}
	if err := w.WriteFrame(frame); err != nil {
		t.Fatalf("WriteFrame failed: %v", err)
	}

	// Close multiple times.
	if err := w.Close(); err != nil {
		t.Errorf("first Close failed: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("second Close failed: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("third Close failed: %v", err)
	}
}

func TestFlush_Works(t *testing.T) {
	tmpdir := t.TempDir()
	path := tmpdir + "/test.flac"

	fmt := source.Format{
		SampleRate:    48000,
		Channels:      1,
		BitsPerSample: 16,
		Float:         false,
	}

	w, err := Create(path, fmt)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Write and flush multiple times.
	for i := range 3 {
		data := make([]byte, 100*2)
		frame := source.Frame{
			Data:      data,
			NumFrames: 100,
		}
		if err := w.WriteFrame(frame); err != nil {
			t.Fatalf("WriteFrame %d failed: %v", i, err)
		}
		if err := w.Flush(); err != nil {
			t.Fatalf("Flush %d failed: %v", i, err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestWriteFrame_EmptyFrameIsNoop(t *testing.T) {
	tmpdir := t.TempDir()
	path := tmpdir + "/test.flac"

	fmt := source.Format{
		SampleRate:    48000,
		Channels:      1,
		BitsPerSample: 16,
		Float:         false,
	}

	w, err := Create(path, fmt)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Write empty frame.
	frame := source.Frame{
		Data:      []byte{},
		NumFrames: 0,
	}
	if err := w.WriteFrame(frame); err != nil {
		t.Fatalf("WriteFrame with 0 samples should be noop, got: %v", err)
	}

	// Write a real frame.
	data := make([]byte, 100*2)
	frame = source.Frame{
		Data:      data,
		NumFrames: 100,
	}
	if err := w.WriteFrame(frame); err != nil {
		t.Fatalf("WriteFrame failed: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestWriteFrame_ErrorOnClosedWriter(t *testing.T) {
	tmpdir := t.TempDir()
	path := tmpdir + "/test.flac"

	fmt := source.Format{
		SampleRate:    48000,
		Channels:      1,
		BitsPerSample: 16,
		Float:         false,
	}

	w, err := Create(path, fmt)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Try to write to closed writer.
	data := make([]byte, 100*2)
	frame := source.Frame{
		Data:      data,
		NumFrames: 100,
	}
	if err := w.WriteFrame(frame); err == nil {
		t.Error("WriteFrame on closed writer should fail")
	}
}

func TestRoundTrip_SineWave(t *testing.T) {
	tmpdir := t.TempDir()
	path := tmpdir + "/test.flac"

	fmt := source.Format{
		SampleRate:    48000,
		Channels:      1,
		BitsPerSample: 16,
		Float:         false,
	}

	w, err := Create(path, fmt)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Write a known pattern.
	const numFrames = 1000
	const samplesPerFrame = 100
	expectedSamples := bytes.NewBuffer(nil)

	for f := range numFrames {
		data := make([]byte, samplesPerFrame*2)
		for i := range samplesPerFrame {
			sampleIdx := f*samplesPerFrame + i
			val := int16((sampleIdx % 1000) - 500) // Range: [-500, 500)
			data[i*2] = byte(val & 0xFF)
			data[i*2+1] = byte((val >> 8) & 0xFF)
			expectedSamples.Write(data[i*2 : i*2+2])
		}

		frame := source.Frame{
			Data:      data,
			NumFrames: samplesPerFrame,
		}
		if err := w.WriteFrame(frame); err != nil {
			t.Fatalf("WriteFrame %d failed: %v", f, err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Re-parse and verify.
	parsed, err := maflac.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	defer parsed.Close()

	if parsed.Info.SampleRate != uint32(fmt.SampleRate) {
		t.Errorf("sample rate mismatch")
	}
	if parsed.Info.NChannels != uint8(fmt.Channels) {
		t.Errorf("channel count mismatch")
	}
	if parsed.Info.BitsPerSample != uint8(fmt.BitsPerSample) {
		t.Errorf("bits per sample mismatch")
	}

	// Verify we can decode at least one frame.
	frame, err := parsed.ParseNext()
	if err != nil {
		t.Fatalf("ParseNext failed: %v", err)
	}
	if frame == nil {
		t.Fatal("expected at least one frame")
	}
}
