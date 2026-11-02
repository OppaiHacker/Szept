package media

import (
	"math"
	"sync/atomic"

	"szept/internal/media/rnnoise"
)

const (
	// gate: a 10 ms block counts as speech above vadOpen
	vadOpen  = 0.5
	gateHold = 35 // 350 ms hold time
)

// VoiceOpts: the voice processing switches (settings, "Głos i wideo").
type VoiceOpts struct {
	NoDenoise   bool    `json:"no_denoise,omitempty"`
	NoAGC       bool    `json:"no_agc,omitempty"`
	NoComp      bool    `json:"no_comp,omitempty"`
	NoRXProtect bool    `json:"no_rx_protect,omitempty"`
	NoGate      bool    `json:"no_gate,omitempty"`
	NoMonitor   bool    `json:"no_monitor,omitempty"`
	Bitrate     int     `json:"bitrate,omitempty"`      // bit/s, 0 = opus.Bitrate (48000)
	CompPreset  string  `json:"comp_preset,omitempty"`  // ""/"natural" | "gentle" | "radio" | "custom"
	CompThresh  float64 `json:"comp_thresh,omitempty"`  // dB, -30..-6
	CompRatio   float64 `json:"comp_ratio,omitempty"`   // 1.5..8.0
	CompAttack  float64 `json:"comp_attack,omitempty"`  // ms, 1..30
	CompRelease float64 `json:"comp_release,omitempty"` // ms, 40..400
	CompMakeup  float64 `json:"comp_makeup,omitempty"`  // dB, 0..12
}

var (
	voiceOpts atomic.Pointer[VoiceOpts]
	micGain   atomic.Uint32 // float32 bits, 0 = 1.0
)

func SetVoiceOpts(o VoiceOpts) { voiceOpts.Store(&o) }

func CurVoiceOpts() VoiceOpts {
	if o := voiceOpts.Load(); o != nil {
		return *o
	}
	return VoiceOpts{}
}

// SetMicGain sets the software gain (0..2, default 1) applied inside the DSP pipeline
// before the compressor and limiter.
func SetMicGain(g float32) {
	if g <= 0 {
		g = 1
	}
	micGain.Store(math.Float32bits(g))
}

func curMicGain() float32 {
	if g := math.Float32frombits(micGain.Load()); g > 0 {
		return g
	}
	return 1
}

// denoiser turns raw mic input into denoised, leveled and compressed 20 ms frames.
type denoiser struct {
	rn     *rnnoise.State
	hpf    *biquadHPF
	gate   *smoothGate
	agcM   *slowAGC
	comp   *lookaheadCompressor
	lim    *peakLimiter
	blk    [rnnoise.FrameSize]float32
	n      int
	out    []byte // s16le being collected into a 20 ms frame
	voice  bool   // frame contains audible speech
	vad    float32
	lastCp string // last applied comp preset/params
}

// learned: the AGC gain of the last mic session, so the next call starts leveled.
var learned atomic.Uint32

func newDenoiser() *denoiser {
	g := math.Float32frombits(learned.Load())
	if g <= 0 {
		g = 1
	}
	d := &denoiser{
		rn:   rnnoise.New(),
		hpf:  newBiquadHPF(),
		gate: newSmoothGate(),
		agcM: newSlowAGC(g),
		comp: newLookaheadCompressor(defaultCompParams("natural")),
		lim:  newPeakLimiter(-1.5),
	}
	return d
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

	// 1. HPF 80 Hz: remove low rumble/DC and breath pops before RNNoise and AGC
	d.hpf.processBlock(d.blk[:])

	// Calculate input RMS before scaling for clean AGC detection
	var sum float64
	for _, v := range d.blk {
		sum += float64(v * v)
	}
	rawRMS := float32(math.Sqrt(sum / float64(rnnoise.FrameSize)))

	// 2. Denoising: RNNoise operates on normalized int16 scale without clipping
	if o.NoDenoise {
		cp := d.blk
		d.vad = d.rn.Process(&cp)
	} else {
		d.vad = d.rn.Process(&d.blk)
	}

	// 3. Smooth Noise Gate with envelope
	d.gate.update(d.vad)
	activeVoice := d.gate.processBlock(d.blk[:])
	if o.NoGate {
		activeVoice = true
	}

	// 4. Slow AGC Leveler (update step + smooth per-sample application)
	d.agcM.update(d.vad, rawRMS, !o.NoAGC)
	if !o.NoAGC {
		d.agcM.processBlock(d.blk[:])
		learned.Store(math.Float32bits(d.agcM.gain))
	}

	// 5. Apply MicGain before compressor & limiter so 200% doesn't clip
	if mg := curMicGain(); mg != 1.0 {
		for i := range d.blk {
			d.blk[i] *= mg
		}
	}

	// 6. Lookahead Soft-Knee Compressor
	if !o.NoComp {
		d.updateCompParams(o)
		d.comp.processBlock(d.blk[:])
	}

	// 7. Peak Limiter (-1.5 dBFS) - always on for hearing safety
	d.lim.processBlock(d.blk[:])

	d.voice = d.voice || activeVoice

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

func (d *denoiser) updateCompParams(o VoiceOpts) {
	key := o.CompPreset
	if key == "custom" {
		p := CompParams{
			ThresholdDB: float32(o.CompThresh),
			Ratio:       float32(o.CompRatio),
			KneeDB:      6.0,
			AttackMS:    float32(o.CompAttack),
			ReleaseMS:   float32(o.CompRelease),
			MakeupDB:    float32(o.CompMakeup),
		}
		if p.ThresholdDB == 0 {
			p.ThresholdDB = -16.0
		}
		if p.Ratio == 0 {
			p.Ratio = 3.5
		}
		if p.AttackMS == 0 {
			p.AttackMS = 4.0
		}
		if p.ReleaseMS == 0 {
			p.ReleaseMS = 140.0
		}
		d.comp.setParams(p)
		return
	}
	if key != d.lastCp {
		d.comp.setParams(defaultCompParams(key))
		d.lastCp = key
	}
}
