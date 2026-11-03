package media

import (
	"math"
)

// biquadHPF: 2nd-order Butterworth high-pass filter (80 Hz @ 48 kHz, Q=1/sqrt(2)).
// Removes DC offset, desk rumble, and mic breath pops before RMS and RNNoise.
type biquadHPF struct {
	b0, b1, b2 float32
	a1, a2     float32
	x1, x2     float32
	y1, y2     float32
}

func newBiquadHPF() *biquadHPF {
	f0 := 80.0
	fs := float64(SampleRate)
	w0 := 2.0 * math.Pi * f0 / fs
	cosw0 := math.Cos(w0)
	sinw0 := math.Sin(w0)
	alpha := sinw0 / (2.0 * math.Sqrt(2)) // Q = 0.7071

	a0 := 1.0 + alpha
	b0 := (1.0 + cosw0) / 2.0 / a0
	b1 := -(1.0 + cosw0) / a0
	b2 := (1.0 + cosw0) / 2.0 / a0
	a1 := (-2.0 * cosw0) / a0
	a2 := (1.0 - alpha) / a0

	return &biquadHPF{
		b0: float32(b0), b1: float32(b1), b2: float32(b2),
		a1: float32(a1), a2: float32(a2),
	}
}

func (f *biquadHPF) process(x float32) float32 {
	y := f.b0*x + f.b1*f.x1 + f.b2*f.x2 - f.a1*f.y1 - f.a2*f.y2
	f.x2 = f.x1
	f.x1 = x
	f.y2 = f.y1
	f.y1 = y
	return y
}

func (f *biquadHPF) processBlock(blk []float32) {
	for i, x := range blk {
		blk[i] = f.process(x)
	}
}

// headShadowLPF: 1-pole low-pass filter for binaural spatial audio (~3.5 kHz cutoff).
// Attenuates high frequencies in the ear shadowed by the head.
type headShadowLPF struct {
	alpha float32
	prev  float32
}

func newHeadShadowLPF() *headShadowLPF {
	fc := 3500.0
	dt := 1.0 / float64(SampleRate)
	rc := 1.0 / (2.0 * math.Pi * fc)
	a := float32(dt / (rc + dt))
	return &headShadowLPF{alpha: a}
}

func (l *headShadowLPF) process(in float32, depth float32) float32 {
	l.prev += l.alpha * (in - l.prev)
	// depth is 0 (direct) to 1 (full shadow)
	return in*(1-depth) + l.prev*depth
}

// smoothGate: soft noise gate with hysteresis and per-sample envelope.
type smoothGate struct {
	state   bool
	hold    int
	env     float32
	atkCoef float32
	relCoef float32
}

func newSmoothGate() *smoothGate {
	// 3 ms attack, 80 ms release
	atk := float32(1.0 - math.Exp(-1.0/(float64(SampleRate)*0.003)))
	rel := float32(1.0 - math.Exp(-1.0/(float64(SampleRate)*0.080)))
	return &smoothGate{
		hold:    0,
		atkCoef: atk,
		relCoef: rel,
	}
}

func (g *smoothGate) update(vad float32) {
	if vad >= vadOpen {
		g.state = true
		g.hold = gateHold
	} else if vad >= 0.35 && g.state {
		g.hold = gateHold
	} else if g.hold > 0 {
		g.hold--
		g.state = true
	} else {
		g.state = false
	}
}

func (g *smoothGate) processBlock(blk []float32) bool {
	target := float32(0.0)
	if g.state {
		target = 1.0
	}
	for i, x := range blk {
		if target > g.env {
			g.env += g.atkCoef * (target - g.env)
		} else {
			g.env += g.relCoef * (target - g.env)
		}
		blk[i] = x * g.env
	}
	return g.state
}

// slowAGC: gentle, musical speech leveler with deadband and bounded rate of change.
type slowAGC struct {
	gain      float32
	smoothedG float32
	targetRMS float32
	minGain   float32
	maxGain   float32
	alphaG    float32
}

func newSlowAGC(initialGain float32) *slowAGC {
	if initialGain <= 0 {
		initialGain = 1.0
	}
	a := float32(1.0 - math.Exp(-1.0/(float64(SampleRate)*0.020)))
	return &slowAGC{
		gain:      initialGain,
		smoothedG: initialGain,
		targetRMS: 3276.7, // -20 dBFS in int16 scale
		minGain:   0.5,    // -6 dB
		maxGain:   8.0,    // +18 dB
		alphaG:    a,
	}
}

func (a *slowAGC) update(vad float32, rms float32, active bool) {
	if !active {
		a.gain = 1.0
		return
	}
	if vad < 0.65 || rms < 80 {
		return
	}

	currentRMS := rms * a.gain
	ratio := a.targetRMS / currentRMS
	dBDiff := 20.0 * math.Log10(float64(ratio))

	if math.Abs(dBDiff) <= 2.5 {
		return
	}

	maxUp := float32(1.0046)
	maxDown := float32(0.9908)

	wantGain := a.gain * float32(ratio)
	if wantGain > a.gain {
		a.gain = min(wantGain, a.gain*maxUp, a.maxGain)
	} else {
		a.gain = max(wantGain, a.gain*maxDown, a.minGain)
	}
}

func (a *slowAGC) processBlock(blk []float32) {
	for i, x := range blk {
		a.smoothedG += a.alphaG * (a.gain - a.smoothedG)
		blk[i] = x * a.smoothedG
	}
}

// CompParams configures the dynamics compressor.
type CompParams struct {
	ThresholdDB float32
	Ratio       float32
	KneeDB      float32
	AttackMS    float32
	ReleaseMS   float32
	MakeupDB    float32
}

