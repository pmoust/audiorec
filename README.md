# audiorec

[![CI](https://github.com/pmoust/audiorec/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/pmoust/audiorec/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Go library and CLI for recording microphone + system audio as separate, crash-safe tracks on macOS and Linux. Outputs WAV, FLAC, or Opus. Designed for meeting and interview capture.

**CLI users:** see [`docs/user-guide.md`](docs/user-guide.md) for the full walkthrough — install, first recording, device selection, macOS permissions, Linux setup, flag reference, common workflows, troubleshooting, and FAQ.

## Features

- **Separate tracks** — mic and system audio land in independent files, ready for per-speaker transcription or DAW post-processing.
- **Crash-safe** — WAV headers are rewritten every 2s (configurable); a `kill -9` leaves playable files with at most one flush interval of tail loss. FLAC and Opus segments are also finalized on each flush.
- **Multi-format output** — `--format wav` (default, uncompressed), `--format flac` (lossless, pure Go, ~40-60% smaller), `--format opus` (lossy via libopus, ideal for archiving meetings).
- **Rolling segmentation** — `--segment-duration 30m` rotates tracks to `mic-001.wav`, `mic-002.wav`, … All segments are independently playable; multi-hour recordings stay manageable.
- **Resampling** — `--sample-rate 48000` wraps all sources at a uniform rate before writing, so downstream tools don't need to deal with mixed rates.
- **Per-app capture (macOS)** — `--include-app com.zoom.us` or `--exclude-app com.apple.Terminal` isolates audio by bundle ID (macOS 13+, best-effort before 14.4).
- **RF64 / large-file support** — WAV output transparently upgrades to RF64 when data exceeds 4 GB. No user action needed.
- **Session manifest** — `manifest.json` is written alongside track files with per-track format, timestamps, frame/byte/drop counters, and start/end times.
- **Library-first** — the `session` package takes any `source.Source` implementation; the CLI is a thin wrapper.
- **Hybrid backend** — `malgo` (miniaudio) for microphone capture everywhere and Linux system audio; ScreenCaptureKit via cgo for macOS system audio.

## Requirements

- Go 1.22+
- macOS 13+ or Linux with PipeWire/PulseAudio
- cgo enabled (Xcode command-line tools on macOS; a C compiler on Linux)
- **Opus output:** `libopus-dev` and `libopusfile-dev` on Linux (`brew install opus opusfile` on macOS)

## Install

```bash
# Latest release via go install (v1.2.0)
go install github.com/pmoust/audiorec/cmd/audiorec@v1.2.0
```

Pre-built binaries are also available on the [releases page](https://github.com/pmoust/audiorec/releases) — download the archive for your platform, verify the checksum, and put the binary on your `$PATH`. See the [user guide](docs/user-guide.md#install) for the full download + verification steps.

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

# Record to FLAC (lossless compression)
audiorec record -o ./rec --format flac

# Record to Opus at 64 kbps (lossy, compact)
audiorec record -o ./rec --format opus --opus-bitrate 64k

# Rotate tracks every 30 minutes
audiorec record -o ./rec --segment-duration 30m

# Force all tracks to 48 kHz
audiorec record -o ./rec --sample-rate 48000

# Capture only Zoom audio on macOS
audiorec record -o ./rec --include-app com.zoom.us
```

Press Ctrl-C to stop cleanly. Files are finalized on shutdown.

### Flags

| Flag | Description |
|---|---|
| `-o DIR` | Output directory (required). A timestamped subdirectory is created under it. |
| `--session-name NAME` | Override the timestamped subdirectory name. |
| `--mic VALUE` | `"default"`, `"none"`, or a device name (case-insensitive substring match). |
| `--system VALUE` | `"default"`, `"none"`, or a device name. On macOS only `"default"` and `"none"` are accepted. |
| `--include-app BUNDLE_IDS` | macOS only: comma-separated bundle IDs to include in system audio. |
| `--exclude-app BUNDLE_IDS` | macOS only: comma-separated bundle IDs to exclude from system audio. |
| `-d DURATION` | Optional hard stop (`30s`, `5m`, `1h`, …). |
| `--flush-interval DUR` | WAV header flush cadence. Default `2s`. |
| `--segment-duration DUR` | Rotate tracks every DURATION (`0` = no segmentation). |
| `--format VALUE` | Output format: `"wav"` (default), `"flac"`, or `"opus"`. |
| `--opus-bitrate VALUE` | Opus bitrate: integer Hz or shorthand like `48k`. Default `48k`. |
| `--sample-rate N` | Resample all tracks to N Hz before writing (`0` = no resampling). |
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

**Opus on Linux** requires `libopus-dev` and `libopusfile-dev` installed before building:

```bash
sudo apt install libopus-dev libopusfile-dev   # Debian/Ubuntu
sudo dnf install opus-devel opusfile-devel     # Fedora/RHEL
```

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
