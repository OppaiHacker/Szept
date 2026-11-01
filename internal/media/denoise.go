package media

import (
	"math"
	"sync/atomic"

	"szept/internal/media/rnnoise"
)

const (
	// gate: a 10 ms block counts as speech above vadOpen; the gate stays open
	// for gateHold blocks after the last one, so word gaps are not chopped.
	vadOpen  = 0.5
	gateHold = 40 // 400 ms

	// AGC: speech is brought to agcTarget RMS (about -18 dBFS), up by at most
	// agcMax (+26 dB, a webcam mic across the desk) or down to agcMin.
	agcTarget = 4100
	agcMax    = 20
	agcMin    = 0.5
	agcLearn  = 0.3   // below the gate: a quiet voice must lift the gain first
	limit     = 30000 // block peak after gain; above it the block is scaled down
)

// VoiceOpts: the voice processing switches (settings, "Głos i wideo"). The
// zero value is the default: everything on, Opus at 32 kbit/s.
type VoiceOpts struct {
	NoDenoise bool `json:"no_denoise,omitempty"`
	NoAGC     bool `json:"no_agc,omitempty"`
	NoGate    bool `json:"no_gate,omitempty"`
	NoMonitor bool `json:"no_monitor,omitempty"` // mic test plays you back
	Bitrate   int  `json:"bitrate,omitempty"`    // bit/s, 0 = opus.Bitrate
}

// voiceOpts is read by the audio thread every 10 ms, so a switch flipped in
// the settings is heard at once, mid-call or mid-test. One mic at a time.
var voiceOpts atomic.Pointer[VoiceOpts]

func SetVoiceOpts(o VoiceOpts) { voiceOpts.Store(&o) }

func CurVoiceOpts() VoiceOpts {
	if o := voiceOpts.Load(); o != nil {
		return *o
	}
	return VoiceOpts{}
}

// denoiser turns raw mic input into denoised 20 ms frames. Frames the gate
// holds closed are not emitted at all.
type denoiser struct {
	rn    *rnnoise.State
	blk   [rnnoise.FrameSize]float32
	n     int
	out   []byte // s16le being collected into a 20 ms frame
	voice bool   // any block of the frame in collection passed the gate
	quiet int    // blocks since the last speech
	gain  float32
	vad   float32 // of the previous block: steers the AGC
}

// learned: the AGC gain of the last mic session (float32 bits), so the next
// test or call starts at your level instead of quiet. 0 = never learned.
var learned atomic.Uint32

func newDenoiser() *denoiser {
	g := math.Float32frombits(learned.Load())
	if g == 0 {
		g = 1
	}
	return &denoiser{rn: rnnoise.New(), quiet: gateHold, gain: g}
}

func (d *denoiser) feed(pcm []byte, emit func([]byte)) {
	for i := 0; i+1 < len(pcm); i += 2 {
		d.blk[d.n] = float32(int16(uint16(pcm[i]) | uint16(pcm[i+1])<<8))
		if d.n++; d.n == rnnoise.FrameSize {
			d.n = 0
			d.block(emit)
		}
	}
}

func (d *denoiser) block(emit func([]byte)) {
	o := CurVoiceOpts()
	if o.NoAGC {
		d.gain = 1
	}
	// gain goes in before rnnoise: its speech detector misses quiet voices
	for i := range d.blk {
		d.blk[i] *= d.gain
	}
	if o.NoDenoise { // rnnoise still scores speech for the gate and AGC
		cp := d.blk
		d.vad = d.rn.Process(&cp)
	} else {
		d.vad = d.rn.Process(&d.blk)
	}
	d.agc(!o.NoAGC)
	if d.vad >= vadOpen || o.NoGate {
		d.quiet = 0
	} else if d.quiet < gateHold {
		d.quiet++
	}
	d.voice = d.voice || d.quiet < gateHold
	for _, v := range d.blk {
		s := clamp16(v)
		d.out = append(d.out, byte(s), byte(uint16(s)>>8))
	}
	if len(d.out) == FrameSamps*2 {
		if d.voice {
			emit(d.out)
			d.out = make([]byte, 0, FrameSamps*2)
		} else {
			d.out = d.out[:0]
		}
		d.voice = false
	}
}

// agc learns the gain from denoised speech blocks only, so silence and noise
// never pump it up. Far below target it climbs fast (+1.2 dB per 10 ms, a
// fresh quiet mic is there in ~0.2 s), near it slowly (+0.26 dB) so speech
// does not pump; down is always fast. A block that would clip is scaled
// under the limit.
func (d *denoiser) agc(learn bool) {
	var sum, peak float64
	for _, v := range d.blk {
		sum += float64(v * v)
		peak = max(peak, math.Abs(float64(v)))
	}
	if peak > limit {
		k := float32(limit / peak)
		for i := range d.blk {
			d.blk[i] *= k
		}
	}
	rms := math.Sqrt(sum / rnnoise.FrameSize)
	if !learn || d.vad < agcLearn || rms < 1 {
		return
	}
	want := float32(float64(d.gain) * agcTarget / rms)
	step := float32(1.03)
	if want > 2*d.gain && d.vad >= vadOpen { // sure it is speech, not noise
		step = 1.15
	}
	if want > d.gain {
		d.gain = min(want, d.gain*step, agcMax)
	} else {
		d.gain = max(want, d.gain*0.8, agcMin)
	}
	learned.Store(math.Float32bits(d.gain))
}
