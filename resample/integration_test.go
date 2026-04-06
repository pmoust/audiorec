package resample

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pmoust/audiorec/session"
	"github.com/pmoust/audiorec/source"
)

// TestIntegration_ResampleWithWAVWriter verifies that resampling integrates
// cleanly with session + WAV writer: a 44100 Hz source resampled to 48000 Hz
// produces a WAV file with the correct sample rate in the header.
func TestIntegration_ResampleWithWAVWriter(t *testing.T) {
	// Create a fake source at 44100 Hz, mono, 16-bit.
	srcFmt := source.Format{
		SampleRate:    44100,
		Channels:      1,
		BitsPerSample: 16,
		Float:         false,
	}

	// Generate 1 second of audio (44100 samples).
	frames := []source.Frame{
		makeInt16Frame([]int16{100, 200, 300, 400, 500}),
		makeInt16Frame([]int16{600, 700, 800, 900, 1000}),
		makeInt16Frame([]int16{1100, 1200, 1300, 1400, 1500}),
	}

	src := newFakeResampleSource(srcFmt, frames)

	// Wrap to 48000 Hz.
	wrapped := Wrap(src, 48000)

	// Create a temp directory for the output WAV file.
	tmpDir, err := os.MkdirTemp("", "audiorec-resample-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	outputPath := filepath.Join(tmpDir, "test.wav")

	// Create a session with the wrapped source.
	sess, err := session.New(session.Config{
		Tracks: []session.Track{
			{
				Source: wrapped,
				Path:   outputPath,
				Label:  "test",
			},
		},
		Logger: nil, // use default logger
	})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Run the session with a timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := sess.Run(ctx); err != nil {
		// Context timeout is expected; the session will shut down cleanly.
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Session failed: %v", err)
		}
	}

	// Verify the output file exists.
	fileInfo, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("Output file not found: %v", err)
	}

	if fileInfo.Size() == 0 {
		t.Fatal("Output file is empty")
	}

	// Read the WAV header and verify the sample rate.
	file, err := os.Open(outputPath)
	if err != nil {
		t.Fatalf("Failed to open output file: %v", err)
	}
	defer file.Close()

	// Parse WAV header: check "RIFF" + size + "WAVE"
	header := make([]byte, 12)
	if _, err := file.Read(header); err != nil {
		t.Fatalf("Failed to read WAV header: %v", err)
	}

	if string(header[0:4]) != "RIFF" {
		t.Errorf("Expected 'RIFF' at start of WAV file, got %s", string(header[0:4]))
	}
	if string(header[8:12]) != "WAVE" {
		t.Errorf("Expected 'WAVE' at offset 8, got %s", string(header[8:12]))
	}

	// Find the fmt chunk and read the sample rate (at offset 24 in the file, or 12 from current position).
	// Format: "fmt " (4 bytes) + chunk size (4 bytes) + format data
	// Within format data: channels (2) + sample rate (4) + byte rate (4) + block align (2) + bits per sample (2)
	chunkHeader := make([]byte, 8)
	for {
		if _, err := file.Read(chunkHeader); err != nil {
			t.Fatalf("Failed to read chunk header: %v", err)
		}

		chunkID := string(chunkHeader[0:4])
		chunkSize := binary.LittleEndian.Uint32(chunkHeader[4:8])

		if chunkID == "fmt " {
			// Read the fmt chunk data.
			fmtData := make([]byte, chunkSize)
			if _, err := file.Read(fmtData); err != nil {
				t.Fatalf("Failed to read fmt chunk: %v", err)
			}

			// Sample rate is at offset 4-8 in fmt data.
			sampleRateFromFile := binary.LittleEndian.Uint32(fmtData[4:8])

			if int(sampleRateFromFile) != 48000 {
				t.Errorf("Expected sample rate 48000 in WAV header, got %d", sampleRateFromFile)
			}

			return
		}

		// Skip to the next chunk.
		if _, err := file.Seek(int64(chunkSize), 1); err != nil {
			t.Fatalf("Failed to seek to next chunk: %v", err)
		}
	}
}

// TestIntegration_ResamplePassthroughWithWAVWriter verifies that when sample
// rates match, resampling is a no-op and produces a valid WAV file.
func TestIntegration_ResamplePassthroughWithWAVWriter(t *testing.T) {
	// Create a fake source at 48000 Hz.
	srcFmt := source.Format{
		SampleRate:    48000,
		Channels:      1,
		BitsPerSample: 16,
		Float:         false,
	}

	frames := []source.Frame{
		makeInt16Frame([]int16{100, 200, 300, 400, 500}),
	}

	src := newFakeResampleSource(srcFmt, frames)

	// Wrap to same rate (should be a passthrough).
	wrapped := Wrap(src, 48000)

	// Verify pointer equality (passthrough optimization).
	if wrapped != src {
		t.Error("Expected Wrap to return the same source when rates match")
	}

	tmpDir, err := os.MkdirTemp("", "audiorec-resample-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	outputPath := filepath.Join(tmpDir, "test.wav")

	sess, err := session.New(session.Config{
		Tracks: []session.Track{
			{
				Source: wrapped,
				Path:   outputPath,
				Label:  "test",
			},
		},
	})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := sess.Run(ctx); err != nil {
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Session failed: %v", err)
		}
	}

	// Verify the output file exists and has content.
	fileInfo, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("Output file not found: %v", err)
	}

	if fileInfo.Size() == 0 {
		t.Fatal("Output file is empty")
	}

	// Quick header check.
	file, err := os.Open(outputPath)
	if err != nil {
		t.Fatalf("Failed to open output file: %v", err)
	}
	defer file.Close()

	header := make([]byte, 12)
	if _, err := file.Read(header); err != nil {
		t.Fatalf("Failed to read WAV header: %v", err)
	}

	if string(header[0:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		t.Error("Invalid WAV file header")
	}
}
