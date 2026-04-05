package wav

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/pmoust/audiorec/source"
)

// readHeader returns the raw 44-byte canonical PCM WAV header from path.
func readHeader(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(b) < 44 {
		t.Fatalf("file too short: %d bytes", len(b))
	}
	return b[:44]
}

func TestCreate_WritesValidEmptyHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.wav")

	w, err := Create(path, source.Format{
		SampleRate:    48000,
		Channels:      2,
		BitsPerSample: 16,
		Float:         false,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	h := readHeader(t, path)

	// RIFF chunk
	if string(h[0:4]) != "RIFF" {
		t.Errorf("magic: got %q want RIFF", string(h[0:4]))
	}
	// RIFF size = 36 + dataSize = 36 for empty
	if got := binary.LittleEndian.Uint32(h[4:8]); got != 36 {
		t.Errorf("riff size: got %d want 36", got)
	}
	if string(h[8:12]) != "WAVE" {
		t.Errorf("format: got %q want WAVE", string(h[8:12]))
	}
	// fmt  subchunk
	if string(h[12:16]) != "fmt " {
		t.Errorf("fmt magic: got %q", string(h[12:16]))
	}
	if got := binary.LittleEndian.Uint32(h[16:20]); got != 16 {
		t.Errorf("fmt size: got %d want 16", got)
	}
	if got := binary.LittleEndian.Uint16(h[20:22]); got != 1 { // WAVE_FORMAT_PCM
		t.Errorf("audio format: got %d want 1", got)
	}
	if got := binary.LittleEndian.Uint16(h[22:24]); got != 2 {
		t.Errorf("channels: got %d want 2", got)
	}
	if got := binary.LittleEndian.Uint32(h[24:28]); got != 48000 {
		t.Errorf("sample rate: got %d want 48000", got)
	}
	// byte rate = sampleRate * channels * bitsPerSample/8
	if got := binary.LittleEndian.Uint32(h[28:32]); got != 48000*2*2 {
		t.Errorf("byte rate: got %d want %d", got, 48000*2*2)
	}
	// block align = channels * bitsPerSample/8
	if got := binary.LittleEndian.Uint16(h[32:34]); got != 4 {
		t.Errorf("block align: got %d want 4", got)
	}
	if got := binary.LittleEndian.Uint16(h[34:36]); got != 16 {
		t.Errorf("bits per sample: got %d want 16", got)
	}
	// data subchunk
	if string(h[36:40]) != "data" {
		t.Errorf("data magic: got %q", string(h[36:40]))
	}
	if got := binary.LittleEndian.Uint32(h[40:44]); got != 0 {
		t.Errorf("data size: got %d want 0", got)
	}
}

func TestWriteFrame_AppendsPCMAndCountsBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "append.wav")

	w, err := Create(path, source.Format{
		SampleRate:    48000,
		Channels:      1,
		BitsPerSample: 16,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// 10 mono 16-bit samples = 20 bytes
	pcm := make([]byte, 20)
	for i := range pcm {
		pcm[i] = byte(i)
	}
	if err := w.WriteFrame(source.Frame{Data: pcm, NumFrames: 10}); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if len(b) != headerSize+20 {
		t.Fatalf("file size: got %d want %d", len(b), headerSize+20)
	}
	// PCM bytes should match what we wrote.
	for i := 0; i < 20; i++ {
		if b[headerSize+i] != byte(i) {
			t.Errorf("pcm[%d]: got %d want %d", i, b[headerSize+i], i)
		}
	}
	// Header length fields should reflect 20 bytes of data.
	if got := binary.LittleEndian.Uint32(b[riffSizeOff : riffSizeOff+4]); got != 36+20 {
		t.Errorf("riff size: got %d want %d", got, 36+20)
	}
	if got := binary.LittleEndian.Uint32(b[dataSizeOff : dataSizeOff+4]); got != 20 {
		t.Errorf("data size: got %d want 20", got)
	}
}

func TestWriteFrame_RejectsMisalignedData(t *testing.T) {
	dir := t.TempDir()
	w, err := Create(filepath.Join(dir, "x.wav"), source.Format{
		SampleRate:    48000,
		Channels:      2,
		BitsPerSample: 16,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer w.Close()
	// 3 bytes is not a multiple of 4 (stereo 16-bit block align).
	err = w.WriteFrame(source.Frame{Data: []byte{1, 2, 3}, NumFrames: 0})
	if err == nil {
		t.Fatalf("expected error for misaligned data")
	}
}

func TestFlush_UpdatesHeaderLengths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flush.wav")

	w, err := Create(path, source.Format{
		SampleRate:    48000,
		Channels:      1,
		BitsPerSample: 16,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer w.Close()

	pcm := make([]byte, 100) // 50 mono 16-bit samples
	if err := w.WriteFrame(source.Frame{Data: pcm, NumFrames: 50}); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}

	// Read header BEFORE flush — length fields should still reflect zero
	// because Create wrote a zero-length header and WriteFrame doesn't
	// touch it.
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("pre-flush read: %v", err)
	}
	if got := binary.LittleEndian.Uint32(b[dataSizeOff : dataSizeOff+4]); got != 0 {
		t.Errorf("pre-flush data size: got %d want 0", got)
	}

	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	b, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("post-flush read: %v", err)
	}
	if got := binary.LittleEndian.Uint32(b[dataSizeOff : dataSizeOff+4]); got != 100 {
		t.Errorf("post-flush data size: got %d want 100", got)
	}
	if got := binary.LittleEndian.Uint32(b[riffSizeOff : riffSizeOff+4]); got != 36+100 {
		t.Errorf("post-flush riff size: got %d want %d", got, 36+100)
	}
}

// TestCrashRecovery simulates kill -9 by NOT calling Close. After Flush,
// the file on disk must be a valid playable WAV containing the pre-flush
// samples.
func TestCrashRecovery_FlushedDataIsPlayable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "crash.wav")

	w, err := Create(path, source.Format{
		SampleRate:    48000,
		Channels:      2,
		BitsPerSample: 16,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Write 200 bytes (50 stereo 16-bit samples), flush, write 40 more
	// bytes that will NOT be flushed.
	pcm1 := make([]byte, 200)
	for i := range pcm1 {
		pcm1[i] = 0xAB
	}
	if err := w.WriteFrame(source.Frame{Data: pcm1, NumFrames: 50}); err != nil {
		t.Fatalf("WriteFrame 1: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	pcm2 := make([]byte, 40)
	for i := range pcm2 {
		pcm2[i] = 0xCD
	}
	if err := w.WriteFrame(source.Frame{Data: pcm2, NumFrames: 10}); err != nil {
		t.Fatalf("WriteFrame 2: %v", err)
	}
	// DO NOT call Close — simulate crash.
	// Leak the *os.File handle intentionally. On most OSes the kernel will
	// keep the write durable since we fsync'd in Flush.

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	// Header must reflect 200 bytes of data (what was flushed).
	if got := binary.LittleEndian.Uint32(b[dataSizeOff : dataSizeOff+4]); got != 200 {
		t.Errorf("data size: got %d want 200", got)
	}
	// File on disk may contain the unflushed tail bytes too (240 total),
	// but players honor the header's data size of 200 and ignore the tail.
	if len(b) < headerSize+200 {
		t.Errorf("file too short: %d < %d", len(b), headerSize+200)
	}
	// Sanity: first 200 data bytes are 0xAB.
	for i := 0; i < 200; i++ {
		if b[headerSize+i] != 0xAB {
			t.Errorf("data[%d]: got %#x want 0xAB", i, b[headerSize+i])
			break
		}
	}
}

func TestClose_Idempotent(t *testing.T) {
	dir := t.TempDir()
	w, err := Create(filepath.Join(dir, "x.wav"), source.Format{
		SampleRate:    48000,
		Channels:      1,
		BitsPerSample: 16,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close 1: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("Close 2 (should be no-op): %v", err)
	}
}

func TestWrite_FloatPCM_SetsCorrectFormatTag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "float.wav")
	w, err := Create(path, source.Format{
		SampleRate:    48000,
		Channels:      2,
		BitsPerSample: 32,
		Float:         true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// 4 stereo float samples = 32 bytes
	if err := w.WriteFrame(source.Frame{Data: make([]byte, 32), NumFrames: 4}); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	h := readHeader(t, path)
	if got := binary.LittleEndian.Uint16(h[20:22]); got != wavFormatIEEE {
		t.Errorf("audio format: got %d want %d (IEEE_FLOAT)", got, wavFormatIEEE)
	}
	if got := binary.LittleEndian.Uint16(h[34:36]); got != 32 {
		t.Errorf("bits per sample: got %d want 32", got)
	}
}

func TestValidateFormat_Rejects(t *testing.T) {
	cases := []source.Format{
		{SampleRate: 0, Channels: 1, BitsPerSample: 16},
		{SampleRate: 48000, Channels: 0, BitsPerSample: 16},
		{SampleRate: 48000, Channels: 9, BitsPerSample: 16},
		{SampleRate: 48000, Channels: 1, BitsPerSample: 24},
		{SampleRate: 48000, Channels: 1, BitsPerSample: 16, Float: true}, // float must be 32-bit
	}
	dir := t.TempDir()
	for i, f := range cases {
		_, err := Create(filepath.Join(dir, "bad.wav"), f)
		if err == nil {
			t.Errorf("case %d: expected error for format %+v", i, f)
		}
	}
}
