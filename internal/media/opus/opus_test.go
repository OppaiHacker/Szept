package opus

import (
	"math"
	"testing"
)

// A 1 kHz tone survives the round trip, packets stay small, and a lost
// packet still yields a full frame (PLC).
func TestRoundTrip(t *testing.T) {
	e, err := NewEncoder()
	if err != nil {
		t.Fatal(err)
	}
	d := NewDecoder()
	pcm := make([]byte, Frame*2)
	var bytes int
	var last []byte
	for k := 0; k < 50; k++ {
		for i := 0; i < Frame; i++ {
			v := int16(8000 * math.Sin(2*math.Pi*1000*float64(k*Frame+i)/Rate))
			pcm[2*i], pcm[2*i+1] = byte(v), byte(uint16(v)>>8)
		}
		pkt, err := e.Encode(pcm)
		if err != nil {
			t.Fatal(err)
		}
		bytes += len(pkt)
		if last, err = d.Decode(pkt, false); err != nil || len(last) != Frame*2 {
			t.Fatalf("decode: %v, %d bytes", err, len(last))
		}
	}
	if kbps := bytes * 8 / 1000; kbps > 60 { // 50 packets = 1 s, default 48k
		t.Fatalf("%d kbit/s", kbps)
	}
	var peak int16
	for i := 0; i < Frame; i++ {
		peak = max(peak, int16(uint16(last[2*i])|uint16(last[2*i+1])<<8))
	}
	if peak < 6000 || peak > 10000 {
		t.Fatalf("tone peak %d, want ~8000", peak)
	}
	if plc, err := d.Decode(nil, false); err != nil || len(plc) != Frame*2 {
		t.Fatalf("plc: %v, %d bytes", err, len(plc))
	}
}

func TestFloatRoundTripAndHighFreq(t *testing.T) {
	e, err := NewEncoder()
	if err != nil {
		t.Fatal(err)
	}
	e.SetBitrate(48000)
	e.SetPacketLoss(5)
	d := NewDecoder()

	// 12 kHz tone at -6 dBFS (0.5 amp)
	pcm := make([]float32, Frame)
	var last []float32
	for k := 0; k < 50; k++ {
		for i := 0; i < Frame; i++ {
			pcm[i] = float32(0.5 * math.Sin(2*math.Pi*12000*float64(k*Frame+i)/Rate))
		}
		pkt, err := e.EncodeFloat(pcm)
		if err != nil {
			t.Fatal(err)
		}
		if last, err = d.DecodeFloat(pkt, false); err != nil || len(last) != Frame {
			t.Fatalf("decode float: %v", err)
		}
	}

	var peak float32
	for _, v := range last {
		if math.Abs(float64(v)) > float64(peak) {
			peak = float32(math.Abs(float64(v)))
		}
	}
	// Verify that high frequency 12 kHz is preserved cleanly (> 0.35 amp)
	if peak < 0.35 {
		t.Fatalf("12 kHz tone peak %f too attenuated, want >= 0.35", peak)
	}

	// Test PLC in float32
	if plc, err := d.DecodeFloat(nil, false); err != nil || len(plc) != Frame {
		t.Fatalf("float plc: %v", err)
	}
}
