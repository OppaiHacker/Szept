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
