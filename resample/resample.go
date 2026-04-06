// Package resample provides a source.Source wrapper that resamples PCM audio
// to a target sample rate using linear interpolation.
package resample

import (
	"context"
	"encoding/binary"
	"math"

	"github.com/pmoust/audiorec/source"
)

// Wrap returns a Source whose Format().SampleRate is targetRate. If the inner
// source's rate already matches, Wrap returns src directly (zero overhead).
// Otherwise it spawns a goroutine that reads from src.Frames(), resamples each
// chunk, and re-delivers on a new channel.
func Wrap(src source.Source, targetRate int) source.Source {
	if targetRate <= 0 || targetRate == src.Format().SampleRate {
		return src
	}
	return &resamplingSource{
		inner:      src,
		targetRate: targetRate,
	}
}

// resamplingSource implements source.Source by wrapping an inner source and
// resampling its output to a target sample rate.
type resamplingSource struct {
	inner      source.Source
	targetRate int

	format source.Format
	resamp *resampler

	outCh chan source.Frame
}

func (rs *resamplingSource) Format() source.Format {
	return rs.format
}

func (rs *resamplingSource) Start(ctx context.Context) error {
	if err := rs.inner.Start(ctx); err != nil {
		return err
	}

	rs.format = rs.inner.Format()
	rs.format.SampleRate = rs.targetRate

	rs.resamp = newResampler(
		float64(rs.targetRate)/float64(rs.inner.Format().SampleRate),
		rs.inner.Format().Channels,
		rs.inner.Format().BitsPerSample,
		rs.inner.Format().Float,
	)

	rs.outCh = make(chan source.Frame, 32)
	go rs.run(ctx)
	return nil
}

func (rs *resamplingSource) run(ctx context.Context) {
	defer close(rs.outCh)
	for frame := range rs.inner.Frames() {
		outFrame := rs.resamp.process(frame)
		if outFrame.NumFrames > 0 {
			select {
			case <-ctx.Done():
				return
			case rs.outCh <- outFrame:
			}
		}
	}
}

func (rs *resamplingSource) Frames() <-chan source.Frame {
	return rs.outCh
}

func (rs *resamplingSource) Err() error {
	return rs.inner.Err()
}

func (rs *resamplingSource) Close() error {
	return rs.inner.Close()
}

// resampler performs linear interpolation on PCM samples.
// It maintains per-channel fractional position and the previous frame's last sample
// so interpolation can span frame boundaries.
type resampler struct {
	ratio         float64 // targetRate / sourceRate
	channels      int
	bitsPerSample int
	float32       bool
	invRatio      float64 // 1.0 / ratio, i.e., sourceRate / targetRate

	// Per-channel state:
	// pos: fractional position within the cumulative input stream
	// prevLastSample: the last sample from the previous frame (for cross-frame interpolation)
	pos            []float64
	prevLastSample []float64
}

func newResampler(ratio float64, channels, bitsPerSample int, isFloat32 bool) *resampler {
	return &resampler{
		ratio:          ratio,
		channels:       channels,
		bitsPerSample:  bitsPerSample,
		float32:        isFloat32,
		invRatio:       1.0 / ratio,
		pos:            make([]float64, channels),
		prevLastSample: make([]float64, channels),
	}
}

func (r *resampler) process(inFrame source.Frame) source.Frame {
	inN := inFrame.NumFrames
	if inN == 0 {
		return source.Frame{Data: []byte{}, NumFrames: 0, Timestamp: inFrame.Timestamp}
	}

	if r.float32 {
		return r.processFloat32(inFrame)
	}
	return r.processInt16(inFrame)
}

