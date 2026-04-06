---
layout: home
title: audiorec
---

# audiorec

Record microphone + system audio as separate crash-safe files on macOS and Linux.

## Quick links

- [User Guide](user-guide) — install, usage, flags, troubleshooting
- [GitHub Repository](https://github.com/pmoust/audiorec)
- [Releases](https://github.com/pmoust/audiorec/releases)

## Features

- **Separate tracks** for mic and system audio
- **Output formats:** WAV (crash-safe), FLAC (lossless), Opus (speech-optimized lossy)
- **Rolling segmentation** for long recordings
- **Resampling** to a uniform sample rate
- **macOS per-app capture** via ScreenCaptureKit
- **RF64** for >4GB WAV files (transparent)
- **Session manifest** (JSON metadata per recording)
- **Library-first** Go API with a CLI on top
