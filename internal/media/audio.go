// Package media: capture/playback devices. Voice is raw PCM s16le 16 kHz mono
// in 20 ms frames. Raw PCM ≈ 32 kB/s per stream — swap in Opus
// (hraban/opus) when bandwidth matters.
package media

import (
	"sync"

	"github.com/gen2brain/malgo"
)

const (
	SampleRate = 16000
	FrameSamps = 320 // 20 ms @ 16 kHz
)

var (
	ctxOnce sync.Once
	actx    *malgo.AllocatedContext
	ctxErr  error
)

func audioCtx() (*malgo.AllocatedContext, error) {
	ctxOnce.Do(func() {
		actx, ctxErr = malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	})
	return actx, ctxErr
}

// StartVoice captures the default microphone; cb receives 20 ms PCM frames.
func StartVoice(cb func(pcm []byte)) (stop func(), err error) {
	ctx, err := audioCtx()
	if err != nil {
		return nil, err
	}
	cfg := malgo.DefaultDeviceConfig(malgo.Capture)
	cfg.Capture.Format = malgo.FormatS16
	cfg.Capture.Channels = 1
	cfg.SampleRate = SampleRate
	cfg.PeriodSizeInFrames = FrameSamps

	var buf []byte
	dev, err := malgo.InitDevice(ctx.Context, cfg, malgo.DeviceCallbacks{
		Data: func(_, in []byte, _ uint32) {
			buf = append(buf, in...)
			for len(buf) >= FrameSamps*2 {
				frame := make([]byte, FrameSamps*2)
				copy(frame, buf[:FrameSamps*2])
				buf = buf[FrameSamps*2:]
				cb(frame)
			}
		},
	})
	if err != nil {
		return nil, err
	}
	if err := dev.Start(); err != nil {
		dev.Uninit()
		return nil, err
	}
	return func() { dev.Uninit() }, nil
}

// Player mixes named PCM streams into the default output device.
type Player struct {
	mu   sync.Mutex
	srcs map[string][]int16
	dev  *malgo.Device
}

func StartPlayer() (*Player, error) {
	ctx, err := audioCtx()
	if err != nil {
		return nil, err
	}
	p := &Player{srcs: map[string][]int16{}}
	cfg := malgo.DefaultDeviceConfig(malgo.Playback)
	cfg.Playback.Format = malgo.FormatS16
	cfg.Playback.Channels = 1
	cfg.SampleRate = SampleRate
	cfg.PeriodSizeInFrames = FrameSamps

	dev, err := malgo.InitDevice(ctx.Context, cfg, malgo.DeviceCallbacks{
		Data: func(out, _ []byte, frames uint32) {
			n := int(frames)
			p.mu.Lock()
			for i := 0; i < n; i++ {
				var sum int32
				for k, s := range p.srcs {
					if len(s) > 0 {
						sum += int32(s[0])
						p.srcs[k] = s[1:]
					}
				}
				if sum > 32767 {
					sum = 32767
				} else if sum < -32768 {
					sum = -32768
				}
				out[2*i] = byte(uint16(sum))
				out[2*i+1] = byte(uint16(sum) >> 8)
			}
			p.mu.Unlock()
		},
	})
	if err != nil {
		return nil, err
	}
	if err := dev.Start(); err != nil {
		dev.Uninit()
		return nil, err
	}
	p.dev = dev
	return p, nil
}

// Push queues a PCM frame from a named source (peer). ~400 ms cap per source.
func (p *Player) Push(src string, pcm []byte) {
	samps := make([]int16, len(pcm)/2)
	for i := range samps {
		samps[i] = int16(uint16(pcm[2*i]) | uint16(pcm[2*i+1])<<8)
	}
	p.mu.Lock()
	s := append(p.srcs[src], samps...)
	if max := SampleRate * 2 / 5; len(s) > max { // 400 ms
		s = s[len(s)-max:]
	}
	p.srcs[src] = s
	p.mu.Unlock()
}

func (p *Player) Close() {
	if p.dev != nil {
		p.dev.Uninit()
	}
}
