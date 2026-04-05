//go:build darwin

#import <Foundation/Foundation.h>
#import <ScreenCaptureKit/ScreenCaptureKit.h>
#import <CoreMedia/CoreMedia.h>
#import "sck_bridge.h"

// Forward declaration of the ObjC helper class.
@class AudiorecSCKDelegate;

struct sck_capture {
    AudiorecSCKDelegate* delegate;
    SCStream* stream;
    int last_error;
    sck_audio_cb cb;
    void* user;
};

@interface AudiorecSCKDelegate : NSObject <SCStreamOutput, SCStreamDelegate>
@property (nonatomic, assign) struct sck_capture* owner;
@end

@implementation AudiorecSCKDelegate

- (void)stream:(SCStream*)stream didOutputSampleBuffer:(CMSampleBufferRef)sampleBuffer
         ofType:(SCStreamOutputType)type {
    if (type != SCStreamOutputTypeAudio || self.owner == NULL || self.owner->cb == NULL) {
        return;
    }
    CMAudioFormatDescriptionRef fmt = CMSampleBufferGetFormatDescription(sampleBuffer);
    if (fmt == NULL) return;
    const AudioStreamBasicDescription* asbd = CMAudioFormatDescriptionGetStreamBasicDescription(fmt);
    if (asbd == NULL) return;

    CMBlockBufferRef block = CMSampleBufferGetDataBuffer(sampleBuffer);
    if (block == NULL) return;

    size_t totalLength = 0;
    char* data = NULL;
    OSStatus s = CMBlockBufferGetDataPointer(block, 0, NULL, &totalLength, &data);
    if (s != kCMBlockBufferNoErr || data == NULL) return;

    int channels = (int)asbd->mChannelsPerFrame;
    int sampleRate = (int)asbd->mSampleRate;
    int bytesPerFrame = channels * (int)sizeof(float);
    int numFrames = bytesPerFrame > 0 ? (int)(totalLength / bytesPerFrame) : 0;

    self.owner->cb((const float*)data, numFrames, channels, sampleRate, self.owner->user);
}

- (void)stream:(SCStream*)stream didStopWithError:(NSError*)error {
    // Error surfaces naturally via the next start attempt or via absence of
    // callbacks. Log for debugging.
    NSLog(@"audiorec sck: stream stopped with error: %@", error);
}

@end

sck_capture_t* sck_capture_create(sck_audio_cb cb, void* user) {
    struct sck_capture* c = calloc(1, sizeof(struct sck_capture));
    c->cb = cb;
    c->user = user;
    c->delegate = [[AudiorecSCKDelegate alloc] init];
    c->delegate.owner = c;
    return c;
}

int sck_capture_start_filtered(sck_capture_t* c,
                               const char** bundleIDs,
                               int bundleIDCount,
                               int include) {
    if (c == NULL) return 5;

    __block NSError* blockErr = nil;
    __block SCShareableContent* content = nil;
    dispatch_semaphore_t sem = dispatch_semaphore_create(0);

    [SCShareableContent getShareableContentWithCompletionHandler:^(SCShareableContent* sc, NSError* err) {
        content = sc;
        blockErr = err;
        dispatch_semaphore_signal(sem);
    }];
    dispatch_semaphore_wait(sem, DISPATCH_TIME_FOREVER);

    if (blockErr != nil || content == nil) {
        if (blockErr != nil &&
            [blockErr.domain isEqualToString:@"com.apple.ScreenCaptureKit.SCStreamErrorDomain"] &&
            blockErr.code == -3801 /* SCStreamErrorUserDeclined */) {
            c->last_error = 1;
        } else {
            c->last_error = 2;
        }
        return c->last_error;
    }

    if (content.displays.count == 0) {
        c->last_error = 2;
        return c->last_error;
    }

    SCDisplay* display = content.displays.firstObject;

    // Resolve bundleIDs → SCRunningApplication instances.
    NSMutableArray<SCRunningApplication*>* matchedApps = [NSMutableArray array];
    for (int i = 0; i < bundleIDCount; i++) {
        NSString* wanted = [NSString stringWithUTF8String:bundleIDs[i]];
        for (SCRunningApplication* app in content.applications) {
            if ([app.bundleIdentifier isEqualToString:wanted]) {
                [matchedApps addObject:app];
                break;
            }
        }
    }

    // Build the filter.
    SCContentFilter* filter;
    if (bundleIDCount == 0) {
        // Default: capture everything on the display.
        filter = [[SCContentFilter alloc] initWithDisplay:display
                                         excludingWindows:@[]];
    } else if (include) {
        filter = [[SCContentFilter alloc] initWithDisplay:display
                                    includingApplications:matchedApps
                                         exceptingWindows:@[]];
    } else {
        filter = [[SCContentFilter alloc] initWithDisplay:display
                                    excludingApplications:matchedApps
                                         exceptingWindows:@[]];
    }

    SCStreamConfiguration* cfg = [[SCStreamConfiguration alloc] init];
    cfg.capturesAudio = YES;
    cfg.excludesCurrentProcessAudio = YES;
    // We don't care about video; set tiny dimensions to minimize work.
    cfg.width = 2;
    cfg.height = 2;
    cfg.minimumFrameInterval = CMTimeMake(1, 1);

    SCStream* stream = [[SCStream alloc] initWithFilter:filter
                                           configuration:cfg
                                                delegate:c->delegate];
    if (stream == nil) {
        c->last_error = 3;
        return c->last_error;
    }
    NSError* addErr = nil;
    BOOL ok = [stream addStreamOutput:c->delegate
                                 type:SCStreamOutputTypeAudio
                   sampleHandlerQueue:dispatch_get_global_queue(QOS_CLASS_USER_INTERACTIVE, 0)
                                error:&addErr];
    if (!ok || addErr != nil) {
        c->last_error = 3;
        return c->last_error;
    }

    __block NSError* startErr = nil;
    dispatch_semaphore_t startSem = dispatch_semaphore_create(0);
    [stream startCaptureWithCompletionHandler:^(NSError* err) {
        startErr = err;
        dispatch_semaphore_signal(startSem);
    }];
    dispatch_semaphore_wait(startSem, DISPATCH_TIME_FOREVER);

    if (startErr != nil) {
        if (startErr.code == -3801) {
            c->last_error = 1; // permission denied
        } else {
            c->last_error = 4;
        }
        return c->last_error;
    }

    c->stream = stream;
    c->last_error = 0;
    return 0;
}

int sck_capture_start(sck_capture_t* c) {
    return sck_capture_start_filtered(c, NULL, 0, 0);
}

void sck_capture_stop(sck_capture_t* c) {
    if (c == NULL || c->stream == NULL) return;
    dispatch_semaphore_t sem = dispatch_semaphore_create(0);
    [c->stream stopCaptureWithCompletionHandler:^(NSError* err) {
        (void)err;
        dispatch_semaphore_signal(sem);
    }];
    dispatch_semaphore_wait(sem, DISPATCH_TIME_FOREVER);
    c->stream = nil;
}

void sck_capture_destroy(sck_capture_t* c) {
    if (c == NULL) return;
    if (c->stream != nil) {
        sck_capture_stop(c);
    }
    c->delegate = nil;
    free(c);
}

int sck_capture_last_error_code(sck_capture_t* c) {
    return c ? c->last_error : 5;
}
