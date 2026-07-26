package connector

import (
	"crypto/ed25519"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRelay(t *testing.T) {
	srv := httptest.NewServer(NewServer())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")

	_, aPriv, _ := ed25519.GenerateKey(nil)
	_, bPriv, _ := ed25519.GenerateKey(nil)

	got := make(chan []byte, 1)
	a := NewClient(url, aPriv)
	b := NewClient(url, bPriv)
	b.OnSignal = func(from, payload []byte) {
		if string(from) == string(a.Pub) {
			got <- payload
		}
	}
	go a.Run()
	go b.Run()
	defer a.Close()
	defer b.Close()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if on, err := a.Lookup(b.Pub, time.Second); err == nil && on {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("b never came online")
		}
		time.Sleep(50 * time.Millisecond)
	}

	if err := a.Signal(b.Pub, []byte("halo")); err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-got:
		if string(p) != "halo" {
			t.Fatalf("payload: %q", p)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("signal not relayed")
	}
}
