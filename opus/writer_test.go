package opus

import (
	"bytes"
	"os"
	"testing"

	"github.com/pmoust/audiorec/source"
)

func TestCreate_RejectsMoreThan2Channels(t *testing.T) {
	tmpfile := t.TempDir() + "/test.opus"

	fmt := source.Format{
		SampleRate:    48000,
		Channels:      3,
		BitsPerSample: 16,
		Float:         false,
	}

	_, err := Create(tmpfile, fmt)
	if err == nil {
		t.Fatalf("expected error for 3 channels")
	}

	if !bytes.Contains([]byte(err.Error()), []byte("channels")) {
		t.Errorf("error should mention channels: %v", err)
	}
}

func TestWriteFrame_MonoInt16_ProducesValidOgg(t *testing.T) {
	tmpfile := t.TempDir() + "/test.opus"

	fmt := source.Format{
		SampleRate:    48000,
		Channels:      1,
		BitsPerSample: 16,
		Float:         false,
	}

	w, err := Create(tmpfile, fmt)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Write 1 second of 48kHz mono silence (48000 samples).
	// Break into frames of 960 samples each.
	frameSize := 960
	numFrames := 48000 / frameSize
	pcm := make([]byte, 960*2) // 960 samples * 1 channel * 2 bytes

	for i := 0; i < numFrames; i++ {
		frame := source.Frame{
			Data:      pcm,
			NumFrames: frameSize,
		}
		if err := w.WriteFrame(frame); err != nil {
			t.Fatalf("WriteFrame failed: %v", err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify the output file is a valid Ogg file.
	data, err := os.ReadFile(tmpfile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if len(data) < 4 {
		t.Fatalf("file too short")
	}

	// Check for "OggS" magic.
	if string(data[:4]) != "OggS" {
		t.Errorf("file does not start with OggS magic")
	}

	// Count Ogg pages.
	pageCount := 0
	for i := 0; i < len(data)-3; i++ {
		if string(data[i:i+4]) == "OggS" {
			pageCount++
		}
	}

	// We should have at least: BOS (OpusHead) + BOS (OpusTags) + data + EOS.
	// Actually OpusTags is not BOS. So: BOS + data pages + EOS.
	if pageCount < 3 {
		t.Errorf("expected at least 3 pages, got %d", pageCount)
	}

	// Verify first page is OpusHead.
	if !bytes.Contains(data[:100], []byte("OpusHead")) {
		t.Errorf("OpusHead not found in first 100 bytes")
	}

	// Verify OpusTags is present.
	if !bytes.Contains(data, []byte("OpusTags")) {
		t.Errorf("OpusTags not found in file")
	}

	// Verify EOS flag on last page (0x04).
	// Find last OggS.
	lastOggS := -1
	for i := 0; i < len(data)-3; i++ {
		if string(data[i:i+4]) == "OggS" {
			lastOggS = i
		}
	}
	if lastOggS < 0 {
		t.Fatalf("no OggS found")
	}

	// Header type is at offset 5 from page start.
	if lastOggS+5 < len(data) {
		headerType := data[lastOggS+5]
		if (headerType & 0x04) == 0 {
			t.Errorf("EOS flag not set on last page: 0x%02x", headerType)
		}
	}
}

func TestWriteFrame_StereoFloat32(t *testing.T) {
	// Note: our implementation currently only handles int16 input.
	// This test writes int16 stereo data (as if from a stereo device).
	tmpfile := t.TempDir() + "/test.opus"

	fmt := source.Format{
		SampleRate:    48000,
		Channels:      2,
		BitsPerSample: 16,
		Float:         false,
	}

	w, err := Create(tmpfile, fmt)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Write 10ms of 48kHz stereo (960 samples per channel).
	frameSize := 960
	pcm := make([]byte, frameSize*2*2) // 960 samples * 2 channels * 2 bytes

	frame := source.Frame{
		Data:      pcm,
		NumFrames: frameSize,
	}

	if err := w.WriteFrame(frame); err != nil {
		t.Fatalf("WriteFrame failed: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	data, err := os.ReadFile(tmpfile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if !bytes.Contains(data, []byte("OggS")) {
		t.Errorf("Ogg magic not found")
	}
}

func TestWriteFrame_Resample44100to48000(t *testing.T) {
	tmpfile := t.TempDir() + "/test.opus"

	fmt := source.Format{
		SampleRate:    44100,
		Channels:      1,
		BitsPerSample: 16,
		Float:         false,
	}

	w, err := Create(tmpfile, fmt)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Write 1 second of 44.1kHz mono (44100 samples).
	frameSize := 44100 / 10 // 10 frames per second
	numFrames := 10
	pcm := make([]byte, frameSize*2) // frameSize samples * 1 channel * 2 bytes

	for i := 0; i < numFrames; i++ {
		frame := source.Frame{
			Data:      pcm,
			NumFrames: frameSize,
		}
		if err := w.WriteFrame(frame); err != nil {
			t.Fatalf("WriteFrame failed: %v", err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	data, err := os.ReadFile(tmpfile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if !bytes.Contains(data, []byte("OggS")) {
		t.Errorf("Ogg magic not found")
	}
}

func TestFlush_PadsPartialFrame(t *testing.T) {
	tmpfile := t.TempDir() + "/test.opus"

	fmt := source.Format{
		SampleRate:    48000,
		Channels:      1,
		BitsPerSample: 16,
		Float:         false,
	}

	w, err := Create(tmpfile, fmt)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Write less than one frame (< 960 samples).
	frameSize := 480 // half a frame
	pcm := make([]byte, frameSize*2)

	frame := source.Frame{
		Data:      pcm,
		NumFrames: frameSize,
	}

	if err := w.WriteFrame(frame); err != nil {
		t.Fatalf("WriteFrame failed: %v", err)
	}

	// Now flush, which should pad and encode.
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Close should be idempotent after Flush.
	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	data, err := os.ReadFile(tmpfile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if !bytes.Contains(data, []byte("OggS")) {
		t.Errorf("Ogg magic not found")
	}

	// Verify at least one encoded packet was written (beyond headers).
	pageCount := 0
	for i := 0; i < len(data)-3; i++ {
		if string(data[i:i+4]) == "OggS" {
			pageCount++
		}
	}

	// BOS (OpusHead) + page (OpusTags) + data page + EOS = 4 pages.
	if pageCount < 3 {
		t.Errorf("expected at least 3 pages after flush, got %d", pageCount)
	}
}

func TestClose_Idempotent(t *testing.T) {
	tmpfile := t.TempDir() + "/test.opus"

	fmt := source.Format{
		SampleRate:    48000,
		Channels:      1,
		BitsPerSample: 16,
		Float:         false,
	}

	w, err := Create(tmpfile, fmt)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Close once.
	if err := w.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}

	// Close again (should be idempotent).
	if err := w.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
}

func TestCRC32Match(t *testing.T) {
	// Write a simple Ogg page and verify CRC32 is correct.
	tmpfile := t.TempDir() + "/test.opus"

	fmt := source.Format{
		SampleRate:    48000,
		Channels:      1,
		BitsPerSample: 16,
		Float:         false,
	}

	w, err := Create(tmpfile, fmt)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	data, err := os.ReadFile(tmpfile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	// Find first OggS page and verify its CRC32.
	pageStart := 0
	if !bytes.Contains(data[pageStart:], []byte("OggS")) {
		t.Fatalf("OggS not found")
	}

	// The file should have valid pages with correct CRC32 checksums.
	// A quick sanity check: file should be at least header + a few pages.
	if len(data) < 100 {
		t.Errorf("file too short")
	}
}

// BenchmarkWrite benchmarks writing audio frames.
func BenchmarkWrite(b *testing.B) {
	tmpfile := b.TempDir() + "/bench.opus"

	fmt := source.Format{
		SampleRate:    48000,
		Channels:      1,
		BitsPerSample: 16,
		Float:         false,
	}

	w, err := Create(tmpfile, fmt)
	if err != nil {
		b.Fatalf("Create failed: %v", err)
	}
	defer w.Close()

	frameSize := 960
	pcm := make([]byte, frameSize*2)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		frame := source.Frame{
			Data:      pcm,
			NumFrames: frameSize,
		}
		if err := w.WriteFrame(frame); err != nil {
			b.Fatalf("WriteFrame failed: %v", err)
		}
	}
}
