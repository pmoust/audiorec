package malgo

import (
	"testing"

	"github.com/pmoust/audiorec/source"
)

func TestClassifyKind(t *testing.T) {
	cases := map[string]source.Kind{
		"Built-in Microphone":                        source.Mic,
		"alsa_input.pci-0000_00_1f.3":                source.Mic,
		"alsa_output.pci-0000.analog-stereo.monitor": source.SystemAudio,
	}
	for name, want := range cases {
		if got := classifyKind(name); got != want {
			t.Errorf("classifyKind(%q) = %v want %v", name, got, want)
		}
	}
}

// TestEnumerate_Smoke runs actual malgo enumeration. Skipped if no audio
// server is available (CI containers, headless sessions).
func TestEnumerate_Smoke(t *testing.T) {
	devs, err := Enumerate()
	if err != nil {
		t.Skipf("malgo unavailable in this environment: %v", err)
	}
	t.Logf("found %d devices", len(devs))
	for _, d := range devs {
		t.Logf("  %s [%s] default=%v", d.Name, d.Kind, d.Default)
	}
}
