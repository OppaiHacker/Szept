// Package media: capture/playback devices. Voice is s16le 48 kHz mono in
// 20 ms frames: denoised by rnnoise (denoise.go), sent as Opus (voice.go).
package media

import (
	"math"
	"sync"
	"time"
	"unsafe"

	"github.com/gen2brain/malgo"

	"szept/internal/media/opus"
	"szept/internal/media/rnnoise"
)

const (
	SampleRate = opus.Rate  // rnnoise needs 48 kHz too
	FrameSamps = opus.Frame // 20 ms
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

// Tones plays a short cue on speaker dev ("" = default): one sine blip per
// [Hz, start s] note, 12 ms attack and an exponential fade, as the old
// WebAudio cues did. Going through miniaudio puts it on the chosen speaker.
func Tones(dev string, notes [][2]float64) error {
	n := SampleRate * 2 / 5 // 400 ms, the player's per-source cap
	mix := make([]float64, n)
	for _, nt := range notes {
		start := int(nt[1] * SampleRate)
		for i := 0; i < SampleRate/5 && start+i < n; i++ { // 200 ms
			t := float64(i) / SampleRate
			env := math.Min(t/0.012, 1) * math.Pow(0.001/0.16, math.Max(0, t-0.012)/0.168)
			mix[start+i] += 0.16 * env * math.Sin(2*math.Pi*nt[0]*t)
		}
	}
	pcm := make([]byte, 2*n)
	for i, v := range mix {
		s := clamp16(float32(v * 32767))
		pcm[2*i], pcm[2*i+1] = byte(s), byte(uint16(s)>>8)
	}
	p, err := StartPlayer(dev)
	if err != nil {
		return err
	}
	p.Push("cue", pcm)
	time.AfterFunc(500*time.Millisecond, p.Close)
	return nil
}

// Device: a microphone or speaker the system offers. ID is what StartVoice
// and StartPlayer take; "" there means the system default.
type Device struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Devices lists the microphones (capture) or speakers (!capture).
func Devices(capture bool) []Device {
	ctx, err := audioCtx()
	if err != nil {
		return nil
	}
	kind := malgo.Playback
	if capture {
		kind = malgo.Capture
	}
	infos, _ := ctx.Devices(kind)
	out := []Device{}
	for _, d := range infos {
		out = append(out, Device{ID: d.ID.String(), Name: d.Name()})
	}
	return out
}

// deviceID finds a device by its ID string; nil (the default) when it is
// gone, e.g. a headset that was unplugged.
func deviceID(ctx *malgo.AllocatedContext, kind malgo.DeviceType, id string) unsafe.Pointer {
	if id == "" {
		return nil
	}
	infos, _ := ctx.Devices(kind)
	for _, d := range infos {
		if d.ID.String() == id {
			// C copy leaks 256 B per call start; free it if calls
			// ever start in a loop
			return d.ID.Pointer()
		}
	}
	return nil
}

// StartVoice captures the microphone dev ("" = default); cb receives 20 ms
// PCM frames, denoised and gated (see denoiser): silence sends nothing.
func StartVoice(dev string, cb func(pcm []byte)) (stop func(), err error) {
	ctx, err := audioCtx()
	if err != nil {
		return nil, err
	}
	cfg := malgo.DefaultDeviceConfig(malgo.Capture)
	cfg.Capture.DeviceID = deviceID(ctx, malgo.Capture, dev)
	cfg.Capture.Format = malgo.FormatS16
	cfg.Capture.Channels = 1
	cfg.SampleRate = SampleRate
	cfg.PeriodSizeInFrames = rnnoise.FrameSize

	dn := newDenoiser()
	d, err := malgo.InitDevice(ctx.Context, cfg, malgo.DeviceCallbacks{
		Data: func(_, in []byte, _ uint32) { dn.feed(in, cb) },
	})
	if err != nil {
		dn.rn.Close()
		return nil, err
	}
	if err := d.Start(); err != nil {
		d.Uninit()
		dn.rn.Close()
		return nil, err
	}
	// Uninit waits for the audio thread, so rnnoise is not freed under it
	return func() { d.Uninit(); dn.rn.Close() }, nil
}

// voiceSrc: one remote speaker in the mix. Gain/pan survive an empty buffer,
// so spatial settings applied before the first frame still stick.
type voiceSrc struct {
	buf  []int16
	gain float32 // 0..2, default 1
	pan  float32 // -1 (left) .. +1 (right)
	mute bool
	live bool      // playing; false after an underrun until prebuf is queued
	peak float32   // 0..1, loudest sample of the last frame
	at   time.Time // when that frame arrived
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

func StartPlayer(dev string) (*Player, error) {
	ctx, err := audioCtx()
	if err != nil {
		return nil, err
	}
	p := &Player{srcs: map[string]*voiceSrc{}, master: 1}
	cfg := malgo.DefaultDeviceConfig(malgo.Playback)
	cfg.Playback.DeviceID = deviceID(ctx, malgo.Playback, dev)
	cfg.Playback.Format = malgo.FormatS16
	cfg.Playback.Channels = 2
	cfg.SampleRate = SampleRate
	cfg.PeriodSizeInFrames = FrameSamps

	d, err := malgo.InitDevice(ctx.Context, cfg, malgo.DeviceCallbacks{
		Data: func(out, _ []byte, frames uint32) {
			p.mix(out, int(frames))
		},
	})
	if err != nil {
		return nil, err
	}
	if err := d.Start(); err != nil {
		d.Uninit()
		return nil, err
	}
	p.dev = d
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
	for _, s := range p.srcs {
		if len(s.buf) == 0 {
			s.live = false
		} else if len(s.buf) >= prebuf {
			s.live = true
		}
	}
	for i := 0; i < n; i++ {
		var sumL, sumR float32
		for _, s := range p.srcs {
			// each source gives one sample per output frame: sample i
			if !s.live || i >= len(s.buf) {
				continue
			}
			v := float32(s.buf[i])
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
	// drop what was played in one copy instead of reslicing per sample, so
	// the buffer keeps one backing array instead of creeping through new ones
	for _, s := range p.srcs {
		if s.live {
			s.buf = s.buf[:copy(s.buf, s.buf[min(n, len(s.buf)):])]
		}
	}
}

// prebuf: audio a source must have queued before it (re)starts, so network
// jitter costs 60 ms of delay instead of a crackle on every late packet.
const prebuf = SampleRate * 6 / 100

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
	s.peak, s.at = Peak(samps), time.Now()
	s.buf = append(s.buf, samps...)
	if max := SampleRate * 2 / 5; len(s.buf) > max { // 400 ms
		s.buf = s.buf[:copy(s.buf, s.buf[len(s.buf)-max:])]
	}
	p.mu.Unlock()
}

// ApplyGain scales s16le PCM in place, clipping at full scale.
func ApplyGain(pcm []byte, g float32) {
	for i := 0; i+1 < len(pcm); i += 2 {
		v := clamp16(float32(int16(uint16(pcm[i])|uint16(pcm[i+1])<<8)) * g)
		pcm[i], pcm[i+1] = byte(v), byte(uint16(v)>>8)
	}
}

// Peak: loudest sample of a frame, 0..1. Drives the "speaking" ring in the UI.
func Peak(samps []int16) float32 {
	var m int32
	for _, v := range samps {
		a := int32(v)
		if a < 0 {
			a = -a
		}
		m = max(m, a)
	}
	return float32(m) / 32768
}

// Levels: current peak per source that sent audio in the last 300 ms.
func (p *Player) Levels() map[string]float32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := map[string]float32{}
	for name, s := range p.srcs {
		if time.Since(s.at) < 300*time.Millisecond && !s.mute && !p.deaf {
			out[name] = s.peak
		}
	}
	return out
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
