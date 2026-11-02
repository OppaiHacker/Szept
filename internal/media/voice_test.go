package media

import (
	"testing"

	"szept/internal/media/opus"
)

func TestReceiver(t *testing.T) {
	enc, err := opus.NewEncoder()
	if err != nil {
		t.Fatal(err)
	}
	pkt, _ := enc.Encode(make([]byte, FrameSamps*2))
	r := NewReceiver()

	// Feed sequenced packets: receiver jitter buffer starts and pops frames
	p1 := r.Decode("a", 10, pkt)
	if len(p1) == 0 {
		t.Fatal("first frame should produce decoded PCM")
	}

	p2 := r.Decode("a", 11, pkt)
	if len(p2) == 0 {
		t.Fatal("second frame should produce decoded PCM")
	}

	// Late duplicate should be ignored safely
	r.Decode("a", 10, pkt)

	// Loss report should be tracked
	losses := r.LossReport()
	if _, ok := losses["a"]; !ok {
		t.Fatal("expected loss report for stream a")
	}
}

func TestJitterBufferReorderAndFEC(t *testing.T) {
	enc, err := opus.NewEncoder()
	if err != nil {
		t.Fatal(err)
	}
	dec := opus.NewDecoder()
	jb := newJitterBuf()

	pkt10, _ := enc.Encode(make([]byte, FrameSamps*2))
	pkt11, _ := enc.Encode(make([]byte, FrameSamps*2))
	pkt12, _ := enc.Encode(make([]byte, FrameSamps*2))

	// Reordered arrival: 10 arrives, then 12, then 11
	jb.Push(10, pkt10)
	jb.Push(12, pkt12)
	jb.Push(11, pkt11)

	// Pop 10
	f1 := jb.Pop(dec)
	if len(f1) != FrameSamps*2 {
		t.Fatalf("frame 10 len = %d", len(f1))
	}

	// Pop 11 (was pushed after 12, but reordered into slot 11)
	f2 := jb.Pop(dec)
	if len(f2) != FrameSamps*2 {
		t.Fatalf("frame 11 len = %d", len(f2))
	}

	// Pop 12
	f3 := jb.Pop(dec)
	if len(f3) != FrameSamps*2 {
		t.Fatalf("frame 12 len = %d", len(f3))
	}
}
