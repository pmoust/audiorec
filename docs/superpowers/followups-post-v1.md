# audiorec — Post-merge follow-ups

This list was produced by the final architectural review of the Wave 0–E implementation before merging `dev/audiorec-v1` into `main`. The review verdict was **MERGE WITH CAVEATS**: architecture sound, no blockers, but three items should land before tagging v1, plus six minor cleanups for later.

## Status: fix wave landed 2026-04-05

- ✅ **S1** — fixed in `f4a5afd` (Linux monitor DeviceID plumbed via `malgo.DefaultSystemAudioCaptureConfig`)
- ✅ **S2** — fixed in `b77802b` (CLI device-name resolution via `malgo.FindCaptureConfig`; macOS `--system` rejects named devices with a clear error)
- ✅ **S3** — fixed in `7d4d66f` (dead `Stats`/`TrackStats` types deleted)
- ✅ **P1** — fixed in `339cc48` (replaced hand-rolled `contains`/`indexOf` with `strings.Contains`)
- ✅ **P2** — fixed in `c3cdd7d` (dead `unsafe.Pointer(nil)` line and unused import removed)
- ⬜ **P3, P4, P5, P6, P7** — deferred to v1.1 (details below)

The project is now ready for a v1 tag. The remaining items below are v1.1 material.

## Status: v1.1 fix wave landed 2026-04-06

- ✅ **P3** — fixed in `8673a49` (malgo Capture.Err() wired to onStop callback via deviceStoppedCh)
- ✅ **P4** — fixed in `988feda` (sck error codes 2/3/4 wrapped in new source.ErrBackendFailure sentinel; code 2 uses ErrDeviceNotFound)
- ✅ **P5** — fixed in `f89c281` (TestMapError pins classification strings)
- ✅ **P6** — fixed in `404c9a4` (sck import moved to platform files via blank import in audiorec_linux.go)
- ✅ **P7** — fixed in `3796999` (flush ticker emits EventError on flush failures)
- ✅ **Test gap A** — fixed in `24463a1` (TestRun_WavCreateFailure_RollsBackCleanly covers Phase-2 rollback)

## Status: v1.2 test coverage wave landed 2026-04-06

- ✅ **Test gap B** — fixed in `517e792` (WriterFactory injection seam + TestRun_FlushTickerError_EmitsEventErrorAndContinues)
- ✅ **Test gap C** — fixed in `5318719` (TestCapture_NullBackend_EndToEnd via CaptureConfig.Backends and ma.BackendNull; skips on platforms where null backend unavailable)
- ✅ **Test gap D** — fixed in `9fbf98b` (TestMapSCKError + TestSCKRegistry; pure-Go coverage of sck bridge; full Obj-C automation still deferred as it requires TCC-primed signed bundles)

## Before tagging v1 (✅ all resolved)

### S1 — Linux system-audio default captures the mic, not the monitor

**Severity:** functional bug; README promises a behavior the code doesn't deliver.

**Location:** `audiorec_linux.go:17-22` (the `newSystemAudioDefault` function).

**What's wrong:** the function calls `malgo.DefaultSystemAudioDevice()` to find the monitor source, then discards the returned `DeviceInfo` and constructs `malgo.NewCapture(malgo.CaptureConfig{Channels: 2})` with a nil `DeviceID`. miniaudio interprets nil as "OS default capture device," which on PipeWire/PulseAudio is typically the default microphone, not the sink monitor. A Linux user following the README's promise will record the mic twice.

**Fix sketch:** plumb the monitor's `ma.DeviceID` end-to-end.

- Change `DefaultSystemAudioDevice` in `backend/malgo/monitor_linux.go` to return a `(*ma.DeviceID, source.DeviceInfo, error)` triple, or expose a new helper `DefaultSystemAudioCaptureConfig() (CaptureConfig, error)` that returns a populated `CaptureConfig`.
- In `audiorec_linux.go`, pass the resolved `DeviceID` into `malgo.NewCapture(CaptureConfig{DeviceID: id, Channels: 2})`.
- The `DeviceID` needs to be obtained from malgo's raw device list in `Enumerate()` — currently we only keep `d.ID.String()`, which isn't round-trippable to a `*ma.DeviceID`. Options: (a) also keep the raw `ma.DeviceID` alongside `DeviceInfo` via an unexported field, or (b) add a second lookup in `DefaultSystemAudioDevice` that re-lists devices and matches by name/ID.

Estimated ~20–40 lines across `monitor_linux.go`, `devices.go`, and `audiorec_linux.go`.

### S2 — CLI silently ignores `--mic` / `--system` device-name arguments

**Severity:** UX bug.

**Location:** `cmd/audiorec/record.go:60-75`.

**What's wrong:** the flag help says `"default", "none", or a device name`, but the code only tests `!= "none"` and always constructs defaults. A user running `audiorec record --mic "Scarlett 2i2"` silently gets the default mic with no warning.

**Fix options (pick one):**

