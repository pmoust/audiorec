# audiorec — Design Spec

**Date:** 2026-04-05
**Status:** Approved via brainstorming, ready for implementation planning

## 1. Overview

`audiorec` is a Go library and CLI for recording audio on macOS and Linux. Its primary use case is **meeting and interview recording**: capturing the local microphone and the system's playback audio as **separate tracks** so downstream tools (transcription, DAWs, ducking) can process them independently.

The project is **library-first with a co-equal CLI**: the library defines explicit APIs that take concrete device handles and sources; the CLI is a real product on top, handling auto-resolution of defaults, flags, and human-friendly error messages.

### Locked scope (v1)

- **Platforms:** macOS (13+) and Linux.
- **Sources:** microphone + system audio, as separate tracks.
- **Output:** one WAV file per source. No mixing, no multi-channel interleaving of different sources.
- **Crash safety:** periodic WAV header rewrites so a `kill -9` leaves playable files (worst case: 2s of tail samples unrepresented in the header).
- **Device selection:** auto-default (system default input and default output's loopback) with `--mic` / `--system` overrides and a `devices` subcommand.
- **Sample rate / format:** native per source, no resampling in v1. Each track's WAV uses whatever the OS delivers (typically 48kHz or 44.1kHz; 16-bit int or 32-bit float).

### Explicit non-goals for v1

- No resampling or format conversion.
- No mixing or track alignment (each track is independent).
- No compressed output (FLAC/Opus) — WAV only.
- No segmentation / rolling chunks.
- No RF64 / W64 for >4GB files (~6.2h of 48kHz stereo 16-bit). Session logs a warning and stops cleanly if hit.
- No automated CI testing of the macOS ScreenCaptureKit backend (manual smoke testing only).
- No concurrent sessions in one process (library permits it; not a guarantee).
- No per-app capture (macOS 14.4+ feature; future work).

## 2. Architectural approach

The central constraint is **macOS system audio capture**: there is no public API for this before macOS 14.4, so **ScreenCaptureKit (macOS 13+)** is effectively the only viable path. On Linux, system audio is a PulseAudio/PipeWire monitor source that looks like a normal capture device.

We therefore use a **hybrid backend strategy**:

- **Mic on macOS + Linux:** `github.com/gen2brain/malgo` (Go bindings to miniaudio).
- **System audio on Linux:** `malgo` targeting the default sink's `.monitor` source.
- **System audio on macOS:** a small cgo + Objective-C bridge to `SCStream` with audio output from ScreenCaptureKit.

All three paths sit behind a single `source.Source` interface. The session/recorder code does not know or care which backend supplies frames.

**Rejected alternatives:**
- *Fully native backends (no miniaudio).* Too much code to write for the mic path when `malgo` already solves it cleanly.
- *miniaudio for everything.* Dealbreaker — miniaudio cannot capture system audio on macOS.

## 3. Module and package layout

Single Go module. Packages:

```
github.com/pmoust/audiorec/
├── audiorec.go                       Public API re-exports: Recorder, Source, Session, Config
├── source/                           Source interface + shared types
│   ├── source.go                     interface Source { Start/Stop/Frames/Format/Err/Close }
│   └── format.go                     Format, Frame, DeviceInfo, Kind
├── backend/
│   ├── malgo/                        miniaudio-backed Source (mic everywhere, Linux system)
│   │   ├── mic.go
│   │   ├── monitor_linux.go          default sink → monitor source resolution (//go:build linux)
│   │   └── devices.go                enumeration
│   └── sck/                          macOS ScreenCaptureKit system-audio Source
│       ├── sck_darwin.go             cgo entry points + Go side (//go:build darwin)
│       ├── sck_bridge.m              Obj-C bridge (SCStream, SCStreamConfiguration, delegate)
│       ├── sck_bridge.h
│       └── sck_stub.go               //go:build !darwin — returns ErrUnsupportedOS
├── wav/                              Crash-safe WAV writer (pure Go, no cgo)
│   └── writer.go                     streaming writer + periodic header rewrite
├── session/                          Orchestrates N Sources → N wav.Writers
│   └── session.go
├── cmd/audiorec/                     CLI (cobra or stdlib flags)
│   ├── main.go
│   ├── record.go                     `audiorec record [flags]`
│   └── devices.go                    `audiorec devices`
└── internal/
    └── logging/                      slog wrapper
```

### Boundary rules

- `source.Source` is the only contract `session/` depends on.
- Backends are gated behind build tags and never imported across platforms.
- `wav/` knows nothing about capture; it takes a `Format` and appends frames.
- `session/` is pure glue. No platform code.
- `cmd/audiorec/` is the only place that resolves "default mic" into a concrete `Source` constructor. Library APIs always take explicit sources.
- No `runtime.GOOS` checks in hot paths; use build tags.

## 4. Core types and interfaces

```go
// package source

type Format struct {
    SampleRate    int  // 48000, 44100, ...
    Channels      int  // 1 = mono, 2 = stereo
    BitsPerSample int  // 16 or 32
    Float         bool // true => 32-bit float PCM; false => signed int
}

type Frame struct {
    Data      []byte    // interleaved PCM
    NumFrames int       // samples per channel in this buffer
    Timestamp time.Time // capture time of first sample
}

type Kind int
const ( Mic Kind = iota; SystemAudio )

type DeviceInfo struct {
    ID      string
    Name    string
    Kind    Kind
    Default bool
    Format  Format // informational
}

type Source interface {
    Format() Format                    // stable after Start()
    Start(ctx context.Context) error
    Frames() <-chan Frame              // closed when capture ends
    Err() error                        // non-nil if ended with error
    Close() error                      // idempotent
}
```

### Contract rules

- **Channel-based delivery.** Backends receive OS callbacks on real-time threads, copy into a pooled buffer, and push onto a bounded channel. Real-time threads never block.
- **Bounded channel, drop-oldest on overflow.** Default buffer ≈ 32 frames (~300ms). When full, the backend drops the *oldest* frame and increments a drop counter. Rationale: in a meeting the most recent audio is the most valuable. Drops are logged and surfaced via `Stats`, not fatal.
- **No resampling, no mixing.** Each Source is independent.
- **Format frozen at Start().** Negotiated once with the OS, never changes. If the OS changes the default device mid-session, the Source ends with `Err()`; the session logs it and continues recording remaining sources.
- **Context cancellation is the normal stop path.** Cancel → backend drains → channel closes → `Err()` returns nil.
- **Device-loss → partial survival.** A dead source ends its own track cleanly; other tracks keep recording. The session returns a joined error for all per-track failures at the end.

## 5. Crash-safe WAV writer

Pure Go, no cgo, no dependencies.

### The problem

A WAV file is a RIFF container with two length fields in the header: RIFF chunk size at offset 4, data subchunk size at offset 40 (for standard PCM). Both are normally written at close. A crash mid-recording leaves them at zero, and most players refuse the file even though the PCM data on disk is intact.

### The fix

Rewrite those two 4-byte fields periodically while recording. PCM data is append-only and already durable; only the header needs to stay honest.

### API

```go
// package wav

type Writer struct { /* ... */ }

func Create(path string, fmt source.Format, opts ...Option) (*Writer, error)
func (w *Writer) WriteFrame(f source.Frame) error  // append PCM, update counter
func (w *Writer) Flush() error                     // rewrite header + fsync
func (w *Writer) Close() error                     // final flush + close; idempotent
```

### Behavior

1. `Create` writes a complete 44-byte PCM WAV header with length fields set to the current (zero) byte count, then `fsync`s. Even an immediate crash leaves a valid empty file.
2. `WriteFrame` appends `Data` to the file and bumps an internal byte counter. One `Write` syscall per frame. No header touch on the hot path.
3. `Flush` seeks to offset 4 (RIFF size = `36 + bytesWritten`), seeks to offset 40 (data size = `bytesWritten`), seeks back to end, and `fsync`s. **`Flush` is the only operation that touches the header** — single code path to reason about.
4. `Close` does a final `Flush` and closes the file. Idempotent.

### Flush interval

**Default: 2 seconds.** Configurable. Bounds worst-case data loss on crash to 2s of tail samples while keeping fsync overhead negligible at audio rates.

### Format support in v1

- 16-bit signed PCM (`WAVE_FORMAT_PCM`) and 32-bit float PCM (`WAVE_FORMAT_IEEE_FLOAT`).
- Interleaved only.
- Up to 8 channels with standard `fmt ` chunk (no `WAVEFORMATEXTENSIBLE` yet).

### What the writer does NOT do

- No RF64/W64 for >4GB files. Session logs a warning and stops cleanly on overflow.
- No write batching. One syscall per frame is fine at ~50-200 writes/sec.
- No tempfile / atomic rename. The file is the in-progress recording.

## 6. Session orchestration

`session/` is the glue turning N Sources into N crash-safe WAVs. Pure Go, no platform code.

### API

```go
// package session

type Track struct {
    Source source.Source  // already constructed by caller
    Path   string         // output WAV path
    Label  string         // "mic", "system" — used in logs/metrics
}

type Config struct {
    Tracks        []Track
    FlushInterval time.Duration  // default 2s
    OnEvent       func(Event)    // optional: drops, flushes, errors, track end
}

type Session struct { /* ... */ }

func New(cfg Config) (*Session, error)
func (s *Session) Run(ctx context.Context) error  // blocks until all tracks finish
func (s *Session) Stats() Stats                   // live per-track metrics
```

### Topology

```
Source(mic)    ──Frame chan──▶  writer goroutine  ──▶  mic.wav
                                       ▲
                              Flush ticker (2s)
                                       ▼
Source(system) ──Frame chan──▶  writer goroutine  ──▶  system.wav
```

### Goroutines per session

- **One writer goroutine per track.** Ranges over `source.Frames()`, calls `wav.Writer.WriteFrame`. That's the entire hot path. Writes are serialized; WAV writer needs no locking beyond its own mutex.
- **One flush goroutine** shared across tracks. `time.Ticker(FlushInterval)` → `Flush()` on every writer in sequence.
- **One supervisor** (the `Run` goroutine). Starts all sources, waits on all writers via `sync.WaitGroup`, watches `ctx.Done()`, fans in errors.

### Startup — all-or-nothing

1. For each Track, call `Source.Start(ctx)`.
2. If any source fails: cancel the ctx, `Close()` every already-started source, delete any WAV files already created, return the error.
3. After all sources have started successfully, create a `wav.Writer` per track using its now-stable `Format()`.
4. Launch writer goroutines and the flush ticker.

### Steady state

Frames flow through channels, writers append, ticker flushes headers every 2s. `Stats()` exposes per-track counters: frames written, bytes written, drops, last flush time.

### Shutdown — normal (ctx cancel / SIGINT)

1. Supervisor cancels the internal ctx.
2. Each source drains, closes its `Frames()` channel, sets `Err()` to nil.
3. Each writer exits its range loop and calls `wav.Writer.Close()` (final flush).
4. Flush ticker stops.
5. `Run` returns nil.

### Shutdown — partial failure

- If one source's `Frames()` closes with non-nil `Err()`, that writer finalizes its WAV cleanly and exits. The supervisor logs via `OnEvent` but does NOT cancel the other tracks.
- `Run` keeps blocking until all remaining sources end.
- `Run` returns `errors.Join(...)` of all per-track errors, or nil if everyone ended cleanly.

### Shutdown — hard crash

Nothing to do. Headers are at most 2s stale; files are closed by the kernel; next launch finds playable files.

### What session does NOT do

- No mixing, no track alignment, no timestamp correlation.
- No file segmentation.
- No retry on source error.
- No cross-filesystem coordination (disk-full on one track does not stop others; see §7).

## 7. Error handling and permissions

### Error categories

| Category | Example | Surfaced at | Session response |
|---|---|---|---|
| Config | invalid device ID, unsupported channels, unwritable path | `Source.Start` / `wav.Create` | All-or-nothing startup fails |
| Permission | macOS TCC screen-recording denied; Linux no audio server | `Source.Start` | Startup fails with typed error; CLI prints actionable help |
| Transient runtime | consumer-side buffer overflow | callback → drop counter | Log, increment `Stats.Drops`, keep recording |
| Fatal runtime | device disconnected, stream ended, backend panic | `Source.Err()` | That track finalizes; others continue |
| I/O | disk full, fsync failure | `wav.Writer` | That track ends with error; others continue |

### Exported typed errors

```go
var (
    ErrPermissionDenied   = errors.New("audiorec: permission denied")
    ErrDeviceNotFound     = errors.New("audiorec: device not found")
    ErrDeviceDisconnected = errors.New("audiorec: device disconnected")
    ErrUnsupportedFormat  = errors.New("audiorec: unsupported format")
    ErrUnsupportedOS      = errors.New("audiorec: backend not supported on this OS")
)
```

Backends wrap with `fmt.Errorf("%w: <native detail>", ErrX)` so callers use `errors.Is`.

### Permission handling

1. **macOS ScreenCaptureKit.** Requires "Screen Recording" TCC permission even for audio-only. First launch triggers the OS prompt. If denied, `SCStream` start returns a specific error code; backend returns `ErrPermissionDenied`. CLI catches and prints:
   > System audio capture requires Screen Recording permission. Grant it in System Settings → Privacy & Security → Screen Recording, then re-run. Microphone-only recording still works without it.
   
   Library does not attempt to trigger the prompt itself; starting the stream triggers it naturally.

2. **macOS microphone.** Standard mic TCC permission. `malgo` triggers on first capture. Same catch-and-explain treatment.

3. **Linux.** No TCC. In headless/container environments without an audio server, `malgo` fails enumeration; we print "no audio devices found; is PipeWire/PulseAudio running?"

### Platform gotchas with explicit guards

- **macOS sample rate.** `SCStream` delivers in its chosen format (typically 48kHz stereo float32). The sck backend does NOT request a format; it reports whatever SCStream gives via `Format()`.
- **AirPods / Bluetooth mid-session switching.** Sources capture a specific device handle at Start, not "default". If that device disconnects, the source ends with `ErrDeviceDisconnected`. We do not silently switch.
- **Linux monitor naming.** PipeWire (with pulse compat) and PulseAudio both expose monitors as `<sink-name>.monitor`. CLI auto-resolution: get default sink name, look up `<sink>.monitor`. On failure, return `ErrDeviceNotFound` and tell the user to pass `--system` explicitly.
- **cgo mandatory.** Both `malgo` and `backend/sck` require cgo. Pure-Go builds are not supported; build fails with a clear message via a stub.

### Logging

`log/slog` with attributes `backend` and `track` on every record. Session exposes a `slog.Handler`-accepting option. **No logging from real-time threads** — only from writer/supervisor goroutines.

### Disk-full semantics

Treated as fatal for the affected track only, consistent with the partial-survival rule. Same-disk tracks will both hit it within the same flush cycle; cross-filesystem tracks behave independently.

## 8. Testing strategy

The `Source` interface is the test seam. Everything above it (session, wav, CLI) is tested with fake sources on all platforms. Everything below it (backends) gets minimal platform-gated smoke testing.

| Layer | Test type | Hardware? | CI? |
|---|---|---|---|
| `wav/` | Pure unit | no | yes, all platforms |
| `session/` | Unit + integration w/ fake sources | no | yes, all platforms |
| `backend/malgo/` | Enumerate + open/close smoke | via host audio server | yes |
| `backend/sck/` | Manual smoke only | real macOS | no (manual) |
| `cmd/audiorec/` | E2E with fake backend (build tag) | no | yes |

### `wav/` — the critical tests

- **Crash recovery.** Write known PCM, `Flush()`, drop the `*Writer` without `Close()`, reopen the file with an independent reader, verify it parses as valid WAV and contains exactly the pre-flush samples.
- **Header math.** For a matrix of (sample rate, channels, bits, frame count), write and close, parse with independent reader, assert RIFF size == 36 + dataSize and dataSize == frames × channels × bytes-per-sample.
- **Format coverage.** 16-bit int mono, 16-bit int stereo, 32-bit float stereo.
- **Idempotent Close.** Double-close is a no-op.
- **Flush frequency.** Inspect file contents between flushes via a read-only fd.

### `session/` — using a `fakeSource` helper

```go
type fakeSource struct {
    format source.Format
    frames []source.Frame
    delay  time.Duration
    endErr error
    ch     chan source.Frame
}
```

Tests:
- **Happy path, two tracks** of different formats. Verify both WAVs are playable and contain exactly the scripted samples.
- **Partial failure.** One source ends with `ErrDeviceDisconnected` at 5s, the other continues to 10s. Verify both files have correct durations and `Run` returns a joined error containing only the failure.
- **Startup failure.** Second source's Start returns an error. Verify the first source is Closed, no WAVs on disk, correct error returned.
- **Backpressure / drops.** Fake source produces faster than a slow writer consumes. Verify `Stats.Drops` increments, drop-oldest semantics preserved, no deadlock.
- **Flush ticker with fake clock.** Verify Flush fires on tick and on Close, nowhere else.
- **Cancel at arbitrary times** (0ms, 100ms, 1s, 5s). Resulting WAV valid and within `[cancel - flushInterval, cancel]` duration.

### `backend/malgo/`

- Device enumeration returns without error, at least one device or `t.Skip`.
- Open default mic, read 100ms, close, repeated 10× (leak/handle-reuse).
- Linux monitor resolution: default sink → `<sink>.monitor` → `DeviceInfo{Kind: SystemAudio}`.
- Shape-only assertions on frame flow. No content assertions.

### `backend/sck/`

- Stub on non-darwin returns `ErrUnsupportedOS`.
- **Manual smoke only on darwin in v1.** Runbook in `backend/sck/README.md`: `go test -tags=sck_smoke ./backend/sck`, grant permission when prompted, verify 3s of system audio in a playable WAV. Automated CI requires a signed bundle and TCC-primed runner — out of scope for v1.

### `cmd/audiorec/`

- Flag parsing and device resolution via build-tagged fake backend.
- E2E: `audiorec record -o /tmp/xxx -d 2s` with fakes; verify exit code 0, two WAVs at expected paths with expected durations.

### Explicit non-testing in v1

- No audio quality / sample correctness assertions from real hardware.
- No long-duration stress tests in CI (manual acceptance before tagging v1).
- No concurrent-sessions tests (not a v1 guarantee).

## 9. CLI shape (sketch)

```
audiorec devices
  # lists input and system-audio devices, marks defaults

audiorec record [flags]
  -o, --output-dir DIR       required; session directory
      --session-name NAME    default: timestamp
      --mic ID|"default"|"none"
      --system ID|"default"|"none"
  -d, --duration DURATION    optional hard stop
      --flush-interval DUR   default 2s
  -v, --verbose              debug-level logging
```

Default invocation `audiorec record -o ./rec` captures default mic + default system audio to `./rec/<timestamp>/mic.wav` and `./rec/<timestamp>/system.wav`. SIGINT stops cleanly.

## 10. Open questions deferred to planning

- Exact CLI flag library (cobra vs stdlib `flag`) — planning decision, no design impact.
- Sidecar JSON manifest (session metadata, per-track formats, start timestamps) — worth including in v1 even though we don't use it for sync. Planning will decide.
- Exact shape of `Stats` and `Event` types — planning.
