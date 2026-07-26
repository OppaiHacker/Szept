package p2p

import (
	"crypto/ed25519"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"szept/internal/connector"
)

func TestP2PHandshakeAndCtrl(t *testing.T) {
	srv := httptest.NewServer(connector.NewServer())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")

	_, aPriv, _ := ed25519.GenerateKey(nil)
	_, bPriv, _ := ed25519.GenerateKey(nil)
	ca := connector.NewClient(url, aPriv)
	cb := connector.NewClient(url, bPriv)

	ma := NewManager(aPriv, ca, nil)
	mb := NewManager(bPriv, cb, nil)
	ma.Accept = func(pub []byte) bool { return string(pub) == string(mb.pub) }
	mb.Accept = func(pub []byte) bool { return string(pub) == string(ma.pub) }

	got := make(chan string, 1)
	mb.OnCtrl = func(p *Peer, data []byte) { got <- string(data) }
	up := make(chan struct{}, 2)
	ma.OnPeerUp = func(p *Peer) { up <- struct{}{} }

	go ca.Run()
	go cb.Run()
	defer ca.Close()
	defer cb.Close()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if on, err := ca.Lookup(mb.pub, time.Second); err == nil && on {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("b not registered")
		}
		time.Sleep(50 * time.Millisecond)
	}

	if err := ma.Connect(mb.pub); err != nil {
		t.Fatal(err)
	}
	select {
	case <-up:
	case <-time.After(15 * time.Second):
		t.Fatal("peer link never came up")
	}
	if err := ma.Peer(mb.pub).SendCtrl(map[string]string{"t": "hello"}); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-got:
		if !strings.Contains(msg, "hello") {
			t.Fatalf("ctrl payload: %s", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ctrl message not delivered")
	}
	ma.Close()
	mb.Close()
}
