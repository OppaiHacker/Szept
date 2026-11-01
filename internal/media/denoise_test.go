package media

import (
	"math"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gen2brain/malgo"
)

func pcm48(f func(i int) float64, n int) []byte {
	b := make([]byte, 2*n)
	for i := 0; i < n; i++ {
		s := clamp16(float32(f(i)))
		b[2*i], b[2*i+1] = byte(s), byte(uint16(s)>>8)
	}
	return b
}

// Steady fan-like (brown) noise is gated: nothing goes out. White noise is a
// poor stand-in, rnnoise half-takes it for speech.
func TestDenoiserGatesNoise(t *testing.T) {
	d := newDenoiser()
	defer d.rn.Close()
	r := rand.New(rand.NewSource(1))
	var y float64
	brown := func(int) float64 { y = 0.95*y + r.NormFloat64()*300; return y }
	sent := 0
	d.feed(pcm48(brown, SampleRate*30), func([]byte) { sent++ })
	if d.gain > 2 {
		t.Fatalf("30 s of noise pumped the gain to %.1f", d.gain)
	}
	if sent > 5 {
		t.Fatalf("noise sent %d frames, want ~0", sent)
	}
}

// A quiet voice is lifted toward agcTarget, silence never moves the gain,
// and a loud block is limited instead of clipping.
func TestAGC(t *testing.T) {
	defer learned.Store(0)
	d := &denoiser{gain: 1}
	fill := func(amp float32) {
		for i := range d.blk {
			d.blk[i] = amp * d.gain * float32(math.Sin(float64(i)/5))
		}
	}
	d.vad = 0
	for range 100 {
		fill(300)
		d.agc(true)
	}
	if d.gain != 1 {
		t.Fatalf("silence moved gain to %.2f", d.gain)
	}
	d.vad = 1
	for range 50 { // 0.5 s of speech
		fill(300)
		d.agc(true)
	}
	if d.gain < 15 {
		t.Fatalf("quiet voice gain %.1f, want ~19", d.gain)
	}
	fill(5000) // shout at the learned gain
	d.agc(true)
	for _, v := range d.blk {
		if v > limit || v < -limit {
			t.Fatalf("sample %.0f over the limit", v)
		}
	}
}

// SZEPT_MICDIAG=1 go test -run MicDiag -v ./internal/media/
// Talk for 8 s: per second it logs the raw level, rnnoise's speech score,
// the AGC gain and how much of the second the gate let through.
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
			10*math.Log10(raw/float64(SampleRate)/32768/32768+1e-12), vad/float64(max(blocks, 1)), d.gain, sent*2)
		raw, vad, blocks, sent = 0, 0, 0, 0
		mu.Unlock()
	}
}

// The next mic session starts at the gain the last one learned.
func TestAGCRemembered(t *testing.T) {
	defer learned.Store(0)
	d := &denoiser{gain: 1, vad: 1}
	for range 100 {
		for i := range d.blk {
			d.blk[i] = 300 * d.gain * float32(math.Sin(float64(i)/5))
		}
		d.agc(true)
	}
	n := newDenoiser()
	defer n.rn.Close()
	if n.gain != d.gain || n.gain < 15 {
		t.Fatalf("new session gain %.1f, learned %.1f", n.gain, d.gain)
	}
}
