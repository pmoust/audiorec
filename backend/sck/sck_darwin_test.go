//go:build darwin

package sck

import (
	"errors"
	"testing"

	"github.com/pmoust/audiorec/source"
)

func TestMapSCKError(t *testing.T) {
	cases := []struct {
		code int
		want error
		name string
	}{
		{1, source.ErrPermissionDenied, "permission"},
		{2, source.ErrDeviceNotFound, "no shareable content"},
		{3, source.ErrBackendFailure, "init failed"},
		{4, source.ErrBackendFailure, "start failed"},
		{99, source.ErrBackendFailure, "unknown code default"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := mapSCKError(c.code)
			if got == nil {
				t.Fatalf("mapSCKError(%d) returned nil", c.code)
			}
			if !errors.Is(got, c.want) {
				t.Errorf("mapSCKError(%d) = %v; errors.Is(%v) = false", c.code, got, c.want)
			}
		})
	}
}

func TestSCKRegistry(t *testing.T) {
	// Ensure a clean registry baseline: no assumptions about nextID since
	// other tests may have run before; we only verify relative uniqueness.
	c1 := &Capture{}
	c2 := &Capture{}

	id1 := register(c1)
	id2 := register(c2)

	if id1 == id2 {
		t.Errorf("register returned duplicate ids: %d", id1)
	}
	if got := lookup(id1); got != c1 {
		t.Errorf("lookup(id1) = %v; want %v", got, c1)
	}
	if got := lookup(id2); got != c2 {
		t.Errorf("lookup(id2) = %v; want %v", got, c2)
	}

	unregister(id1)
	if got := lookup(id1); got != nil {
		t.Errorf("lookup(id1) after unregister = %v; want nil", got)
	}
	// id2 should still resolve.
	if got := lookup(id2); got != c2 {
		t.Errorf("lookup(id2) after unregister(id1) = %v; want %v", got, c2)
	}
	unregister(id2)
}

func TestNewSystemAudioWithConfig(t *testing.T) {
	// Test that NewSystemAudioWithConfig stores the config correctly.
	cases := []struct {
		name   string
		config SystemAudioConfig
	}{
		{
			name:   "empty config",
			config: SystemAudioConfig{},
		},
		{
			name: "include single bundle",
			config: SystemAudioConfig{
				IncludeBundleIDs: []string{"com.apple.Safari"},
			},
		},
		{
			name: "exclude multiple bundles",
			config: SystemAudioConfig{
				ExcludeBundleIDs: []string{"com.apple.Terminal", "com.apple.Music"},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cap := NewSystemAudioWithConfig(c.config)
			if cap.config.IncludeBundleIDs != nil {
				if len(cap.config.IncludeBundleIDs) != len(c.config.IncludeBundleIDs) {
					t.Errorf("IncludeBundleIDs length mismatch: got %d, want %d",
						len(cap.config.IncludeBundleIDs), len(c.config.IncludeBundleIDs))
				}
				for i, s := range cap.config.IncludeBundleIDs {
					if s != c.config.IncludeBundleIDs[i] {
						t.Errorf("IncludeBundleIDs[%d] = %q; want %q", i, s, c.config.IncludeBundleIDs[i])
					}
				}
			}
			if cap.config.ExcludeBundleIDs != nil {
				if len(cap.config.ExcludeBundleIDs) != len(c.config.ExcludeBundleIDs) {
					t.Errorf("ExcludeBundleIDs length mismatch: got %d, want %d",
						len(cap.config.ExcludeBundleIDs), len(c.config.ExcludeBundleIDs))
				}
				for i, s := range cap.config.ExcludeBundleIDs {
					if s != c.config.ExcludeBundleIDs[i] {
						t.Errorf("ExcludeBundleIDs[%d] = %q; want %q", i, s, c.config.ExcludeBundleIDs[i])
					}
				}
			}
		})
	}
}
