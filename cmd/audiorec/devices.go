package main

import (
	"fmt"

	"github.com/pmoust/audiorec"
)

func runDevices(args []string) error {
	_ = args // no flags in v1
	devs, err := audiorec.EnumerateMalgoDevices()
	if err != nil {
		return fmt.Errorf("enumerate: %w", err)
	}
	if len(devs) == 0 {
		fmt.Println("(no devices found)")
		return nil
	}
	fmt.Printf("%-8s  %-12s  %s\n", "KIND", "DEFAULT", "NAME")
	for _, d := range devs {
		def := ""
		if d.Default {
			def = "yes"
		}
		fmt.Printf("%-8s  %-12s  %s\n", d.Kind, def, d.Name)
	}
	return nil
}
