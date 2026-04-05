//go:build darwin

package audiorec

import "github.com/pmoust/audiorec/backend/sck"

func newSystemAudioDefault() Source {
	return sck.NewSystemAudio()
}

func newSystemAudioWithConfig(cfg SystemAudioConfig) Source {
	return sck.NewSystemAudioWithConfig(cfg)
}
