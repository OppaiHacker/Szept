package media

import (
	"testing"
	"time"

	"szept/internal/media/opus"
)

// Late packets are dropped, a short gap is filled, a restarted counter (a
// rejoin) is taken as a new stream.
func TestReceiver(t *testing.T) {
	enc, err := opus.NewEncoder()
	if err != nil {
		t.Fatal(err)
	}
	pkt, _ := enc.Encode(make([]byte, FrameSamps*2))
	r := NewReceiver()
	frames := func(seq uint32) int { return len(r.Decode("a", seq, pkt)) / (FrameSamps * 2) }
	for _, c := range []struct {
		seq  uint32
		want int
	}{
		{10, 1}, {11, 1},
		{11, 0}, {9, 0}, // duplicate, late
		{14, 3}, // 12, 13 lost: PLC + FEC + 14
		{30, 1}, // long gap: no concealment flood
		{1, 1},  // counter restarted
	} {
		if got := frames(c.seq); got != c.want {
			t.Fatalf("seq %d: %d frames, want %d", c.seq, got, c.want)
		}
	}
}

// Every 10th packet lost: the window reports about 10%.
func TestReceiverLoss(t *testing.T) {
	enc, err := opus.NewEncoder()
	if err != nil {
		t.Fatal(err)
	}
	pkt, _ := enc.Encode(make([]byte, FrameSamps*2))
	r := NewReceiver()
	for seq := uint32(1); seq <= 200; seq++ {
		if seq%10 == 0 {
			continue
		}
		r.Decode("a", seq, pkt)
	}
	r.srcs["a"].since = time.Now().Add(-lossWindow - time.Second)
	r.Decode("a", 201, pkt)
	if l := r.LossReport()["a"]; l < 8 || l > 12 {
		t.Fatalf("loss %d%%, want ~10", l)
	}
}
