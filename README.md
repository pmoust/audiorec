# audiorec

Go library and CLI for recording microphone + system audio as separate crash-safe WAV files on macOS and Linux. Designed for meeting and interview capture.

## Features

- **Separate tracks** — mic and system audio land in two independent WAV files, ready for per-speaker transcription or DAW post-processing.
- **Crash-safe** — WAV headers are rewritten every 2s (configurable), so a `kill -9` leaves playable files with at most 2s of tail loss.
- **Library-first** — the `session` package takes any `source.Source` implementation; the CLI is a thin wrapper.
- **Hybrid backend** — `malgo` (miniaudio) for microphone capture everywhere and Linux system audio; ScreenCaptureKit via cgo for macOS system audio.
- **Native sample rates** — no resampling; every source records at whatever the device delivers.

## Requirements

- Go 1.22+
- macOS 13+ or Linux with PipeWire/PulseAudio
- cgo enabled (Xcode command-line tools on macOS; a C compiler on Linux)

## Install

```bash
go install github.com/pmoust/audiorec/cmd/audiorec@latest
```

## Usage

```bash
# List capture devices
audiorec devices

# Record mic + system audio to ./rec/<timestamp>/{mic,system}.wav
audiorec record -o ./rec

# Mic only (skip system audio and its permission prompt)
audiorec record -o ./rec --system none

# Hard stop after 30 minutes
audiorec record -o ./rec -d 30m
```

Press Ctrl-C to stop cleanly. Files are finalized on shutdown.

### Flags

| Flag | Description |
|---|---|
| `-o DIR` | Output directory (required). A timestamped subdirectory is created under it. |
| `--session-name NAME` | Override the timestamped subdirectory name. |
| `--mic ID\|default\|none` | Microphone selection. |
| `--system ID\|default\|none` | System audio selection. |
| `-d DURATION` | Optional hard stop (`30m`, `1h`, ...). |
| `--flush-interval DUR` | WAV header flush cadence. Default `2s`. |
| `-v` | Verbose (debug) logging. |

## macOS permissions (important)

System audio capture uses ScreenCaptureKit and requires **Screen Recording** permission in System Settings → Privacy & Security → Screen Recording. The OS prompts on first run.

Microphone capture requires the standard **Microphone** permission.

### Permissions and unbundled binaries

macOS ties TCC permission grants to a signed bundle identifier. A plain `go build` binary is not a bundle, so each rebuild may look like a new app and force re-granting. For reliable permission persistence, use the Makefile target:

```bash
make app       # builds dist/audiorec.app with a stable bundle ID and ad-hoc signature
./dist/audiorec.app/Contents/MacOS/audiorec record -o ./rec
```

## Linux system audio

On Linux, system audio is captured via the default sink's monitor source (`<sink>.monitor`), which PipeWire and PulseAudio both expose. If your default sink has no monitor (rare), pass `--system <name>` explicitly.

## Library usage

```go
import (
    "context"
    "github.com/pmoust/audiorec"
)

mic := audiorec.NewMicCapture(audiorec.CaptureConfig{Channels: 1})
sys := audiorec.NewSystemAudioCapture()

sess, err := audiorec.NewSession(audiorec.SessionConfig{
    Tracks: []audiorec.Track{
        {Source: mic, Path: "mic.wav",    Label: "mic"},
        {Source: sys, Path: "system.wav", Label: "system"},
    },
})
if err != nil { panic(err) }

ctx, cancel := context.WithCancel(context.Background())
defer cancel()
if err := sess.Run(ctx); err != nil {
    // Check errors.Is(err, audiorec.ErrPermissionDenied), etc.
}
```

## Development

```bash
make test       # run all unit tests with race detector
make build      # build the CLI
make app        # macOS-only: bundle as .app for TCC
```

## Design

See `docs/superpowers/specs/2026-04-05-audiorec-design.md` for the full design spec.
