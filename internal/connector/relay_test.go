package connector_test

import (
	"crypto/ed25519"
	"net"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"szept/internal/connector"
	"szept/internal/p2p"
)

// Relay-only peers connect through the connector's TURN server, with wire
// obfuscation on, and never hand each other a host or srflx candidate.
func TestRelayOnlyThroughConnectorTURN(t *testing.T) {
	t.Run("both relayed", func(t *testing.T) { relayPair(t, true) })
	// one side direct: it talks to the other's relay address, which must stay
	// out of the obfuscation (bypassRelays)
	t.Run("one relayed", func(t *testing.T) { relayPair(t, false) })
}

func relayPair(t *testing.T, bRelay bool) {
	connector.AllowPrivateHosts.Store(true)
	t.Cleanup(func() { connector.AllowPrivateHosts.Store(false) })

	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(pc.LocalAddr().(*net.UDPAddr).Port)
	pc.Close()

	s := connector.NewServer()
	if err := s.StartTURN("127.0.0.1:"+port, net.ParseIP("127.0.0.1"), "127.0.0.1:"+port, 1<<20); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s)
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")

	aPub, aPriv, _ := ed25519.GenerateKey(nil)
	bPub, bPriv, _ := ed25519.GenerateKey(nil)
	ca, cb := connector.NewClient(url, aPriv), connector.NewClient(url, bPriv)
	ma, mb := p2p.NewManager(aPriv, ca, nil), p2p.NewManager(bPriv, cb, nil)
	ma.SetRelayOnly(true)
	mb.SetRelayOnly(bRelay)
	ma.Accept = func(pub, _ []byte) bool { return string(pub) == string(bPub) }
	mb.Accept = func(pub, _ []byte) bool { return string(pub) == string(aPub) }
	got := make(chan string, 1)
	mb.OnCtrl = func(_ *p2p.Peer, data []byte) { got <- string(data) }
	up := make(chan struct{}, 1)
	ma.OnPeerUp = func(*p2p.Peer) { up <- struct{}{} }

	go ca.Run()
	go cb.Run()
	defer ca.Close()
	defer cb.Close()
	deadline := time.Now().Add(5 * time.Second)
	for ca.ICE() == nil || !cb.Up() {
		if time.Now().After(deadline) {
			t.Fatal("no relay credentials from the connector")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := ma.Connect(bPub); err != nil {
		t.Fatal(err)
	}
	select {
	case <-up:
	case <-time.After(20 * time.Second):
		t.Fatal("relayed link never came up")
	}
	if ma.Peer(bPub).Relay() == "" {
		t.Fatal("relay-only link reports itself as direct")
	}
	if err := ma.Peer(bPub).SendCtrl("hej"); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-got:
		if m != `"hej"` {
			t.Fatalf("got %q", m)
		}
		// the other side must be told too, even when it did not ask for a relay
		if mb.Peer(aPub).Relay() == "" {
			t.Fatal("peer of a relayed user is not told the link is relayed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ctrl message lost on the relay")
	}
}
