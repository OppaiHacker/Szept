package media

import "testing"

func TestPeak(t *testing.T) {
	if got := Peak([]int16{100, -16384, 200}); got != .5 {
		t.Fatalf("Peak = %v, want .5", got)
	}
	if got := Peak(nil); got != 0 {
		t.Fatalf("Peak(nil) = %v, want 0", got)
	}
}

func TestPlayerLevels(t *testing.T) {
	p := &Player{srcs: map[string]*voiceSrc{}, master: 1}
	p.Push("a", []byte{0x00, 0x40}) // one sample, 16384 = half scale
	if got := p.Levels()["a"]; got != .5 {
		t.Fatalf("level = %v, want .5", got)
	}
	p.MuteSource("a", true)
	if _, ok := p.Levels()["a"]; ok {
		t.Fatal("muted source still reported")
	}
}

func TestApplyGain(t *testing.T) {
	pcm := []byte{0x10, 0x00, 0x00, 0x40, 0x00, 0xc0} // 16, 16384, -16384
	ApplyGain(pcm, 2)
	got := []int16{int16(uint16(pcm[0]) | uint16(pcm[1])<<8), int16(uint16(pcm[2]) | uint16(pcm[3])<<8), int16(uint16(pcm[4]) | uint16(pcm[5])<<8)}
	if got[0] != 32 || got[1] != 32767 || got[2] != -32768 {
		t.Fatalf("got %v", got)
	}
}
