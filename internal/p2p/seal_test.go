package p2p

import (
	"bytes"
	"crypto/ed25519"
	"testing"
)

func TestSealRoundTrip(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	_, other, _ := ed25519.GenerateKey(nil)
	msg := []byte(`{"obf":"tajny klucz"}`)
	blob, err := sealTo(pub, msg)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, []byte("tajny")) {
		t.Fatal("plaintext visible in sealed blob")
	}
	got, err := openFrom(priv, blob)
	if err != nil || !bytes.Equal(got, msg) {
		t.Fatalf("open: %v %q", err, got)
	}
	if _, err := openFrom(other, blob); err == nil {
		t.Fatal("wrong key opened the blob")
	}
	blob[len(blob)-1] ^= 1
	if _, err := openFrom(priv, blob); err == nil {
		t.Fatal("tampered blob opened")
	}
}