1. **Reject named values in v1.** The simplest fix: if the flag value is not `default` or `none`, return an error pointing the user at `audiorec devices`. Update the flag help to say so. ~10 lines.
2. **Resolve by name or ID.** On non-special values, call `audiorec.EnumerateMalgoDevices()` and match against `DeviceInfo.ID` or `DeviceInfo.Name`. Requires plumbing the resolved `*ma.DeviceID` through `CaptureConfig` (same prerequisite as S1). ~30–50 lines.

Option 1 ships the promise the CLI already makes; option 2 ships the richer promise the flag help describes. Option 2 is the better v1 outcome if S1 is being fixed anyway, because the plumbing overlaps.

### S3 — `Stats` / `TrackStats` types are dead code

**Severity:** API cleanliness. Not a bug, but a public type that is never populated or exposed.

**Location:** `session/session.go:98-107` (types) and nowhere else (no method, no event field).

**Fix options:**

1. **Delete.** Remove the types. `audiorec.Stats` re-export in `audiorec.go:52` also goes. ~10 lines deleted.
2. **Wire up.** Add `(*Session).Stats() Stats` method, populate from per-writer counters, emit `TrackStats.Drops` into `Event{Kind: EventFrameDropped}`. Requires the backends to surface their drop counts into session. ~30–50 lines.

Deletion is honest about the v1 scope. Wiring up is better if we want the metrics hook available to library consumers from day one.

## Post-v1 minor cleanups (don't block)

### P1 — Hand-rolled `strings.Contains`

**Location:** `backend/malgo/mic.go:218-233` (the `contains` and `indexOf` helpers).

**Fix:** import `strings` and use `strings.Contains`. Delete the two helpers. ~8 lines.

### P2 — Dead `unsafe.Pointer(nil)` line

**Location:** `backend/malgo/mic.go:136`.

**What's wrong:** the file has `_ = unsafe.Pointer(nil) // silence unused-import on some toolchains` plus an `unsafe` import that isn't used anywhere else in the file.

**Fix:** remove the line and the `unsafe` import.

### P3 — `Capture.Err()` is never set

**Location:** `backend/malgo/mic.go:166`.

**What's wrong:** `Err()` returns `c.err`, which no code path assigns to. On clean shutdown, returning nil is correct; but if a device disconnects mid-stream, there's currently no wiring from miniaudio's device state callback into `c.err`, so the caller just sees a stalled stream with nil `Err()`.

**Fix:** hook malgo's device state change notification (`ma_device_notification_proc` or whatever malgo exposes) and set `c.err = source.ErrDeviceDisconnected` on disconnect before closing the frames channel.

### P4 — sck error codes 2/3/4 not wrapped in typed sentinels

**Location:** `backend/sck/sck_darwin.go:223-235` (`mapSCKError`).

**What's wrong:** only code 1 (permission denied) is wrapped with a typed error. Codes 2 (no displays), 3 (init failed), 4 (start failed) return bare `fmt.Errorf` strings. A library caller can't `errors.Is` these.

**Fix:** add sentinels (e.g., `ErrNoDisplays`) or reuse existing ones (`ErrDeviceNotFound` for code 2), and wrap.

### P5 — `mapError` substring matching is fragile

**Location:** `backend/malgo/mic.go:198-216`.

**What's wrong:** classifies miniaudio errors by substring match on the error string. Will break silently if miniaudio rewords its errors.

**Fix:** map by `ma.Result` codes if malgo exposes them. Otherwise, accept the fragility and add a regression test.

### P6 — Misleading `var _ = sck.NewSystemAudio`

**Location:** `audiorec.go:74`.

**What's wrong:** the comment says "ensure sck is reachable on both platforms." The import already makes sck reachable; the `_ =` assignment does nothing extra. On Linux the stub is compiled via the `sck` import alone.

**Fix:** either remove the line entirely or replace with a blank import `_ "github.com/pmoust/audiorec/backend/sck"` with a comment explaining it's kept to ensure the stub compiles on non-darwin builds (which is already guaranteed by the normal import, so removal is cleaner).

### P7 — Flush ticker failures not routed through `OnEvent`

**Location:** `session/session.go:249` (the `runFlushTicker` error log path).

**What's wrong:** when `wav.Writer.Flush` fails (e.g., disk full), the ticker logs at debug and continues. Library consumers using `OnEvent` never see it; they only learn about the problem if the next `WriteFrame` also fails, which routes through `EventError`.

**Fix:** emit `EventError` or a new `EventFlushFailed` from the ticker on flush errors.

## Test coverage gaps (noted, not scheduled)

- No test for `session.Run` when `wav.Create` fails on track N>0 (Phase-2 rollback path).
- No test for `runFlushTicker` error handling.
- No end-to-end test running a real malgo capture for 1–2s and verifying the produced WAV decodes.
- No Go-side tests for the sck Obj-C bridge (validated only via manual CLI runs on macOS).

None of these are required for v1; they're candidates for a v1.1 test hardening pass.
