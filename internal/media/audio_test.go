package media

import (
	"runtime"
	"testing"
)

func pcm(v int16, n int) []byte {
	b := make([]byte, 2*n)
	for i := 0; i < n; i++ {
		b[2*i] = byte(uint16(v))
		b[2*i+1] = byte(uint16(v) >> 8)
	}
	return b
}

func frame(p *Player) (l, r int16) {
	out := make([]byte, 4)
	p.mix(out, 1)
	return int16(uint16(out[0]) | uint16(out[1])<<8),
		int16(uint16(out[2]) | uint16(out[3])<<8)
}

func TestMix(t *testing.T) {
	SetVoiceOpts(VoiceOpts{NoRXProtect: true}) // raw pass-through for exact multiplier tests
	p := &Player{
		srcs:   map[string]*voiceSrc{},
		master: 1,
	}

	// jitter buffer: a lone frame waits until prebuf is queued
	p.Push("a", pcm(1000, 8))
	if l, _ := frame(p); l != 0 {
		t.Fatalf("played before prebuf: %d", l)
	}
	p.Push("a", pcm(1000, prebuf))
	if l, r := frame(p); l != 1000 || r != 1000 {
		t.Fatalf("center: %d/%d", l, r)
	}

	p.SetSource("a", 1, 1) // full pan right (pan = 1.0 -> left=0, right=1414)
	if l, r := frame(p); l != 0 || r < 1400 {
		t.Fatalf("pan right: %d/%d", l, r)
	}

	p.SetSource("a", 0.5, 0)
	if l, _ := frame(p); l != 500 {
		t.Fatalf("gain 0.5: %d", l)
	}

	p.MuteSource("a", true)
	if l, r := frame(p); l != 0 || r != 0 {
		t.Fatalf("mute: %d/%d", l, r)
	}
	p.MuteSource("a", false)

	p.SetMaster(2)
	p.SetSource("a", 1, 0)
	if l, _ := frame(p); l != 2000 {
		t.Fatalf("master 2: %d", l)
	}

	p.SetDeafen(true)
	if l, r := frame(p); l != 0 || r != 0 {
		t.Fatalf("deafen: %d/%d", l, r)
	}

	// mute drains buffer
	p.SetDeafen(false)
	p.MuteSource("a", true)
	for i := 0; i < prebuf+8; i++ {
		frame(p)
	}
	p.MuteSource("a", false)
	if l, _ := frame(p); l != 0 {
		t.Fatalf("buffer drained during mute, got %d", l)
	}
}

func TestBinauralConstantPowerMix(t *testing.T) {
	SetVoiceOpts(VoiceOpts{}) // with RX protect and master limiter
	p, err := StartPlayer("")
	if err != nil {
		// No physical audio device available in test environment is ok
		p = &Player{
			srcs:      map[string]*voiceSrc{},
			master:    1,
			masterLim: [2]*peakLimiter{newPeakLimiter(-1.0), newPeakLimiter(-1.0)},
		}
	} else {
		defer p.Close()
	}

	// 4 loud speakers talking at once
	for _, name := range []string{"a", "b", "c", "d"} {
		p.Push(name, pcm(25000, prebuf+FrameSamps))
	}
	p.SetMaster(2.0)

	// Mix 10 frames and ensure master limiter prevents sample overflow/wrap
	out := make([]byte, FrameSamps*4)
	p.mix(out, FrameSamps)

	for i := 0; i < len(out)/2; i++ {
		val := int16(uint16(out[2*i]) | uint16(out[2*i+1])<<8)
		if val > 32700 || val < -32700 {
			t.Fatalf("master limiter let sample clip: %d", val)
		}
	}
}

// A configured camera name must never turn into an ffmpeg flag, and on Linux a
// camera is always a /dev node.
func TestSafeDevice(t *testing.T) {
	for _, bad := range []string{"-f", "-i /etc/passwd"} {
		if got := safeDevice(bad); got != "" {
			t.Errorf("safeDevice(%q) = %q, chciano odrzucenia", bad, got)
		}
	}
	if runtime.GOOS != "windows" {
		for _, bad := range []string{"/etc/passwd", "video0"} {
			if got := safeDevice(bad); got != "" {
				t.Errorf("safeDevice(%q) = %q, poza /dev/ ma odpaść", bad, got)
			}
		}
		if got := safeDevice("/dev/video1"); got != "/dev/video1" {
			t.Errorf("poprawne urządzenie odrzucone: %q", got)
		}
	}
	if got := safeDevice(""); got != "" {
		t.Errorf("pusta wartość ma zostać pusta, jest %q", got)
	}
}
