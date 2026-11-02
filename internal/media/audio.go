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
			return d.ID.Pointer()
		}
	}
	return nil
}

// StartVoice captures the microphone dev ("" = default); cb receives 20 ms
// PCM frames, denoised, leveled and compressed.
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
	return func() { d.Uninit(); dn.rn.Close() }, nil
}

const (
	itdMaxDelay = 24 // up to 0.5 ms delay line for interaural time difference (ITD)
)

// voiceSrc: one remote speaker in the mix.
type voiceSrc struct {
	buf      []int16
	gain     float32 // 0..2, default 1
	pan      float32 // -1 (left) .. +1 (right)
	mute     bool
	live     bool
	peak     float32
	at       time.Time
	rxComp   *lookaheadCompressor
	rxLim    *peakLimiter
	itdBufL  [itdMaxDelay]float32
	itdPosL  int
	itdBufR  [itdMaxDelay]float32
	itdPosR  int
	shadowL  *headShadowLPF
	shadowR  *headShadowLPF
}

func newVoiceSrc() *voiceSrc {
	// RX protect preset: -14 dBFS threshold, 4:1 ratio, 3 ms attack, 120 ms release, 0 dB makeup
	p := CompParams{
		ThresholdDB: -14.0,
		Ratio:       4.0,
		KneeDB:      6.0,
		AttackMS:    3.0,
		ReleaseMS:   120.0,
		MakeupDB:    0.0,
	}
	return &voiceSrc{
		gain:    1,
		rxComp:  newLookaheadCompressor(p),
		rxLim:   newPeakLimiter(-1.5),
		shadowL: newHeadShadowLPF(),
		shadowR: newHeadShadowLPF(),
	}
}

// Player mixes named PCM streams into the default stereo output device.
type Player struct {
	mu        sync.Mutex
	srcs      map[string]*voiceSrc
	dev       *malgo.Device
	master    float32
	deaf      bool
	masterLim [2]*peakLimiter // master limiter for L and R channels
}

func StartPlayer(dev string) (*Player, error) {
	ctx, err := audioCtx()
	if err != nil {
		return nil, err
	}
	p := &Player{
		srcs:      map[string]*voiceSrc{},
		master:    1,
		masterLim: [2]*peakLimiter{newPeakLimiter(-1.0), newPeakLimiter(-1.0)},
	}
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

// mix fills out (interleaved s16le stereo) with n frames.
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

	opts := CurVoiceOpts()
	rxProtect := !opts.NoRXProtect

	for i := 0; i < n; i++ {
		var sumL, sumR float32
		for _, s := range p.srcs {
			if !s.live || i >= len(s.buf) {
				continue
			}
			if s.mute {
				continue
			}
			v := float32(s.buf[i])

			// 1. RX hearing protection per-source
			if rxProtect {
				v = s.rxComp.processSample(v)
				v = s.rxLim.processSample(v)
			}

			// 2. Constant-Power Panning (equal energy)
			// theta = pi/4 * (pan + 1), pan in [-1, +1]
			// at pan=0, theta=pi/4 -> cos(pi/4)*sqrt(2) = 1.0, sin(pi/4)*sqrt(2) = 1.0
			theta := (float64(s.pan) + 1.0) * (math.Pi / 4.0)
			panL := float32(math.Cos(theta) * math.Sqrt2)
			panR := float32(math.Sin(theta) * math.Sqrt2)

			sample := v * s.gain
			sampL := sample * panL
			sampR := sample * panR

			// 3. Binaural ITD (Interaural Time Difference up to ~0.4 ms = 19 samples) & Head Shadow
			itdSamples := int(math.Abs(float64(s.pan)) * 19.0)
			if itdSamples >= itdMaxDelay {
				itdSamples = itdMaxDelay - 1
			}

			if s.pan > 0 { // panned to right: left ear is shadowed and delayed
				s.itdBufL[s.itdPosL] = sampL
				readIdx := (s.itdPosL - itdSamples + itdMaxDelay) % itdMaxDelay
				s.itdPosL = (s.itdPosL + 1) % itdMaxDelay
				sampL = s.shadowL.process(s.itdBufL[readIdx], s.pan)
			} else if s.pan < 0 { // panned to left: right ear is shadowed and delayed
				s.itdBufR[s.itdPosR] = sampR
				readIdx := (s.itdPosR - itdSamples + itdMaxDelay) % itdMaxDelay
				s.itdPosR = (s.itdPosR + 1) % itdMaxDelay
				sampR = s.shadowR.process(s.itdBufR[readIdx], -s.pan)
			}

			sumL += sampL
			sumR += sampR
		}

		// Master volume
		finalL := sumL * master
		finalR := sumR * master

		// 4. Master Bus Peak Limiter (-1.0 dBFS)
		if p.masterLim[0] != nil {
			finalL = p.masterLim[0].processSample(finalL)
			finalR = p.masterLim[1].processSample(finalR)
		}

		l := clamp16(finalL)
		r := clamp16(finalR)
		out[4*i] = byte(uint16(l))
		out[4*i+1] = byte(uint16(l) >> 8)
		out[4*i+2] = byte(uint16(r))
		out[4*i+3] = byte(uint16(r) >> 8)
	}

	for _, s := range p.srcs {
		if s.live {
			s.buf = s.buf[:copy(s.buf, s.buf[min(n, len(s.buf)):])]
		}
	}
}

// prebuf: audio a source must have queued before it (re)starts
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
		s = newVoiceSrc()
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
