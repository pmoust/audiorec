#ifndef AUDIOREC_SCK_BRIDGE_H
#define AUDIOREC_SCK_BRIDGE_H

#include <stdint.h>
#include <stddef.h>

// Opaque handle for a running capture session. One per Source.
typedef struct sck_capture sck_capture_t;

// Callback invoked from an arbitrary dispatch queue thread when new audio
// frames are available. `data` points to interleaved 32-bit float PCM;
// `num_frames` is samples per channel; `channels` is the channel count.
// `sample_rate` is the stream's sample rate. `user` is the pointer passed
// to sck_capture_create.
typedef void (*sck_audio_cb)(const float* data, int num_frames, int channels,
                             int sample_rate, void* user);

// sck_capture_create constructs a new capture object. It does NOT start
// the stream. user is stored and passed back to cb on every audio buffer.
sck_capture_t* sck_capture_create(sck_audio_cb cb, void* user);

// sck_capture_start begins audio-only capture via SCStream. Returns 0 on
// success, nonzero on error (see sck_capture_last_error_code).
// On success, cb begins firing on a background queue.
int sck_capture_start(sck_capture_t* c);

// sck_capture_start_filtered is like sck_capture_start but accepts an
// optional list of application bundle identifiers to include or exclude
// from audio capture. bundleIDs is a C array of NUL-terminated strings;
// bundleIDCount is the element count. If include is nonzero the filter
// includes only matching apps; if zero it excludes them. If bundleIDCount
// is 0 the filter is equivalent to sck_capture_start (capture everything).
int sck_capture_start_filtered(sck_capture_t* c,
                               const char** bundleIDs,
                               int bundleIDCount,
                               int include);

// sck_capture_stop stops the stream and blocks until the last audio
// callback has returned. Safe to call multiple times.
void sck_capture_stop(sck_capture_t* c);

// sck_capture_destroy releases all resources. Must be called after stop.
void sck_capture_destroy(sck_capture_t* c);

// Returned from sck_capture_start on failure. Values:
//   1 = permission denied (TCC screen recording)
//   2 = no shareable content (unlikely)
//   3 = SCStream init failed
//   4 = SCStream start failed
//   5 = other
int sck_capture_last_error_code(sck_capture_t* c);

#endif
