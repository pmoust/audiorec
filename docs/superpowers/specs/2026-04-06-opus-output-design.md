# Opus Output — Design Spec

**Date:** 2026-04-06
**Status:** Approved

## Overview

Add OggOpus output to audiorec via `--format opus`. Uses `gopkg.in/hraban/opus.v2` (cgo libopus wrapper) for encoding and a minimal hand-written Ogg page muxer for the container. Unlike FLAC, Opus accepts both int16 and float32 PCM, so macOS system audio (32-bit float via ScreenCaptureKit) works out of the box.

## Architecture

### Package layout

```
opus/
├── writer.go           # Opus encoder + PCM accumulator implementing session.Writer
└── writer_test.go      # round-trip: encode → decode, format validation, flush semantics
ogg/
├── writer.go           # minimal single-stream Ogg page muxer
└── writer_test.go      # page structure, CRC32, EOS marker
```

### Dependencies

- `gopkg.in/hraban/opus.v2` — cgo wrapper for libopus. On macOS, the package bundles a static libopus build. On Linux CI/dev, requires `libopus-dev` (apt) or `opus` (brew). Add to CI workflow's Linux job as an apt install step.
- No Ogg library dependency — we write a ~150-line muxer for the single-stream OggOpus case. The full Ogg spec is complex (multiplexed streams, seeking), but a single-stream write-only muxer is trivial: 27-byte page header + segment table + payload + CRC32.

### Ogg page muxer (`ogg/writer.go`)

Writes a sequence of Ogg pages to an `io.Writer`. Each page:

```
capture_pattern  "OggS"   4 bytes
version          0         1 byte
header_type      flags     1 byte (0x02=BOS, 0x04=EOS)
granule_position int64     8 bytes (PCM sample count for audio)
serial_number    uint32    4 bytes (random, fixed per stream)
page_sequence    uint32    4 bytes (monotonically increasing)
checksum         uint32    4 bytes (CRC32 of entire page with checksum field zeroed)
segment_count    uint8     1 byte
segment_table    []uint8   segment_count bytes (each ≤ 255; packet boundary at segment < 255)
payload          []byte    sum of segment_table bytes
```

API:

```go
type PageWriter struct { w io.Writer; serial uint32; seq uint32 }
func NewPageWriter(w io.Writer) *PageWriter
func (p *PageWriter) WriteBOS(granule int64, data []byte) error  // first page
func (p *PageWriter) WritePage(granule int64, data []byte) error // middle pages
func (p *PageWriter) WriteEOS(granule int64) error               // last page (empty payload)
```

CRC32 uses the Ogg-specific polynomial (0x04C11DB7, not the standard CRC32 used by zlib/gzip). Implement via a 256-entry lookup table.

### OggOpus header pages

Per RFC 7845, an OggOpus stream starts with two header pages:

1. **OpusHead** (BOS page): 19 bytes — magic `"OpusHead"`, version 1, channel count, pre-skip (3840 samples at 48kHz is standard), input sample rate (informational), output gain 0, channel mapping family 0 (mono/stereo).
2. **OpusTags** (second page): `"OpusTags"` + vendor string + comment count 0. Minimal: `"OpusTags" + uint32(len("audiorec")) + "audiorec" + uint32(0)`.

### Opus encoder + accumulator (`opus/writer.go`)

```go
type Writer struct {
    mu        sync.Mutex
    f         *os.File
    ogg       *ogg.PageWriter
    enc       *opus.Encoder
    fmt       source.Format
    buf       []byte        // PCM accumulator (ring-style, append-only)
    frameSize int           // samples per channel per Opus frame (960 for 20ms@48kHz)
    granule   int64         // running PCM sample count at 48kHz
    closed    bool
    // resampler fields (only when source rate != 48000)
    needsResample bool
    resampleRatio float64
    resampleBuf   []float32 // intermediate float buffer for resampling
}

func Create(path string, f source.Format, opts ...Option) (*Writer, error)
```

**Options:** `WithBitrate(bps int)` — default 48000 (48kbps).

