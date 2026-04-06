# Resampling — Design Spec

**Date:** 2026-04-06
**Status:** Approved

## Overview

Add a `--sample-rate N` CLI flag that resamples every track to a uniform target rate before writing. Implemented as a `source.Source` wrapper so the session and writer layers are unaware of resampling — they just see a Source at the target rate.

## Architecture

### Package layout

```
resample/
├── resample.go       # Wrap function + resampling Source implementation
└── resample_test.go  # rate-conversion math, passthrough, multi-channel
```

### Core API

```go
// Wrap returns a Source whose Format().SampleRate is targetRate. If the
// inner source's rate already matches, Wrap returns src directly (zero
// overhead). Otherwise it spawns a goroutine that reads from src.Frames(),
// resamples each chunk, and re-delivers on a new channel.
func Wrap(src source.Source, targetRate int) source.Source
```

The returned Source:
- `Format()` returns a copy of the inner format with `SampleRate = targetRate`.
- `Start(ctx)` delegates to the inner source's `Start`, then spawns the resampler goroutine.
- `Frames()` returns a new channel fed by the resampler.
- `Err()` returns the inner source's `Err()`.
- `Close()` delegates to the inner source's `Close()`.

### Resampling algorithm

**Linear interpolation** for v1. Rationale:
- audiorec's primary use case is meeting/interview recording (speech). Speech is bandwidth-limited to ~8kHz; even at 16kHz/44.1kHz/48kHz sample rates, linear interpolation introduces negligible aliasing artifacts at speech frequencies.
- Pure Go, zero dependencies, ~60 lines of math.
- If the Opus writer also resamples internally (see the Opus spec), the global resampler is redundant for `--format opus` — but it's still valuable for WAV and FLAC users who want uniform rates.

**Implementation:**

```go
type resampler struct {
    ratio    float64 // targetRate / sourceRate
    channels int
    // Per-channel state: fractional sample position + last sample value
    pos      []float64
    lastSamp []float64 // last seen sample per channel (for interpolation)
}

func (r *resampler) process(in source.Frame) source.Frame {
    // For each output sample at position p:
    //   find the two nearest input samples at floor(p) and ceil(p)
    //   linearly interpolate between them
    //   advance p by 1/ratio
    // Output frame has NumFrames = round(in.NumFrames * ratio)
}
```

Handles:
- Mono and multi-channel (up to 8, matching source.Format limits).
- int16 and float32 PCM (detect from `Format.BitsPerSample` and `Format.Float`).
- Upsampling (44100→48000) and downsampling (48000→44100).
- Fractional sample positions carried across frame boundaries (no discontinuities between chunks).

### Passthrough optimization

```go
func Wrap(src source.Source, targetRate int) source.Source {
    if targetRate <= 0 || targetRate == src.Format().SampleRate {
        return src // no-op
    }
    return &resamplingSource{inner: src, targetRate: targetRate}
}
```

The wrapper's `Start` calls `inner.Start`, reads the now-stable `inner.Format()`, initializes the resampler, and launches the goroutine.

### Goroutine model

```
inner.Frames() ──▶ resampler goroutine ──▶ resampled channel ──▶ session writer
```

The resampler goroutine ranges over `inner.Frames()`, calls `r.process(frame)`, and sends the result on its own channel. Channel buffer matches the inner source's (32 frames). Backpressure: if the consumer can't keep up, the resampler blocks on send, which blocks on the inner read, which triggers the inner source's drop-oldest behavior. No additional drop logic needed.

The goroutine exits when `inner.Frames()` closes. It then closes its own output channel.

### CLI

```
--sample-rate N     resample all tracks to N Hz before writing.
                    0 = no resampling (default, current behavior).
                    Common values: 16000, 44100, 48000.
```

When set, the CLI wraps each Source with `resample.Wrap(src, *sampleRate)` before constructing tracks. Applied before the session sees the source, so the session, the writer, and the manifest all see the resampled rate.

### Edge cases

- **Source rate equals target rate:** passthrough, zero overhead.
- **Source rate is 0 or unknown:** should never happen (backends always set a rate after Start). If it does, `Wrap` returns the source as-is with a logged warning.
- **Very large ratio (e.g., 8000→192000):** linear interpolation works but produces large output buffers. Acceptable for v1; add a ratio-limit warning in v2 if needed.
- **Format changes mid-session:** the spec says Format is frozen after Start. The resampler reads Format once after Start and never checks again.

### Testing

- `TestWrap_Passthrough`: verify that `Wrap(src, src.Format().SampleRate)` returns `src` directly (pointer equality).
- `TestResample_44100to48000_Mono16`: create a fake source at 44100 Hz, wrap to 48000, drain frames, verify output format reports 48000, verify total sample count is proportional (`count_out / count_in ≈ 48000/44100`).
- `TestResample_48000to44100_Stereo`: downsampling case.
- `TestResample_48000to16000`: large downsample ratio (3:1), verify output is still well-formed.
- `TestResample_PreservesSineWave`: generate a 440Hz sine wave at 44100 Hz, resample to 48000, verify the output has a dominant frequency near 440Hz (via zero-crossing count, not FFT — keep it simple).
- `TestResample_Float32`: verify float32 PCM path works.
- `TestResample_FrameBoundary`: send two small frames, verify no discontinuity at the boundary (the fractional position carries over).
