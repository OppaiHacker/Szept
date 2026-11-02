package media

import (
	"math"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gen2brain/malgo"

	"szept/internal/media/rnnoise"
)

func pcm48(f func(i int) float64, n int) []byte {
	b := make([]byte, 2*n)
	for i := 0; i < n; i++ {
		s := clamp16(float32(f(i)))
		b[2*i], b[2*i+1] = byte(s), byte(uint16(s)>>8)
	}
	return b
}

// Steady fan-like (brown) noise is gated: nothing goes out.
func TestDenoiserGatesNoise(t *testing.T) {
	d := newDenoiser()
	defer d.rn.Close()
	r := rand.New(rand.NewSource(1))
	var y float64
	brown := func(int) float64 { y = 0.95*y + r.NormFloat64()*300; return y }
	sent := 0
	d.feed(pcm48(brown, SampleRate*10), func([]byte) { sent++ })
	if d.agcM.gain > 2 {
		t.Fatalf("10 s of noise pumped the gain to %.1f", d.agcM.gain)
	}
	if sent > 5 {
		t.Fatalf("noise sent %d frames, want ~0", sent)
	}
}

// TestTransientNoSpike: after quiet speech when AGC has raised gain, a sudden 20x spike (shout/clap)
// must be caught by lookahead compressor and peak limiter without exceeding -1.5 dBFS (~27500 in s16).
func TestTransientNoSpike(t *testing.T) {
	defer learned.Store(0)
	SetVoiceOpts(VoiceOpts{}) // default: AGC + Natural Compressor + Limiter
	SetMicGain(1.0)

	d := newDenoiser()
	defer d.rn.Close()

	// Train AGC for 1 second on speech-like quiet tone (amplitude 400)
	for k := 0; k < 100; k++ {
		for i := range d.blk {
			d.blk[i] = 400.0 * float32(math.Sin(2.0*math.Pi*500.0*float64(k*rnnoise.FrameSize+i)/float64(SampleRate)))
		}
		d.vad = 0.9 // high VAD speech
		d.block(func([]byte) {})
	}

	if d.agcM.gain <= 1.0 {
		t.Fatalf("AGC gain did not adapt upward, got %.2f", d.agcM.gain)
	}

	// Sudden shout / burst: amplitude jumps 25x to 10000
	maxPeak := float32(0.0)
	for k := 0; k < 10; k++ {
		for i := range d.blk {
			d.blk[i] = 10000.0 * float32(math.Sin(2.0*math.Pi*500.0*float64(k*rnnoise.FrameSize+i)/float64(SampleRate)))
		}
		d.vad = 0.95
		d.block(func(pcm []byte) {
			for i := 0; i+1 < len(pcm); i += 2 {
				val := float32(int16(uint16(pcm[i]) | uint16(pcm[i+1])<<8))
				if abs := float32(math.Abs(float64(val))); abs > maxPeak {
					maxPeak = abs
				}
			}
		})
	}

	// Ceiling -1.5 dBFS is ~27500. Ensure it never pierced through painfully.
	if maxPeak > 28000 {
		t.Fatalf("transient pierced limiter ceiling: max peak was %.0f (want <= 28000)", maxPeak)
	}
}

func TestCompressorPresetsAndCustom(t *testing.T) {
	p := defaultCompParams("radio")
	c := newLookaheadCompressor(p)

	// Feed 200 ms of loud tone (30000 amplitude)
	var maxOut float32
	for i := 0; i < SampleRate/5; i++ {
		in := float32(30000.0 * math.Sin(2.0*math.Pi*400.0*float64(i)/float64(SampleRate)))
		out := c.processSample(in)
		// after attack has engaged (> 20 ms = 960 samples), measure compressed level
		if i > 960 {
			if abs := float32(math.Abs(float64(out))); abs > maxOut {
				maxOut = abs
			}
		}
	}

	// Radio preset (-20 dB thresh, 5:1 ratio, +3.5 dB makeup) brings 30000 (-0.7 dBFS) down significantly (< 18000)
	if maxOut > 18000 {
		t.Fatalf("radio compressor output too high: %.0f, want <= 18000", maxOut)
	}
}

// SZEPT_MICDIAG=1 go test -run MicDiag -v ./internal/media/
func TestMicDiag(t *testing.T) {
	if os.Getenv("SZEPT_MICDIAG") == "" {
		t.Skip("needs a microphone and someone talking")
	}
	ctx, err := audioCtx()
	if err != nil {
		t.Fatal(err)
	}
	cfg := malgo.DefaultDeviceConfig(malgo.Capture)
	cfg.Capture.Format, cfg.Capture.Channels, cfg.SampleRate = malgo.FormatS16, 1, SampleRate
	d := newDenoiser()
	var mu sync.Mutex
	var raw, vad float64
	var blocks, sent int
	dev, err := malgo.InitDevice(ctx.Context, cfg, malgo.DeviceCallbacks{Data: func(_, in []byte, _ uint32) {
		mu.Lock()
		defer mu.Unlock()
		for i := 0; i+1 < len(in); i += 2 {
			v := float64(int16(uint16(in[i]) | uint16(in[i+1])<<8))
			raw += v * v
		}
		d.feed(in, func([]byte) { sent++ })
		vad += float64(d.vad)
		blocks++
	}})
	if err != nil {
		t.Fatal(err)
	}
	dev.Start()
	defer dev.Uninit()
	for range 8 {
		time.Sleep(time.Second)
		mu.Lock()
		t.Logf("raw %5.1f dBFS  speech %.2f  gain %5.1fx  sent %3d%%",
			10*math.Log10(raw/float64(SampleRate)/32768/32768+1e-12), vad/float64(max(blocks, 1)), d.agcM.gain, sent*2)
		raw, vad, blocks, sent = 0, 0, 0, 0
		mu.Unlock()
	}
}
