package p2p

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"

	"szept/internal/crypt"
)

func TestObfRoundTrip(t *testing.T) {
	key := crypt.NewKey()
	for _, pt := range [][]byte{{}, []byte("hi"), bytes.Repeat([]byte{0xAB}, 1200)} {
		w, err := obfSeal(key, pt)
		if err != nil {
			t.Fatal(err)
		}
		ln := obfOpen(key, w)
		if ln != len(pt) {
			t.Fatalf("len %d want %d", ln, len(pt))
		}
		if !bytes.Equal(w[obfNonce+2:obfNonce+2+ln], pt) {
			t.Fatal("plaintext mismatch")
		}
	}
}

// The wire must not leak the STUN magic cookie a DPI box keys on.
func TestObfHidesStunCookie(t *testing.T) {
	key := crypt.NewKey()
	// a plausible STUN binding request: type, len, magic cookie 0x2112A442
	stun := make([]byte, 20)
	binary.BigEndian.PutUint32(stun[4:], 0x2112A442)
	w, _ := obfSeal(key, stun)
	if bytes.Contains(w, []byte{0x21, 0x12, 0xA4, 0x42}) {
		t.Fatal("STUN magic cookie visible on the wire")
	}
	// wrong key -> garbage, never the original
	if ln := obfOpen(crypt.NewKey(), w); ln >= 0 && bytes.Equal(w[obfNonce+2:obfNonce+2+ln], stun) {
		t.Fatal("decrypted with wrong key")
	}
}

func TestStunHostSet(t *testing.T) {
	set := stunHostSet([]string{"stun:stun.l.google.com:19302", "stun:noport", "turns:t.example:443"})
	for _, h := range []string{"stun.l.google.com", "noport", "t.example"} {
		if !set[h] {
			t.Fatalf("missing host %q", h)
		}
	}
}

func TestAddrKeyFoldsV4(t *testing.T) {
	a := &net.UDPAddr{IP: net.ParseIP("::ffff:1.2.3.4"), Port: 5}
	b := &net.UDPAddr{IP: net.ParseIP("1.2.3.4"), Port: 5}
	if addrKey(a) != addrKey(b) {
		t.Fatalf("%s != %s", addrKey(a), addrKey(b))
	}
}
