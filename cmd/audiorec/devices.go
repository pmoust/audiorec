package main

import (
	"fmt"
	"runtime"

	"github.com/pmoust/audiorec"
)

func runDevices(args []string) error {
	_ = args
	devs, err := audiorec.EnumerateMalgoDevices()
	if err != nil {
		return fmt.Errorf("enumerate: %w", err)
	}
	fmt.Printf("%-8s  %-12s  %s\n", "KIND", "DEFAULT", "NAME")
	for _, d := range devs {
		def := ""
		if d.Default {
			def = "yes"
		}
		fmt.Printf("%-8s  %-12s  %s\n", d.Kind, def, d.Name)
	}
	// On macOS, system audio is captured via ScreenCaptureKit, not malgo.
	// Show a synthetic entry so users know it's available.
	if runtime.GOOS == "darwin" {
		fmt.Printf("%-8s  %-12s  %s\n", "system", "yes", "System Audio (ScreenCaptureKit)")
	}
	if len(devs) == 0 && runtime.GOOS != "darwin" {
		fmt.Println("(no devices found)")
	}
	return nil
}
