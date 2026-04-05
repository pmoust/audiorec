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
