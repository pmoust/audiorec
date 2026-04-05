package session

import (
	"testing"
)

func TestSegmentPath(t *testing.T) {
	cases := []struct {
		base string
		seg  int
		want string
	}{
		{"/tmp/mic.wav", 1, "/tmp/mic-001.wav"},
		{"/tmp/mic.wav", 42, "/tmp/mic-042.wav"},
		{"/tmp/mic.wav", 999, "/tmp/mic-999.wav"},
		{"rec/system.flac", 7, "rec/system-007.flac"},
		{"no-ext", 1, "no-ext-001"},
	}
	for _, c := range cases {
		if got := segmentPath(c.base, c.seg); got != c.want {
			t.Errorf("segmentPath(%q, %d) = %q, want %q", c.base, c.seg, got, c.want)
		}
	}
}