func defaultCompParams(preset string) CompParams {
	switch preset {
	case "gentle":
		return CompParams{
			ThresholdDB: -13.0,
			Ratio:       2.2,
			KneeDB:      8.0,
			AttackMS:    8.0,
			ReleaseMS:   180.0,
			MakeupDB:    0.5,
		}
	case "radio":
		return CompParams{
			ThresholdDB: -20.0,
			Ratio:       5.0,
			KneeDB:      6.0,
			AttackMS:    2.5,
			ReleaseMS:   110.0,
			MakeupDB:    3.5,
		}
	case "natural", "":
		fallthrough
	default:
		return CompParams{
			ThresholdDB: -16.0,
			Ratio:       3.5,
			KneeDB:      6.0,
			AttackMS:    4.0,
			ReleaseMS:   140.0,
			MakeupDB:    1.5,
		}
	}
}

const lookaheadSamples = 72 // 1.5 ms @ 48 kHz

// lookaheadCompressor: feedforward soft-knee compressor with 1.5 ms lookahead.
type lookaheadCompressor struct {
	buf     [lookaheadSamples]float32
	pos     int
	grDB    float32 // smoothed gain reduction in dB (<= 0)
	params  CompParams
	atkCoef float32
	relCoef float32
	makeup  float32
}

func newLookaheadCompressor(p CompParams) *lookaheadCompressor {
	c := &lookaheadCompressor{}
	c.setParams(p)
	return c
}

func (c *lookaheadCompressor) setParams(p CompParams) {
	if p.Ratio < 1.0 {
		p.Ratio = 1.0
	}
	if p.AttackMS < 0.5 {
		p.AttackMS = 0.5
	}
	if p.ReleaseMS < 10.0 {
		p.ReleaseMS = 10.0
	}
	c.params = p
	c.atkCoef = float32(1.0 - math.Exp(-1.0/(float64(SampleRate)*float64(p.AttackMS)*0.001)))
	c.relCoef = float32(1.0 - math.Exp(-1.0/(float64(SampleRate)*float64(p.ReleaseMS)*0.001)))
	c.makeup = float32(math.Pow(10.0, float64(p.MakeupDB)/20.0))
}

func (c *lookaheadCompressor) computeTargetGR(inputDB float32) float32 {
	t := c.params.ThresholdDB
	k := c.params.KneeDB
	r := c.params.Ratio

	if k <= 0 {
		if inputDB <= t {
			return 0
		}
		return (inputDB - t) * (1.0/r - 1.0)
	}

	halfK := k * 0.5
	if inputDB <= t-halfK {
		return 0
	}
	if inputDB >= t+halfK {
		return (inputDB - t) * (1.0/r - 1.0)
	}
	diff := inputDB - t + halfK
	return (1.0/r - 1.0) * (diff * diff) / (2.0 * k)
}

func (c *lookaheadCompressor) processSample(x float32) float32 {
	delayed := c.buf[c.pos]
	c.buf[c.pos] = x
	c.pos = (c.pos + 1) % lookaheadSamples

	// Sidechain sees future sample x
	mag := float32(math.Abs(float64(x)))
	var inDB float32 = -96.0
	if mag > 1e-4 {
		inDB = float32(20.0 * math.Log10(float64(mag)/32768.0))
	}

	targetGR := c.computeTargetGR(inDB) // <= 0 dB

	// Smooth the gain reduction: attack pulls gain DOWN (targetGR < grDB)
	if targetGR < c.grDB {
		c.grDB += c.atkCoef * (targetGR - c.grDB)
	} else {
		c.grDB += c.relCoef * (targetGR - c.grDB)
	}

	gain := float32(math.Pow(10.0, float64(c.grDB)/20.0)) * c.makeup
	return delayed * gain
}

func (c *lookaheadCompressor) processBlock(blk []float32) {
	for i, x := range blk {
		blk[i] = c.processSample(x)
	}
}

const limiterLookahead = 48 // 1.0 ms @ 48 kHz

// peakLimiter: brickwall true-peak limiter with 1 ms lookahead, -1.5 dBFS ceiling, and soft-clipping.
type peakLimiter struct {
	buf     [limiterLookahead]float32
	pos     int
	ceiling float32 // 27500 in int16 scale (-1.5 dBFS)
	gain    float32
	relCoef float32
}

func newPeakLimiter(ceilingDB float32) *peakLimiter {
	ceil := float32(32768.0 * math.Pow(10.0, float64(ceilingDB)/20.0))
	rel := float32(1.0 - math.Exp(-1.0/(float64(SampleRate)*0.060))) // 60 ms release
	return &peakLimiter{
		ceiling: ceil,
		gain:    1.0,
		relCoef: rel,
	}
}

func (l *peakLimiter) processSample(x float32) float32 {
	delayed := l.buf[l.pos]
	l.buf[l.pos] = x
	l.pos = (l.pos + 1) % limiterLookahead

	peak := float32(math.Abs(float64(x)))
	targetGain := float32(1.0)
	if peak > l.ceiling {
		targetGain = l.ceiling / peak
	}

	if targetGain < l.gain {
		l.gain = targetGain // instant attack on lookahead
	} else {
		l.gain += l.relCoef * (targetGain - l.gain)
	}

	out := delayed * l.gain
	return softClip(out, l.ceiling*1.05)
}

func (l *peakLimiter) processBlock(blk []float32) {
	for i, x := range blk {
		blk[i] = l.processSample(x)
	}
}

func softClip(x, limit float32) float32 {
	if x > limit {
		diff := x - limit
		return limit + limit*(1.0-float32(math.Exp(float64(-diff/limit))))*0.5
	}
	if x < -limit {
		diff := -x - limit
		return -(limit + limit*(1.0-float32(math.Exp(float64(-diff/limit))))*0.5)
	}
	return x
}
