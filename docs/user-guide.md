---
layout: default
title: User Guide
nav_order: 2
---

# audiorec User Guide

`audiorec` is a command-line tool that records your microphone and your computer's system audio to **separate files**. It's designed for meeting and interview capture: you get one clean track of your own voice and one clean track of the other side, both ready to drop into a transcription tool, a DAW, or a ducking pipeline. Output formats include uncompressed WAV, lossless FLAC, and lossy Opus.

This guide walks you through installing, recording your first session, understanding what you get, and handling the gotchas. If you just want the flag reference, skip to [Flags](#flag-reference).

---

## Contents

1. [Install](#install)
2. [Your first recording](#your-first-recording)
3. [What you get](#what-you-get)
4. [Output formats](#output-formats)
5. [Selecting devices](#selecting-devices)
6. [Rolling segmentation](#rolling-segmentation)
7. [Resampling](#resampling)
8. [Per-app capture](#per-app-capture)
9. [Session manifest](#session-manifest)
10. [Flag reference](#flag-reference)
11. [macOS permissions](#macos-permissions)
12. [Linux notes](#linux-notes)
13. [Common workflows](#common-workflows)
14. [Troubleshooting](#troubleshooting)
15. [FAQ](#faq)

---

## Install

### Download a pre-built binary (recommended)

Pre-built binaries are published on the [releases page](https://github.com/pmoust/audiorec/releases). Grab the archive for your platform, verify it, extract, and put the binary on your `$PATH`.

**macOS (Apple Silicon):**

```sh
VERSION=v1.2.0
curl -LO "https://github.com/pmoust/audiorec/releases/download/${VERSION}/audiorec-${VERSION}-darwin-arm64.tar.gz"
curl -LO "https://github.com/pmoust/audiorec/releases/download/${VERSION}/audiorec-${VERSION}-darwin-arm64.tar.gz.sha256"
shasum -a 256 -c "audiorec-${VERSION}-darwin-arm64.tar.gz.sha256"
tar -xzf "audiorec-${VERSION}-darwin-arm64.tar.gz"
sudo mv audiorec /usr/local/bin/
```

**Linux (x86_64):**

```sh
VERSION=v1.2.0
curl -LO "https://github.com/pmoust/audiorec/releases/download/${VERSION}/audiorec-${VERSION}-linux-amd64.tar.gz"
curl -LO "https://github.com/pmoust/audiorec/releases/download/${VERSION}/audiorec-${VERSION}-linux-amd64.tar.gz.sha256"
sha256sum -c "audiorec-${VERSION}-linux-amd64.tar.gz.sha256"
tar -xzf "audiorec-${VERSION}-linux-amd64.tar.gz"
sudo mv audiorec /usr/local/bin/
```

### macOS: prefer the `.app` bundle for persistent permissions

macOS ties Screen Recording and Microphone permissions to a code signature + bundle identifier. A plain `audiorec` binary will prompt for permission every time you rebuild or re-download it, because macOS sees each unique binary as a new application. For normal use, download the `audiorec-<version>-darwin-arm64-app.zip` bundle from releases instead:

```sh
VERSION=v1.2.0
curl -LO "https://github.com/pmoust/audiorec/releases/download/${VERSION}/audiorec-${VERSION}-darwin-arm64-app.zip"
unzip "audiorec-${VERSION}-darwin-arm64-app.zip"
# The binary lives inside the bundle:
./audiorec.app/Contents/MacOS/audiorec --help
# Move the bundle wherever you like (e.g. /Applications):
mv audiorec.app /Applications/
# Then alias it for convenience:
alias audiorec='/Applications/audiorec.app/Contents/MacOS/audiorec'
```

The bundle has a stable `com.pmoust.audiorec` identifier, so TCC remembers your permission grants across runs.

### Build from source

See the `README.md` at the repo root. Requires Go 1.22+ and cgo.

**Opus dependency:** `--format opus` uses libopus via cgo. If you're building from source and want Opus support, install the library first:

```sh
# macOS
brew install opus opusfile

# Debian/Ubuntu
sudo apt install libopus-dev libopusfile-dev

# Fedora/RHEL
sudo dnf install opus-devel opusfile-devel
```

If you only need WAV and FLAC output, no extra libraries are required — FLAC uses a pure-Go encoder.

---

## Your first recording

Record yourself and whatever is currently playing on your computer for 30 seconds, into a directory called `rec/` in the current folder:

```sh
audiorec record -o ./rec -d 30s
```

You'll see output like:

```
time=... level=INFO msg="recording started" dir=./rec/20260406-102315 tracks=2
```

After 30 seconds (or whenever you hit `Ctrl-C`), the command returns and you'll find:

```
rec/20260406-102315/
├── manifest.json   # session metadata, per-track stats
├── mic.wav         # your microphone
└── system.wav      # whatever was playing on your speakers/headphones
```

That's it. The timestamp subdirectory (`20260406-102315`) is created automatically so repeated runs don't overwrite each other.

### Stopping a recording

- **Press `Ctrl-C`** in the terminal. audiorec catches `SIGINT`, finalizes all track files cleanly, and exits 0.
- **Pass `-d`** with a duration (`30s`, `5m`, `1h30m`) to stop automatically after that long.
- **If the process crashes or you `kill -9`** mid-session: the files are still playable, but the last ~2 seconds of audio may be truncated. audiorec rewrites the WAV header every 2 seconds (configurable via `--flush-interval`), so you never lose more than the tail of a single flush interval.

---

## What you get

Each recording session produces **one file per audio source**, in a timestamped subdirectory of your output directory.

### File layout

```
<output-dir>/<session-name>/
├── manifest.json   # always written
├── mic.wav         # present unless you pass --mic none
└── system.wav      # present unless you pass --system none
```

The session name defaults to a compact timestamp (`20260406-102315`). Override with `--session-name NAME` if you want something specific like `interview-with-alex`.

With `--segment-duration`, files rotate to numbered segments:

```
<output-dir>/<session-name>/
├── manifest.json
├── mic-001.wav
├── mic-002.wav
├── system-001.wav
└── system-002.wav
```

### Why two files instead of one?

Separate tracks let downstream tools treat your voice and the other side independently. Typical uses:

- **Per-speaker transcription.** Transcribe `mic.wav` and `system.wav` separately, then merge timestamps — you get a transcript with accurate speaker labels.
- **Ducking.** When you talk, lower the system-audio track in post; when they talk, lower yours. Impossible from a single mixed file.
- **DAW import.** Drop both files onto adjacent tracks in your DAW; they're time-aligned because both recordings started at the same moment.

---

## Output formats

Select the output format with `--format`. The default is `wav`.

### WAV (default)

Uncompressed PCM audio. Every player and audio tool in the world reads WAV files. audiorec's crash-safe flush rewrites the WAV header every `--flush-interval` (default 2s), so a crash leaves you with a playable file for everything recorded up to the last flush. For recordings that grow beyond 4 GB, audiorec **transparently upgrades to RF64** — a superset of WAV with a 64-bit size field. No user action is needed; RF64 files play in any modern tool that supports large WAV files.

### FLAC (`--format flac`)

Lossless compression, typically 40-60% smaller than WAV. Uses a pure-Go encoder (`mewkiz/flac`) — no cgo required. The encoded audio is bit-for-bit identical to the original; quality is the same as WAV.

**Limitation:** the FLAC encoder requires 16-bit integer PCM input. On macOS, ScreenCaptureKit delivers system audio as 32-bit float PCM, which is incompatible. If you use `--format flac` with system audio on macOS, that track will error out. Workarounds:

- Record mic to FLAC and system audio to WAV by using the library API directly.
- Pass `--system none` and capture only mic.
- Use `--format wav` for the full session and compress downstream.

### Opus (`--format opus`)

Lossy, speech-optimized codec via libopus (cgo required). Files are extremely compact — at the default 48 kbps bitrate, a one-hour meeting produces roughly 21 MB per track. Ideal when archival fidelity isn't required and storage is at a premium.

Control bitrate with `--opus-bitrate`:

```sh
audiorec record -o ./rec --format opus --opus-bitrate 64k   # higher quality
audiorec record -o ./rec --format opus --opus-bitrate 32k   # smaller files
```

Internally, audiorec resamples audio to 48 kHz before encoding (Opus requires 48 kHz input), regardless of what `--sample-rate` is set to. This resampling is transparent.

**Limitation:** like FLAC, Opus also rejects float32 PCM input in v1.2.0. The same macOS system-audio caveat applies — use `--system none` or `--format wav` if you need system audio on macOS with Opus.

---

## Selecting devices

### List available devices

```sh
audiorec devices
```

Example output on macOS:

```
KIND      DEFAULT       NAME
mic       yes           MacBook Pro Microphone
mic                     Scarlett 2i2 USB
```

System-audio devices only appear on Linux (as `<sink>.monitor` entries); on macOS, system audio is always captured via ScreenCaptureKit and doesn't show up in the device list.

### Use a specific microphone

Pass the device name (case-insensitive exact match, then substring fallback):

```sh
audiorec record -o ./rec --mic "Scarlett 2i2"
```

If the name isn't found, audiorec exits with a clear error:

```
error: resolve --mic "Scarlett 2i3": audiorec: device not found: "Scarlett 2i3" (run 'audiorec devices' to list)
```

### Use a specific Linux system audio source

On Linux, you can target a specific monitor source by name:

```sh
audiorec devices                              # find the monitor you want
audiorec record -o ./rec --system "alsa_output.pci-0000_00_1f.3.analog-stereo.monitor"
```

**On macOS, `--system` must be `default` or `none`** — ScreenCaptureKit doesn't expose multiple system-audio devices. audiorec will error if you pass a named system device on macOS.

### Disable one of the tracks

```sh
audiorec record -o ./rec --system none   # mic only
audiorec record -o ./rec --mic none      # system audio only
```

You can't pass `none` for both — at least one track is required.

---

## Rolling segmentation

By default, audiorec writes a single file per track for the entire session. Pass `--segment-duration` to rotate tracks at a fixed interval:

```sh
audiorec record -o ./rec --segment-duration 30m
```

Output layout changes from `mic.wav` to `mic-001.wav`, `mic-002.wav`, etc. All tracks rotate simultaneously at the same boundary, so segments from different tracks stay time-aligned. Each segment is a complete, independently playable file — crash safety applies to segments the same way it applies to full sessions.

Use cases:

- **Multi-hour recordings.** A 6-hour recording broken into 30-minute segments is far easier to navigate, back up, or hand off than a single massive file.
- **Incremental processing.** A background script can pick up completed segments for transcription while recording continues.
- **RF64 avoidance.** Segmenting before 4 GB keeps every file as a standard RIFF WAV if you need maximum compatibility with older tools. (audiorec handles >4 GB transparently via RF64, but this is a valid reason to segment anyway.)

---

## Resampling

By default, audiorec records each source at whatever sample rate the OS delivers. Tracks in the same session may have different rates (e.g., mic at 44.1 kHz, system audio at 48 kHz on macOS).

Pass `--sample-rate N` to resample all sources to a uniform rate before writing:

```sh
audiorec record -o ./rec --sample-rate 48000
```

This wraps each source with a linear interpolation resampler. When the source already delivers the requested rate, the resampler is a passthrough with zero overhead.

When to use it:

- Downstream tools that require uniform rates across tracks without post-processing.
- Feeding tracks directly into a pipeline (e.g., a streaming transcription service) that only accepts one specific rate.
- Ensuring WAV files from different sessions are trivially mixable in a DAW without re-importing.

When to skip it: if you're going to post-process with ffmpeg anyway, or your DAW handles mixed rates natively, the default (no resampling) is fine.

---

## Per-app capture

On macOS, you can restrict system audio capture to a specific application — or capture everything except a specific application:

```sh
# Capture only Zoom audio
audiorec record -o ./rec --include-app com.zoom.us

# Capture everything except the built-in Music app
audiorec record -o ./rec --exclude-app com.apple.Music

# Multiple apps, comma-separated
audiorec record -o ./rec --include-app "com.zoom.us,com.microsoft.teams2"
```

`--include-app` and `--exclude-app` are mutually exclusive — passing both is an error.

### Finding bundle IDs

Bundle identifiers are in reverse-DNS format. The easiest way to find one:

```sh
osascript -e 'id of app "Zoom"'
# → us.zoom.xos

osascript -e 'id of app "Safari"'
# → com.apple.Safari
```

You can also check the app's `Contents/Info.plist`:

```sh
defaults read /Applications/Zoom.us.app/Contents/Info.plist CFBundleIdentifier
```

### Platform and version notes

- **macOS only.** Passing `--include-app` or `--exclude-app` on Linux is an error.
- **macOS 13+:** the flags are accepted and forwarded to ScreenCaptureKit, but audio isolation is best-effort and may include or exclude more than expected.
- **macOS 14.4+:** per-app audio isolation is properly enforced by the OS.

---

## Session manifest

Every session automatically writes a `manifest.json` file alongside the track files:

```json
{
  "version": 1,
  "session_id": "20260406-102315",
  "started_at": "2026-04-06T10:23:15Z",
  "ended_at": "2026-04-06T11:23:15Z",
  "duration_seconds": 3600.0,
  "tracks": [
    {
      "label": "mic",
      "path": "mic.wav",
      "format": "wav",
      "sample_rate": 48000,
      "channels": 1,
      "started_at": "2026-04-06T10:23:15Z",
      "frames": 172800000,
      "bytes": 345600000,
      "drops": 0
    },
    {
      "label": "system",
      "path": "system.wav",
      "format": "wav",
      "sample_rate": 48000,
      "channels": 2,
      "started_at": "2026-04-06T10:23:15Z",
      "frames": 172800000,
      "bytes": 691200000,
      "drops": 0
    }
  ]
}
```

Use the manifest when:

- **Downstream tools** need to know track formats, sample rates, or start times without parsing audio file headers.
- **Quality monitoring:** the `drops` counter per track surfaces buffer overruns. A non-zero value means some audio frames were dropped under load.
- **Scripting:** parse `ended_at` and `duration_seconds` to verify that a recording ran to completion before processing it.

---

## Flag reference

```
audiorec devices                List capture devices.

audiorec record [flags]         Record a session.

  -o DIR                        Required. Output directory. A timestamped
                                subdirectory is created under it for each
                                session.

  --session-name NAME           Override the timestamp-based subdirectory
                                name. Useful for scripting or when you want
                                a human-readable session label.

  --mic VALUE                   Microphone source. One of:
                                  "default" — system default input
                                  "none"    — don't record mic
                                  <name>    — device name from
                                              `audiorec devices`. Match
                                              is case-insensitive, exact
                                              first, then substring.

  --system VALUE                System audio source. Values are the same as
                                --mic. On macOS, only "default" and "none"
                                are accepted because ScreenCaptureKit
                                doesn't support device selection.

  --include-app BUNDLE_IDS      macOS only. Comma-separated bundle IDs to
                                include in system audio capture. Mutually
                                exclusive with --exclude-app.

  --exclude-app BUNDLE_IDS      macOS only. Comma-separated bundle IDs to
                                exclude from system audio capture. Mutually
                                exclusive with --include-app.

  -d DURATION                   Optional hard stop. Accepts Go's time.
                                Duration format: "30s", "5m", "1h30m". If
                                omitted (default 0), recording continues
                                until SIGINT.

  --flush-interval DURATION     How often audiorec rewrites WAV header
                                length fields + fsyncs. Default "2s".
                                Lower values reduce worst-case data loss
                                on crash, but do not meaningfully affect
                                performance at audio rates.

  --segment-duration DURATION   Rotate tracks every DURATION. Output files
                                are named mic-001.wav, mic-002.wav, etc.
                                "0" (default) means no segmentation.

  --format VALUE                Output format. One of:
                                  "wav"  — uncompressed PCM (default)
                                  "flac" — lossless, pure Go, no cgo
                                  "opus" — lossy via libopus (cgo)

  --opus-bitrate VALUE          Target Opus bitrate. Integer (e.g. 48000)
                                or shorthand (e.g. "48k"). Default "48k".
                                Only meaningful when --format opus.

  --sample-rate N               Resample all tracks to N Hz before writing.
                                "0" (default) means no resampling; each
                                source records at its native device rate.

  -v                            Verbose (debug) logging to stderr. Enables
                                per-flush events, per-track start/end
                                events, and drop-counter details.
```

### Exit codes

| Code | Meaning |
|---|---|
| 0 | Recording completed cleanly (graceful stop or duration reached) |
| 1 | Error during recording (permission denied, device disconnect, disk full, invalid flag value) |
| 2 | Usage error (missing `-o`, unknown subcommand, missing required arg) |

### Examples

```sh
# 1 hour meeting, default devices
audiorec record -o ~/recordings -d 1h

# Named session for archival
audiorec record -o ~/recordings --session-name "alex-interview-2026-04-06"

# Mic-only (skip system audio to avoid the macOS Screen Recording prompt)
audiorec record -o ./rec --system none

# Specific USB mic, stop when killed
audiorec record -o ./rec --mic "Scarlett"

# Lossless FLAC mic recording
audiorec record -o ./rec --format flac --system none

# Opus at 64 kbps, rotate every hour
audiorec record -o ./rec --format opus --opus-bitrate 64k --segment-duration 1h

# All tracks at 48 kHz
audiorec record -o ./rec --sample-rate 48000

# Capture only Zoom on macOS
audiorec record -o ./rec --include-app us.zoom.xos

# Debug why something isn't working
audiorec record -o ./rec -v -d 10s
```

---

## macOS permissions

audiorec uses two Apple privacy systems:

### 1. Microphone (TCC)

Required for any mic recording. On first run, macOS shows a system dialog:

> "audiorec" wants to use the microphone.

Click **Allow**. The grant is remembered per-application. If you're using the `.app` bundle from a release (recommended), the grant persists across updates. If you're running a plain `go build` binary, each rebuild is treated as a new app and you'll be prompted again.

### 2. Screen Recording (ScreenCaptureKit)

Required for **system audio only**. This is Apple's constraint: the ScreenCaptureKit API doesn't expose a separate "system audio only" permission. audiorec does not capture video — only audio — but you still have to grant Screen Recording permission.

On first run without this permission, audiorec prints:

```
System audio capture requires Screen Recording permission.
Grant it in System Settings → Privacy & Security → Screen Recording,
then re-run. Microphone-only recording still works without it
(pass --system none).
```

**Workflow:**

1. Run audiorec once. It fails with the message above.
2. Open **System Settings → Privacy & Security → Screen Recording**.
3. Enable audiorec (or the `.app` bundle) in the list. If audiorec isn't listed yet, click the `+` button and add it.
4. Quit the Terminal app completely and reopen it. (macOS caches TCC state per-process; a fresh terminal session picks up the new grant.)
5. Re-run audiorec.

### If you only want mic

Pass `--system none` and you'll never trigger the Screen Recording prompt. Useful when you're interviewing someone in person and don't need to capture the other end of a video call.

---

## Linux notes

### Audio server requirements

audiorec needs either **PipeWire** (with pulse compatibility) or **PulseAudio** running. ALSA-only setups are not currently tested. To check:

```sh
pactl info        # should succeed and show a server
```

If you get "Connection refused" or "Connection terminated", start the audio server or log into a desktop session.

### How system audio capture works

On Linux, audiorec records "system audio" by capturing from your default sink's **monitor source** — PulseAudio/PipeWire automatically exposes a virtual capture device named `<your-sink>.monitor` for every output sink. This captures everything that's playing through your speakers or headphones.

```sh
audiorec devices
# Look for entries ending in .monitor — those are the system-audio sources.
```

If no monitor device shows up, your audio server is misconfigured or running in a mode that doesn't expose monitors. Pass `--system none` and fall back to mic-only, or consult your distribution's PipeWire docs.

### Headless / CI environments

audiorec runs in environments without a desktop session — but only if you have an audio server running. Containers, SSH sessions without a graphical login, and minimal server installs typically don't have one. In those environments, `audiorec devices` will either return an empty list or a "no backend" error.

---

## Common workflows

### Meeting recording → transcription

```sh
# 1. Record
audiorec record -o ~/meetings --session-name "standup-2026-04-06"

# 2. Transcribe each track separately (example with whisper.cpp)
whisper.cpp ~/meetings/standup-2026-04-06/mic.wav    -otxt -l en
whisper.cpp ~/meetings/standup-2026-04-06/system.wav -otxt -l en

# 3. You now have two transcripts: yours and everyone else's.
#    Merge by timestamp if your transcription tool supports it.
```

### Interview recording → DAW cleanup

1. Record with `--session-name interview-alex`.
2. Open your DAW.
3. Import both `mic.wav` and `system.wav` as separate tracks on the same timeline, both starting at bar 1 beat 1.
4. They're perfectly time-aligned — apply noise reduction, EQ, compression, ducking as needed, then mix down.

### Long unattended recording with safety

```sh
# Record for up to 3 hours, flush header every 1s for extra crash-safety,
# verbose logging so you can tell it's still alive if you check
audiorec record -o ~/recordings -d 3h --flush-interval 1s -v
```

### Meeting recording → archival with Opus compression

For recurring meetings where you want a permanent archive but lossless quality isn't needed, Opus at 64 kbps keeps files tiny while remaining highly intelligible for speech:

```sh
# Record mic only (skip system audio on macOS to avoid float32 + Opus incompatibility),
# rotate every hour so files stay manageable, archive at 64 kbps
audiorec record -o ~/archive --session-name "weekly-sync-2026-04-06" \
  --format opus --opus-bitrate 64k \
  --segment-duration 1h \
  --system none

# If you need system audio too, record in WAV and compress after
audiorec record -o ~/archive --session-name "weekly-sync-2026-04-06" \
  --format wav --segment-duration 1h
ffmpeg -i ~/archive/weekly-sync-2026-04-06/system-001.wav \
       -c:a libopus -b:a 64k \
       ~/archive/weekly-sync-2026-04-06/system-001.opus
```

### Multi-hour recording with segmentation

```sh
# 8-hour recording, rotate every 30 minutes, force 48 kHz
audiorec record -o ~/longform -d 8h \
  --segment-duration 30m \
  --sample-rate 48000

# Process segments as they complete (runs in parallel with recording)
for f in ~/longform/*/mic-*.wav; do
  whisper.cpp "$f" -otxt -l en &
done
```

---

## Troubleshooting

### `error: resolve --mic "X": audiorec: device not found`

The device name you passed doesn't match anything in the enumeration. Run `audiorec devices` to see the exact names. The match is case-insensitive and will do an exact-then-substring search, so partial names work (`"scarlett"` matches `"Scarlett 2i2 USB"`).

### `error: session: start "system": audiorec: permission denied: macOS Screen Recording not granted`

See [macOS permissions](#macos-permissions) above. Quick fix: either grant Screen Recording permission in System Settings, or pass `--system none` to record only the mic.

### Zero-byte or tiny WAV files after a long recording

audiorec's crash-safe header rewrites run every `--flush-interval` (default 2s). Between flushes, data is being written to disk but the header's length field is stale. If the process exited cleanly (Ctrl-C, `-d` timeout, or `return 0`), the final flush ran and the file is complete. If you see truncated files:

- Check that you stopped cleanly (Ctrl-C, not `kill -9`).
- If you did hit `kill -9`, the file is still playable up to the last successful flush — at most ~2 seconds of tail samples are missing from the header count. Players honor the header, not the raw byte count.

### `audiorec devices` returns empty on Linux

Your audio server isn't running or audiorec can't see it. Check with `pactl info`. If that fails, start PipeWire or PulseAudio; on most modern desktop distributions it starts automatically with your desktop session.

### `audiorec devices` returns empty in a container or SSH session

No audio server in the environment. Not a bug — audiorec needs a running PipeWire/PulseAudio daemon, which usually requires an interactive session.

### Recording fails immediately with `ErrDeviceDisconnected`

The device you selected was unplugged or switched by the OS between enumeration and capture start. Re-run and select again. On macOS, this commonly happens if AirPods disconnect mid-setup; audiorec intentionally does not silently switch devices mid-session (that would corrupt the recording).

### The two files have different lengths

They shouldn't, under normal stop conditions. If they do:

- One track may have hit an error (device disconnect, disk full) while the other kept going. This is by design — audiorec's partial-failure policy is "finalize the dead track, keep the living ones." Check stderr logs for events.
- With `-v`, every track end is logged with the reason.
- Check `manifest.json` — per-track `drops` and end timestamps will show which track encountered the error first.

### `--format flac` or `--format opus` fails for system audio on macOS

macOS ScreenCaptureKit delivers system audio as 32-bit float PCM. Both the FLAC encoder and the Opus encoder in v1.2.0 require 16-bit integer PCM input. If you use `--format flac` or `--format opus` and also record system audio on macOS, the system track will error out at start.

**Workarounds:**

1. Pass `--system none` to record mic only in FLAC/Opus.
2. Use `--format wav` for the full session and compress downstream with ffmpeg.
3. Record in WAV and post-convert: `ffmpeg -i system.wav -c:a flac system.flac`.

---

## FAQ

### Can I record in the background with the terminal closed?

Yes, via the usual Unix tooling: `nohup audiorec record ... &`, or a `tmux`/`screen` session, or a launchd/systemd user service. audiorec is a normal long-running process and handles SIGTERM the same way as SIGINT.

### Can I record more than two sources (e.g., mic + system + another mic)?

Not from the CLI. The library (`github.com/pmoust/audiorec`) exposes `session.New(Config{Tracks: []Track{...}})` which accepts arbitrary `Source` values, so you can construct a custom recorder with as many tracks as you want.

### Why is the mic 44.1 kHz but the system audio is 48 kHz?

Because that's what the OS delivered. By default, audiorec records each source at its device-native rate. Downstream tools handle mixed rates fine. If you need a uniform rate, pass `--sample-rate 48000` (see [Resampling](#resampling)), or resample with ffmpeg after recording.

### Does audiorec support FLAC, Opus, MP3?

FLAC and Opus are supported via `--format flac` and `--format opus`. MP3 is not supported. See [Output formats](#output-formats).

### What is manifest.json?

A JSON file written automatically alongside your track files. It records session start/end times, duration, and per-track stats (format, sample rate, frame count, byte count, drop count). See [Session manifest](#session-manifest).

### What about recordings longer than 6 hours?

Two features cover this:

- **RF64:** WAV output transparently upgrades to RF64 (64-bit size fields) when data exceeds 4 GB. No user action needed.
- **`--segment-duration`:** rotates tracks at a fixed interval (e.g., `--segment-duration 1h`) so no individual file grows very large. See [Rolling segmentation](#rolling-segmentation).

For very long recordings both are recommended together.

### Can I capture audio from a specific application?

Yes, on macOS 13+ via `--include-app` or `--exclude-app`. Audio isolation is best-effort before macOS 14.4 and fully enforced from 14.4 onward. See [Per-app capture](#per-app-capture). Linux does not support per-app capture — passing these flags on Linux is an error.

### What happens if the disk fills up mid-recording?

The affected track's next `WriteFrame` or `Flush` fails, audiorec emits an `EventError` for that track, finalizes it with whatever was successfully flushed, and keeps recording any other tracks. If both tracks are on the same filesystem, both will hit the error within the same flush cycle. The `manifest.json` records per-track error state so you can tell what happened.

### Where is the source code / how do I report a bug?

https://github.com/pmoust/audiorec — open an issue with the version, platform, exact command, and stderr output (run with `-v` for maximum detail).

---

## Further reading

- [README](../README.md) — elevator pitch, library usage, install
- [Design spec](superpowers/specs/2026-04-05-audiorec-design.md) — architecture and rationale
- [Followups](superpowers/followups-post-v1.md) — resolved and deferred issues
