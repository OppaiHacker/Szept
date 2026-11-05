package main

import "testing"

func TestSpatialAudio(t *testing.T) {
	// przy uchu, na wprost: pełny głos, środek
	if g, p := spatialAudio(0, 0); g != 1 || p != 0 {
		t.Fatalf("center: g=%v p=%v", g, p)
	}
	// pod horyzontem: ledwo słychać, ale nie zero
	if g, _ := spatialAudio(0, 1); g < 0.1 || g > 0.2 {
		t.Fatalf("far gain: %v", g)
	}
	// pełne prawo: pan dodatni, głos tylko przytłumiony
	g, p := spatialAudio(1, 0)
	if p <= 0 || p > 1 || g < 0.4 {
		t.Fatalf("right: g=%v p=%v", g, p)
	}
	// symetria lewa/prawa
	gl, pl := spatialAudio(-0.6, 0.3)
	gr, pr := spatialAudio(0.6, 0.3)
	if gl != gr || pl != -pr {
		t.Fatalf("asymmetric: %v/%v vs %v/%v", gl, pl, gr, pr)
	}
	// głębia monotonicznie wycisza
	prev := float32(2)
	for d := float32(0); d <= 1; d += 0.1 {
		g, _ := spatialAudio(0.2, d)
		if g >= prev {
			t.Fatalf("gain not falling at depth %v", d)
		}
		prev = g
	}
}
