# audiorec User Guide

`audiorec` is a command-line tool that records your microphone and your computer's system audio to **two separate WAV files**. It's designed for meeting and interview capture: you get one clean track of your own voice and one clean track of the other side, both ready to drop into a transcription tool, a DAW, or a ducking pipeline.

This guide walks you through installing, recording your first session, understanding what you get, and handling the gotchas. If you just want the flag reference, skip to [Flags](#flag-reference).

---

## Contents

1. [Install](#install)
2. [Your first recording](#your-first-recording)
3. [What you get](#what-you-get)
4. [Selecting devices](#selecting-devices)
5. [Flag reference](#flag-reference)
6. [macOS permissions](#macos-permissions)
7. [Linux notes](#linux-notes)
8. [Common workflows](#common-workflows)
9. [Troubleshooting](#troubleshooting)
10. [FAQ](#faq)

---

## Install

### Download a pre-built binary (recommended)

Pre-built binaries are published on the [releases page](https://github.com/pmoust/audiorec/releases). Grab the archive for your platform, verify it, extract, and put the binary on your `$PATH`.

**macOS (Apple Silicon):**

```sh
VERSION=v1.0.1
curl -LO "https://github.com/pmoust/audiorec/releases/download/${VERSION}/audiorec-${VERSION}-darwin-arm64.tar.gz"
curl -LO "https://github.com/pmoust/audiorec/releases/download/${VERSION}/audiorec-${VERSION}-darwin-arm64.tar.gz.sha256"
shasum -a 256 -c "audiorec-${VERSION}-darwin-arm64.tar.gz.sha256"
tar -xzf "audiorec-${VERSION}-darwin-arm64.tar.gz"
sudo mv audiorec /usr/local/bin/
```

**Linux (x86_64):**

```sh
VERSION=v1.0.1
curl -LO "https://github.com/pmoust/audiorec/releases/download/${VERSION}/audiorec-${VERSION}-linux-amd64.tar.gz"
curl -LO "https://github.com/pmoust/audiorec/releases/download/${VERSION}/audiorec-${VERSION}-linux-amd64.tar.gz.sha256"
sha256sum -c "audiorec-${VERSION}-linux-amd64.tar.gz.sha256"
tar -xzf "audiorec-${VERSION}-linux-amd64.tar.gz"
sudo mv audiorec /usr/local/bin/
```

### macOS: prefer the `.app` bundle for persistent permissions

macOS ties Screen Recording and Microphone permissions to a code signature + bundle identifier. A plain `audiorec` binary will prompt for permission every time you rebuild or re-download it, because macOS sees each unique binary as a new application. For normal use, download the `audiorec-<version>-darwin-arm64-app.zip` bundle from releases instead:

```sh
VERSION=v1.0.1
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

After 30 seconds (or whenever you hit `Ctrl-C`), the command returns and you'll find two files:

```
rec/20260406-102315/
├── mic.wav      # your microphone
└── system.wav   # whatever was playing on your speakers/headphones
```

That's it. The timestamp subdirectory (`20260406-102315`) is created automatically so repeated runs don't overwrite each other.

### Stopping a recording

- **Press `Ctrl-C`** in the terminal. audiorec catches `SIGINT`, finalizes both WAV files cleanly, and exits 0.
- **Pass `-d`** with a duration (`30s`, `5m`, `1h30m`) to stop automatically after that long.
- **If the process crashes or you `kill -9`** mid-session: the WAVs are still playable, but the last ~2 seconds of audio may be truncated. audiorec rewrites the WAV header every 2 seconds (configurable via `--flush-interval`), so you never lose more than the tail of a single flush interval.

---

## What you get

Each recording session produces **one WAV file per audio source**, in a timestamped subdirectory of your output directory.

### File layout

```
<output-dir>/<session-name>/
├── mic.wav      # always present unless you pass --mic none
└── system.wav   # always present unless you pass --system none
```

The session name defaults to a compact timestamp (`20260406-102315`). Override with `--session-name NAME` if you want something specific like `interview-with-alex`.

### Why two files instead of one?

Separate tracks let downstream tools treat your voice and the other side independently. Typical uses:

- **Per-speaker transcription.** Transcribe `mic.wav` and `system.wav` separately, then merge timestamps — you get a transcript with accurate speaker labels.
- **Ducking.** When you talk, lower the system-audio track in post; when they talk, lower yours. Impossible from a single mixed file.
- **DAW import.** Drop both files onto adjacent tracks in your DAW; they're time-aligned because both recording started at the same moment.

### Sample rates and formats

Each track records at **whatever sample rate the OS delivers**. audiorec does not resample in v1. The most common combinations:

| Platform | Mic | System audio |
|---|---|---|
| macOS | 48 kHz or 44.1 kHz, 16-bit int | 48 kHz stereo, 32-bit float (ScreenCaptureKit) |
| Linux | 48 kHz mono, 16-bit int | whatever the default sink's monitor delivers |

This means `mic.wav` and `system.wav` may have **different sample rates** in the same session. Every tool you're likely to feed them into (ffmpeg, whisper.cpp, Audacity, Logic, Reaper, Pro Tools) handles this without issue. If you need everything at a single rate, resample downstream with ffmpeg:

```sh
ffmpeg -i mic.wav -ar 48000 mic-48k.wav
```

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
                                  "default" — system default input (the
                                              current behavior of
                                              unspecified flag)
                                  "none"    — don't record mic
                                  <name>    — device name from
                                              `audiorec devices`. Match
                                              is case-insensitive, exact
                                              first, then substring.

  --system VALUE                System audio source. Values are the same as
                                --mic. On macOS, only "default" and "none"
                                are accepted because ScreenCaptureKit
                                doesn't support device selection.

  -d DURATION                   Optional hard stop. Accepts Go's time.
                                Duration format: "30s", "5m", "1h30m". If
                                omitted (default 0), recording continues
                                until SIGINT.

  --flush-interval DURATION     How often audiorec rewrites WAV header
                                length fields + fsyncs. Default "2s".
                                Lower values reduce worst-case data loss
                                on crash, but do not meaningfully affect
                                performance at audio rates.

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

### The two WAV files have different lengths

They shouldn't, under normal stop conditions. If they do:

- One track may have hit an error (device disconnect, disk full) while the other kept going. This is by design — audiorec's partial-failure policy is "finalize the dead track, keep the living ones." Check stderr logs for events.
- With `-v`, every track end is logged with the reason.

---

## FAQ

### Can I record in the background with the terminal closed?

Yes, via the usual Unix tooling: `nohup audiorec record ... &`, or a `tmux`/`screen` session, or a launchd/systemd user service. audiorec is a normal long-running process and handles SIGTERM the same way as SIGINT.

### Can I record more than two sources (e.g., mic + system + another mic)?

Not from the CLI in v1. The library (`github.com/pmoust/audiorec`) exposes `session.New(Config{Tracks: []Track{...}})` which accepts arbitrary `Source` values, so you can construct a custom recorder with as many tracks as you want. The CLI will likely grow multi-mic support in a future minor release.

### Why is the mic 44.1 kHz but the system audio is 48 kHz?

Because that's what the OS delivered. audiorec never resamples — each track records at its device-native rate. Downstream tools handle mixed rates fine. If you need a uniform rate, resample with ffmpeg after recording (see [Sample rates and formats](#sample-rates-and-formats)).

### Does audiorec support FLAC, Opus, MP3?

Not in v1. Output is uncompressed WAV (16-bit signed PCM or 32-bit float PCM, depending on what the backend delivers). Compressed output is on the roadmap. For now, compress downstream:

```sh
ffmpeg -i mic.wav -c:a flac mic.flac        # lossless ~40-60% of WAV
ffmpeg -i mic.wav -c:a libopus -b:a 48k mic.opus   # speech-quality lossy
```

### What happens if the disk fills up mid-recording?

The affected track's next `WriteFrame` or `Flush` fails, audiorec emits an `EventError` for that track, finalizes its WAV with whatever was successfully flushed, and keeps recording any other tracks on different filesystems. If both tracks are on the same filesystem (the common case), both will hit the error within the same flush cycle.

### Can I capture audio from a specific application?

Not in v1. The macOS 14.4+ ScreenCaptureKit API supports per-app audio capture, but audiorec currently uses the system-wide audio path. Per-app capture is on the v2 roadmap.

### Where is the source code / how do I report a bug?

https://github.com/pmoust/audiorec — open an issue with the version, platform, exact command, and stderr output (run with `-v` for maximum detail).

---

## Further reading

- [README](../README.md) — elevator pitch, library usage, install
- [Design spec](superpowers/specs/2026-04-05-audiorec-design.md) — architecture and rationale
- [Followups](superpowers/followups-post-v1.md) — resolved and deferred issues