**`Create` flow:**
1. Validate: channels ≤ 2 (Opus channel mapping family 0 supports mono/stereo; 3+ channels need mapping family 1 which is complex — reject for v1).
2. Open file.
3. Create `ogg.PageWriter`.
4. Write OpusHead + OpusTags header pages.
5. Create `opus.Encoder` at 48kHz with the given channel count and `opus.AppVoIP` application mode (optimized for speech).
6. Set encoder bitrate.
7. If `f.SampleRate != 48000`, configure the internal linear resampler.

**`WriteFrame` flow:**
1. If `needsResample`: convert input PCM to float32, resample to 48kHz, convert back to the encoder's expected format.
2. Append PCM to `buf`.
3. While `buf` has ≥ `frameSize * channels * bytesPerSample` bytes: extract one frame, encode via `enc.Encode` (int16) or `enc.EncodeFloat32` (float32), write the resulting Opus packet to the Ogg page writer. Increment `granule` by `frameSize`.

**`Flush` flow:**
1. If `buf` has any remaining PCM: pad with silence to fill a complete Opus frame, encode, write. This ensures every sample makes it to disk, at the cost of up to 20ms of trailing silence.
2. `f.Sync()`.

**`Close` flow:**
1. `Flush()`.
2. Write Ogg EOS page.
3. `f.Sync()`, `f.Close()`.
4. Idempotent.

### Crash safety

Ogg is inherently crash-safe: each page has a CRC32 and can be validated independently. A partial file is playable up to the last complete page. No header rewrite is needed (unlike WAV). The `Flush` → `f.Sync()` path ensures durable pages hit disk on the configured interval.

### Internal resampling

When the source sample rate is not 48kHz (e.g., 44100 Hz mic), the writer resamples internally using linear interpolation. This is acceptable because:
1. Opus re-encodes the signal lossy anyway — interpolation artifacts are below the codec's noise floor.
2. The alternative (requiring the global `--sample-rate` flag) creates a dependency between features.

The resampler is private to the `opus` package, not the standalone `resample/` package (which is a separate v2.x feature for the WAV/FLAC path).

### CLI

- `--format opus` added alongside `wav` and `flac`. File extension: `.opus`.
- `--opus-bitrate` flag: accepts an integer (bits per second) or `Nk` shorthand (`48k` = 48000). Default: `48k`. Range: 6000–510000 (libopus limits). Only meaningful when `--format opus`; ignored otherwise.
- Validation: `--format opus` with `--system none` on macOS works (mic is int16, Opus handles it). `--format opus` with system audio also works (float32, Opus handles it). No format-rejection path unlike FLAC.

### Testing

- `ogg/writer_test.go`: verify page structure (magic, version, CRC32, segment table, BOS/EOS flags) by writing pages and re-reading byte-by-byte.
- `opus/writer_test.go`:
  - `TestCreate_RejectsMoreThan2Channels`: 3+ channels → `ErrUnsupportedFormat`.
  - `TestWriteFrame_MonoInt16_ProducesValidOgg`: encode 1s of 48kHz mono silence, re-parse with an Ogg reader, verify page count > 0.
  - `TestWriteFrame_StereoFloat32`: same but float32 stereo (simulates macOS system audio).
  - `TestWriteFrame_Resample44100to48000`: source at 44.1kHz, verify the writer doesn't error and produces valid Ogg.
  - `TestFlush_PadsPartialFrame`: write fewer than 960 samples, flush, verify one encoded frame was written.
  - `TestClose_Idempotent`.
  - Round-trip decode is NOT practical without a Go Opus decoder in the test; verify structural validity via Ogg page parsing instead.

### CI changes

Add `libopus-dev` to the Linux CI jobs' apt install step:
```yaml
- name: Install Linux audio + codec headers
  if: runner.os == 'Linux'
  run: sudo apt-get update && sudo apt-get install -y --no-install-recommends libopus-dev
```

macOS: `hraban/opus` bundles a static libopus, so no brew install needed.
