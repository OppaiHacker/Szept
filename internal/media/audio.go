// Package media: capture/playback devices. Voice is raw PCM s16le 16 kHz mono
// in 20 ms frames. ponytail: raw PCM ≈ 32 kB/s per stream — swap in Opus
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

// voiceSrc: one remote speaker in the mix. Gain/pan survive an empty buffer,
// so spatial settings applied before the first frame still stick.
type voiceSrc struct {
	buf  []int16
	gain float32 // 0..2, default 1
	pan  float32 // -1 (left) .. +1 (right)
	mute bool
}

// Player mixes named PCM streams into the default stereo output device.
// Each source has its own gain/pan/mute; master scales everything; deafen
// silences the mix without losing the volume setting.
type Player struct {
	mu     sync.Mutex
	srcs   map[string]*voiceSrc
	dev    *malgo.Device
	master float32
	deaf   bool
}

func StartPlayer() (*Player, error) {
	ctx, err := audioCtx()
	if err != nil {
		return nil, err
	}
	p := &Player{srcs: map[string]*voiceSrc{}, master: 1}
	cfg := malgo.DefaultDeviceConfig(malgo.Playback)
	cfg.Playback.Format = malgo.FormatS16
	cfg.Playback.Channels = 2
	cfg.SampleRate = SampleRate
	cfg.PeriodSizeInFrames = FrameSamps

	dev, err := malgo.InitDevice(ctx.Context, cfg, malgo.DeviceCallbacks{
		Data: func(out, _ []byte, frames uint32) {
			p.mix(out, int(frames))
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

// mix fills out (interleaved s16le stereo) with n frames. Pure of any device
// state, so tests can call it directly.
func (p *Player) mix(out []byte, n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	master := p.master
	if p.deaf {
		master = 0
	}
	for i := 0; i < n; i++ {
		var sumL, sumR float32
		for _, s := range p.srcs {
			if len(s.buf) == 0 {
				continue
			}
			v := float32(s.buf[0])
			s.buf = s.buf[1:]
			if s.mute {
				continue
			}
			// stała moc: pan przesuwa między kanałami, środek gra pełną głośnością
			l, r := float32(1), float32(1)
			if s.pan > 0 {
				l = 1 - s.pan
			} else if s.pan < 0 {
				r = 1 + s.pan
			}
			sumL += v * s.gain * l
			sumR += v * s.gain * r
		}
		l := clamp16(sumL * master)
		r := clamp16(sumR * master)
		out[4*i] = byte(uint16(l))
		out[4*i+1] = byte(uint16(l) >> 8)
		out[4*i+2] = byte(uint16(r))
		out[4*i+3] = byte(uint16(r) >> 8)
	}
}

func clamp16(v float32) int16 {
	if v > 32767 {
		return 32767
	}
	if v < -32768 {
		return -32768
	}
	return int16(v)
}

func (p *Player) src(name string) *voiceSrc {
	s := p.srcs[name]
	if s == nil {
		s = &voiceSrc{gain: 1}
		p.srcs[name] = s
	}
	return s
}

// Push queues a PCM frame from a named source (peer). ~400 ms cap per source.
func (p *Player) Push(src string, pcm []byte) {
	samps := make([]int16, len(pcm)/2)
	for i := range samps {
		samps[i] = int16(uint16(pcm[2*i]) | uint16(pcm[2*i+1])<<8)
	}
	p.mu.Lock()
	s := p.src(src)
	s.buf = append(s.buf, samps...)
	if max := SampleRate * 2 / 5; len(s.buf) > max { // 400 ms
		s.buf = s.buf[len(s.buf)-max:]
	}
	p.mu.Unlock()
}

// SetSource: spatial mix for one speaker. gain 0..2, pan -1..1.
func (p *Player) SetSource(name string, gain, pan float32) {
	p.mu.Lock()
	s := p.src(name)
	s.gain = clampF(gain, 0, 2)
	s.pan = clampF(pan, -1, 1)
	p.mu.Unlock()
}

func (p *Player) MuteSource(name string, mute bool) {
	p.mu.Lock()
	p.src(name).mute = mute
	p.mu.Unlock()
}

func (p *Player) SetMaster(gain float32) {
	p.mu.Lock()
	p.master = clampF(gain, 0, 2)
	p.mu.Unlock()
}

func (p *Player) SetDeafen(d bool) {
	p.mu.Lock()
	p.deaf = d
	p.mu.Unlock()
}

func clampF(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (p *Player) Close() {
	if p.dev != nil {
		p.dev.Uninit()
	}
}
