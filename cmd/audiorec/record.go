package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/pmoust/audiorec"
)

func runRecord(args []string) error {
	fs := flag.NewFlagSet("record", flag.ContinueOnError)
	var (
		outDir        = fs.String("o", "", "output directory (required)")
		sessionName   = fs.String("session-name", "", "session subdirectory name (default: timestamp)")
		micFlag       = fs.String("mic", "default", `microphone: "default", "none", or a device name`)
		systemFlag    = fs.String("system", "default", `system audio: "default", "none", or a device name`)
		includeApps   = fs.String("include-app", "", "macOS only: comma-separated bundle identifiers to capture audio from (mutually exclusive with --exclude-app)")
		excludeApps   = fs.String("exclude-app", "", "macOS only: comma-separated bundle identifiers to exclude from audio capture (mutually exclusive with --include-app)")
		duration      = fs.Duration("d", 0, "optional hard-stop duration (e.g. 30m); 0 = record until SIGINT")
		flushInterval = fs.Duration("flush-interval", 2*time.Second, "WAV header flush interval")
		verbose       = fs.Bool("v", false, "verbose (debug) logging")
	)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: audiorec record -o DIR [flags]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *outDir == "" {
		fs.Usage()
		return errors.New("-o is required")
	}
	if *micFlag == "none" && *systemFlag == "none" {
		return errors.New("at least one of --mic or --system must be enabled")
	}
	if *includeApps != "" && *excludeApps != "" {
		return errors.New("--include-app and --exclude-app are mutually exclusive")
	}
	if (*includeApps != "" || *excludeApps != "") && runtime.GOOS != "darwin" {
		return errors.New("--include-app and --exclude-app are only supported on macOS")
	}

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	name := *sessionName
	if name == "" {
		name = time.Now().Format("20060102-150405")
	}
	sessDir := filepath.Join(*outDir, name)
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", sessDir, err)
	}

	var tracks []audiorec.Track
	if *micFlag != "none" {
		var cfg audiorec.CaptureConfig
		if *micFlag == "default" || *micFlag == "" {
			cfg = audiorec.CaptureConfig{Channels: 1}
		} else {
			var err error
			cfg, err = audiorec.FindCaptureConfig(*micFlag, 1)
			if err != nil {
				return fmt.Errorf("resolve --mic %q: %w", *micFlag, err)
			}
		}
		mic := audiorec.NewMicCapture(cfg)
		tracks = append(tracks, audiorec.Track{
			Source: mic,
			Path:   filepath.Join(sessDir, "mic.wav"),
			Label:  "mic",
		})
	}
	if *systemFlag != "none" {
		if *systemFlag != "default" && *systemFlag != "" {
			if runtime.GOOS == "darwin" {
				return fmt.Errorf("--system: named devices are not supported on macOS (ScreenCaptureKit always uses system default); pass --system default or --system none")
			}
			// Linux: resolve the named device via malgo.
			cfg, err := audiorec.FindCaptureConfig(*systemFlag, 2)
			if err != nil {
				return fmt.Errorf("resolve --system %q: %w", *systemFlag, err)
			}
			tracks = append(tracks, audiorec.Track{
				Source: audiorec.NewMicCapture(cfg), // malgo capture works for monitor devices too
				Path:   filepath.Join(sessDir, "system.wav"),
				Label:  "system",
			})
		} else {
			// default
			var sys audiorec.Source
			if *includeApps != "" || *excludeApps != "" {
				// Parse comma-separated bundle IDs and create config.
				config := audiorec.SystemAudioConfig{}
				if *includeApps != "" {
					config.IncludeBundleIDs = parseCommaSeparated(*includeApps)
				} else if *excludeApps != "" {
					config.ExcludeBundleIDs = parseCommaSeparated(*excludeApps)
				}
				sys = audiorec.NewSystemAudioCaptureWithConfig(config)
			} else {
				sys = audiorec.NewSystemAudioCapture()
			}
			tracks = append(tracks, audiorec.Track{
				Source: sys,
				Path:   filepath.Join(sessDir, "system.wav"),
				Label:  "system",
			})
		}
	}

	sess, err := audiorec.NewSession(audiorec.SessionConfig{
		Tracks:        tracks,
		FlushInterval: *flushInterval,
		Logger:        logger,
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if *duration > 0 {
		ctx, cancel = context.WithTimeout(ctx, *duration)
		defer cancel()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info("received signal, stopping")
		cancel()
	}()

	logger.Info("recording started", "dir", sessDir, "tracks", len(tracks))
	if err := sess.Run(ctx); err != nil {
		// Classify permission errors for human-friendly hint.
		if errors.Is(err, audiorec.ErrPermissionDenied) {
			fmt.Fprintln(os.Stderr, `
System audio capture requires Screen Recording permission on macOS.
Grant it in System Settings → Privacy & Security → Screen Recording,
then re-run. Microphone-only recording still works without it
(pass --system none).`)
		}
		return err
	}
	logger.Info("recording stopped cleanly", "dir", sessDir)
	return nil
}

// parseCommaSeparated splits a comma-separated string and returns the
// non-empty trimmed parts.
func parseCommaSeparated(s string) []string {
	var result []string
	for _, part := range strings.Split(s, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