func (r *resampler) processInt16(inFrame source.Frame) source.Frame {
	inData := inFrame.Data
	inN := inFrame.NumFrames
	bytesPerFrame := r.channels * 2

	// Allocate output buffer.
	outMaxN := int(math.Ceil(float64(inN)*r.ratio)) + 1
	outData := make([]byte, outMaxN*bytesPerFrame)
	outPos := 0

	// Generate output samples. pos[ch] is the fractional sample position within the current frame.
	// Negative pos means we're working off the previous frame's state but can't generate yet.
	for outPos < outMaxN {
		canGenerate := false
		for ch := range r.channels {
			// We can generate a sample if we have two consecutive input samples to interpolate.
			// If pos is in range [i, i+1) where i < inN-1, both samples i and i+1 exist.
			// If pos < 0, it refers to the previous frame's last sample, which we have in prevLastSample.
			if (r.pos[ch] < 0 && r.pos[ch]+1.0 >= 0) ||
				(r.pos[ch] >= 0 && r.pos[ch] < float64(inN-1)) {
				canGenerate = true
				break
			}
		}

		if !canGenerate {
			break
		}

		// Generate one output sample across all channels.
		for ch := range r.channels {
			pos := r.pos[ch]

			var s0, s1 float64
			var frac float64

			if pos < 0 && pos+1.0 >= 0 {
				// Interpolate between prevLastSample and inData[0].
				i0 := int(pos)           // will be -1
				frac = pos - float64(i0) // frac = pos - (-1) = pos + 1
				s0 = r.prevLastSample[ch]
				s1 = float64(int16(binary.LittleEndian.Uint16(inData[ch*2:])))
			} else if pos >= 0 && pos < float64(inN-1) {
				// Interpolate within the frame.
				i := int(pos)
				frac = pos - float64(i)
				s0 = float64(int16(binary.LittleEndian.Uint16(inData[(i*r.channels+ch)*2:])))
				s1 = float64(int16(binary.LittleEndian.Uint16(inData[((i+1)*r.channels+ch)*2:])))
			} else {
				// Can't generate; shouldn't reach here.
				continue
			}

			out := s0 + frac*(s1-s0)
			sampleInt := int16(math.Round(out))

			binary.LittleEndian.PutUint16(outData[(outPos*r.channels+ch)*2:], uint16(sampleInt))
		}

		// Advance all channels.
		for ch := range r.channels {
			r.pos[ch] += r.invRatio
		}
		outPos++
	}

	// Save the last sample of this frame for use in the next frame's cross-boundary interpolation.
	if inN > 0 {
		for ch := range r.channels {
			r.prevLastSample[ch] = float64(int16(binary.LittleEndian.Uint16(inData[((inN-1)*r.channels+ch)*2:])))
		}
		// Adjust positions: subtract inN so that in the next frame, pos=0 refers to the first sample.
		for ch := range r.channels {
			r.pos[ch] -= float64(inN)
		}
	}

	return source.Frame{
		Data:      outData[:outPos*bytesPerFrame],
		NumFrames: outPos,
		Timestamp: inFrame.Timestamp,
	}
}

func (r *resampler) processFloat32(inFrame source.Frame) source.Frame {
	inData := inFrame.Data
	inN := inFrame.NumFrames
	bytesPerFrame := r.channels * 4

	// Allocate output buffer.
	outMaxN := int(math.Ceil(float64(inN)*r.ratio)) + 1
	outData := make([]byte, outMaxN*bytesPerFrame)
	outPos := 0

	// Generate output samples.
	for outPos < outMaxN {
		canGenerate := false
		for ch := range r.channels {
			// We can generate a sample if we have two consecutive input samples to interpolate.
			if (r.pos[ch] < 0 && r.pos[ch]+1.0 >= 0) ||
				(r.pos[ch] >= 0 && r.pos[ch] < float64(inN-1)) {
				canGenerate = true
				break
			}
		}

		if !canGenerate {
			break
		}

		// Generate one output sample across all channels.
		for ch := range r.channels {
			pos := r.pos[ch]

			var s0, s1 float64
			var frac float64

			if pos < 0 && pos+1.0 >= 0 {
				// Interpolate between prevLastSample and inData[0].
				i0 := int(pos)           // will be -1
				frac = pos - float64(i0) // frac = pos - (-1) = pos + 1
				s0 = r.prevLastSample[ch]
				s1 = float64(math.Float32frombits(binary.LittleEndian.Uint32(inData[ch*4:])))
			} else if pos >= 0 && pos < float64(inN-1) {
				// Interpolate within the frame.
				i := int(pos)
				frac = pos - float64(i)
				s0 = float64(math.Float32frombits(binary.LittleEndian.Uint32(inData[(i*r.channels+ch)*4:])))
				s1 = float64(math.Float32frombits(binary.LittleEndian.Uint32(inData[((i+1)*r.channels+ch)*4:])))
			} else {
				// Can't generate; shouldn't reach here.
				continue
			}

			out := float32(s0 + frac*(s1-s0))

			binary.LittleEndian.PutUint32(outData[(outPos*r.channels+ch)*4:], math.Float32bits(out))
		}

		// Advance all channels.
		for ch := range r.channels {
			r.pos[ch] += r.invRatio
		}
		outPos++
	}

	// Save the last sample of this frame for use in the next frame's cross-boundary interpolation.
	if inN > 0 {
		for ch := range r.channels {
			r.prevLastSample[ch] = float64(math.Float32frombits(binary.LittleEndian.Uint32(inData[((inN-1)*r.channels+ch)*4:])))
		}
		// Adjust positions.
		for ch := range r.channels {
			r.pos[ch] -= float64(inN)
		}
	}

	return source.Frame{
		Data:      outData[:outPos*bytesPerFrame],
		NumFrames: outPos,
		Timestamp: inFrame.Timestamp,
	}
}
