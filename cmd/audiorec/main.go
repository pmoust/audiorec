// Command audiorec records microphone and system audio as separate WAV
// files on macOS and Linux. See README.md for full usage.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "devices":
		if err := runDevices(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "record":
		if err := runRecord(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `audiorec — record microphone and system audio to WAV

Usage:
  audiorec devices                 List capture devices
  audiorec record [flags]          Record a session

Run "audiorec record -h" for recording flags.`)
}
