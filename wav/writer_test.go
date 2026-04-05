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
