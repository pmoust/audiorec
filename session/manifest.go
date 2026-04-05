package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/pmoust/audiorec/source"
)

// ManifestVersion is the current manifest schema version. Bump on any
// breaking schema change.
const ManifestVersion = 1

// Manifest describes a completed recording session: the tracks that ran,
// their formats, per-track timing and byte counters, and any errors. It's
// written as manifest.json alongside the WAV files.
type Manifest struct {
	Version         int             `json:"version"`
	SessionID       string          `json:"session_id"`
	StartedAt       time.Time       `json:"started_at"`
	EndedAt         time.Time       `json:"ended_at"`
	DurationSeconds float64         `json:"duration_seconds"`
	Tracks          []TrackManifest `json:"tracks"`
}

// TrackManifest is the per-track entry in Manifest.Tracks.
type TrackManifest struct {
	Label         string        `json:"label"`
	Path          string        `json:"path"` // relative to the session dir
	Format        source.Format `json:"format"`
	StartedAt     time.Time     `json:"started_at"`
	EndedAt       time.Time     `json:"ended_at"`
	FramesWritten int64         `json:"frames_written"`
	BytesWritten  int64         `json:"bytes_written"`
	Drops         int64         `json:"drops"`
	Error         *string       `json:"error"`
}

// WriteManifestJSON marshals m as pretty-printed JSON and writes it to path
// with a trailing newline. It does NOT create parent directories.
func WriteManifestJSON(path string, m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

// sessionDir returns the common parent directory of every track's Path,
// or "" if the tracks don't share a parent (in which case we skip the
// manifest).
func sessionDir(tracks []Track) string {
	if len(tracks) == 0 {
		return ""
	}
	dir := filepath.Dir(tracks[0].Path)
	for _, t := range tracks[1:] {
		if filepath.Dir(t.Path) != dir {
			return ""
		}
	}
	return dir
}
