package p2p

import (
	"crypto/ed25519"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

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
	ma.Accept = func(pub, _ []byte) bool { return string(pub) == string(mb.pub) }
	mb.Accept = func(pub, _ []byte) bool { return string(pub) == string(ma.pub) }

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

// A superseded or never-up PeerConnection closing must not report the peer
// down: core would drop a still-connected peer from its voice calls.
func TestStaleLinkCloseKeepsPeer(t *testing.T) {
	m := &Manager{peers: map[string]*Peer{}, pending: map[string][]byte{},
		dialing: map[string]*Peer{}, answering: map[string]*webrtc.PeerConnection{}}
	down := make(chan struct{}, 2)
	m.OnPeerDown = func([]byte) { down <- struct{}{} }
	pub := []byte("peer")
	mk := func() *Peer {
		pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			t.Fatal(err)
		}
		return m.newPeer(pub, pc)
	}
	old, cur := mk(), mk()
	m.peers[string(pub)] = cur
	old.Close()
	select {
	case <-down:
		t.Fatal("stale link reported the peer down")
	case <-time.After(300 * time.Millisecond):
	}
	if m.Peer(pub) != cur {
		t.Fatal("stale link removed the live peer")
	}
	cur.Close()
	select {
	case <-down:
	case <-time.After(2 * time.Second):
		t.Fatal("live link down not reported")
	}
}
