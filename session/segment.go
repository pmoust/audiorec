package session

import (
	"fmt"
	"path/filepath"
	"strings"
)

// segmentPath returns the path for the Nth segment of a base track path.
// It splits at the final dot to preserve the extension.
// Example: segmentPath("/tmp/mic.wav", 1) -> "/tmp/mic-001.wav"
// Example: segmentPath("/tmp/mic", 1) -> "/tmp/mic-001"
func segmentPath(base string, segment int) string {
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	return fmt.Sprintf("%s-%03d%s", stem, segment, ext)
}
