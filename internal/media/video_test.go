package media

import (
	"bufio"
	"bytes"
	"testing"
)

func TestSplitJPEG(t *testing.T) {
	a := []byte{0xFF, 0xD8, 1, 0xD9, 2, 0xFF, 0xD9}
	b := []byte{0xFF, 0xD8, 3, 0xFF, 0xD9}
	stream := append(append(append([]byte{9, 0xD9, 7}, a...), 5, 5), b...)
	var got [][]byte
	splitJPEG(bufio.NewReaderSize(bytes.NewReader(stream), 16), func(f []byte) { got = append(got, f) })
	if len(got) != 2 || !bytes.Equal(got[0], a) || !bytes.Equal(got[1], b) {
		t.Fatalf("%x", got)
	}
}

func TestFitSize(t *testing.T) {
	for _, c := range [][6]int{
		{2560, 1440, 1280, 720, 1280, 720},
		{1920, 1200, 1280, 720, 1152, 720},
		{800, 600, 1280, 720, 800, 600},
		{0, 0, 1280, 720, 1280, 720},
	} {
		if w, h := fitSize(c[0], c[1], c[2], c[3]); w != c[4] || h != c[5] {
			t.Errorf("fitSize(%d,%d) = %d,%d", c[0], c[1], w, h)
		}
	}
}

func TestVideoOptsOr(t *testing.T) {
	def := VideoOpts{FPS: 15, Height: 720, Quality: 60}
	if got := (VideoOpts{}).Or(def); got != def {
		t.Errorf("zero: %+v", got)
	}
	if got := (VideoOpts{FPS: 500, Height: 9000, Quality: 1}).Or(def); got != (VideoOpts{60, 1440, 10}) {
		t.Errorf("clamp: %+v", got)
	}
}
