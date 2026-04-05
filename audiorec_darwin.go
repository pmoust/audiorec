//go:build darwin

package audiorec

import "github.com/pmoust/audiorec/backend/sck"

func newSystemAudioDefault() Source {
	return sck.NewSystemAudio()
}
