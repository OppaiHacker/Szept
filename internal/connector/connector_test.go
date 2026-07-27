package connector

import (
	"crypto/ed25519"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestPerIPLimit(t *testing.T) {
	s := NewServer()
	for i := 0; i < MaxSessionsPerIP; i++ {
		if !s.takeSlot("9.9.9.9") {
			t.Fatalf("slot %d odrzucony przed limitem", i)
		}
	}
	if s.takeSlot("9.9.9.9") {
		t.Fatal("limit na IP nie zadziałał")
	}
	if !s.takeSlot("9.9.9.8") {
		t.Fatal("inne IP nie ma obrywać za sąsiada")
	}
	s.freeSlot("9.9.9.9")
	if !s.takeSlot("9.9.9.9") {
		t.Fatal("zwolniony slot nie wrócił")
	}

	// loopback (reverse proxy na tym samym hoście) jest zwolniony z limitu
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	if ip := clientIP(r); ip != "" {
		t.Fatalf("loopback ma być zwolniony, dostałem %q", ip)
	}
	r.RemoteAddr = "203.0.113.7:5555"
	if ip := clientIP(r); ip != "203.0.113.7" {
		t.Fatalf("clientIP = %q", ip)
	}
	for i := 0; i < MaxSessionsPerIP+5; i++ {
		if !s.takeSlot("") {
			t.Fatal("zwolnione IP nie ma mieć limitu")
		}
	}
}

// Jedno gniazdo = jeden pubkey (inaczej każda rejestracja zostawia wpis,
// którego rozłączenie nie sprząta), a podsłuchana ramka nie przejmuje slotu.
func TestRegisterBinding(t *testing.T) {
	s := NewServer()
	srv := httptest.NewServer(s)
	defer srv.Close()
	dial := func() *websocket.Conn {
		ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
		if err != nil {
			t.Fatal(err)
		}
		return ws
	}
	register := func(ws *websocket.Conn, pub ed25519.PublicKey, priv ed25519.PrivateKey, ts int64) string {
		if err := ws.WriteJSON(Msg{Op: "register", Pub: pub, TS: ts, Sig: SignRegister(priv, pub, ts)}); err != nil {
			t.Fatal(err)
		}
		var ack Msg
		if err := ws.ReadJSON(&ack); err != nil {
			t.Fatal(err)
		}
		return ack.Op
	}

	pub, priv, _ := ed25519.GenerateKey(nil)
	ts := time.Now().Unix()
	ws := dial()
	defer ws.Close()
	if op := register(ws, pub, priv, ts); op != "ok" {
		t.Fatalf("pierwsza rejestracja: %s", op)
	}
	other, otherPriv, _ := ed25519.GenerateKey(nil)
	if op := register(ws, other, otherPriv, ts); op == "ok" {
		t.Fatal("drugi pubkey na tym samym gnieździe przeszedł")
	}
	// powtórka tej samej podpisanej ramki z innego gniazda nie przejmuje slotu
	replay := dial()
	defer replay.Close()
	if op := register(replay, pub, priv, ts); op == "ok" {
		t.Fatal("odtworzona rejestracja przejęła slot")
	}
	if n := s.Count(); n != 1 {
		t.Fatalf("peers = %d, want 1", n)
	}
}

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

func TestLoadRedirect(t *testing.T) {
	s := NewServer()
	s.SetMaxPeers(1)
	srv := httptest.NewServer(s)
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	_, aPriv, _ := ed25519.GenerateKey(nil)
	_, bPriv, _ := ed25519.GenerateKey(nil)

	a := NewClient(url, aPriv)
	go a.Run()
	defer a.Close()

	deadline := time.Now().Add(5 * time.Second)
	for !a.Up() {
		if time.Now().After(deadline) {
			t.Fatal("client A did not connect")
		}
		time.Sleep(50 * time.Millisecond)
	}

	b := NewClient(url, bPriv)
	go b.Run()
	defer b.Close()

	time.Sleep(500 * time.Millisecond)
	if b.Up() {
		t.Fatal("client B should be rejected due to MaxPeers=1 limit")
	}
}

func TestMeshRelay(t *testing.T) {
	s1 := NewServer()
	srv1 := httptest.NewServer(s1)
	defer srv1.Close()
	url1 := "ws" + strings.TrimPrefix(srv1.URL, "http")
	s1.SetPublicURL(url1)

	s2 := NewServer()
	srv2 := httptest.NewServer(s2)
	defer srv2.Close()
	url2 := "ws" + strings.TrimPrefix(srv2.URL, "http")
	s2.SetPublicURL(url2)

	// Połączenie sieciowe SNDP z wagami latencji
	s1.mesh.addNeighbor(url2, 0, 10)
	s2.mesh.addNeighbor(url1, 0, 10)

	_, aPriv, _ := ed25519.GenerateKey(nil)
	_, bPriv, _ := ed25519.GenerateKey(nil)

	a := NewClient(url1, aPriv)
	b := NewClient(url2, bPriv)

	got := make(chan []byte, 1)
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
	for !(a.Up() && b.Up()) {
		if time.Now().After(deadline) {
			t.Fatal("clients did not come online")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Klient A (Server 1) wysyła sygnał do Klienta B (Server 2)
	if err := a.Signal(b.Pub, []byte("mesh-hello")); err != nil {
		t.Fatal(err)
	}

	select {
	case p := <-got:
		if string(p) != "mesh-hello" {
			t.Fatalf("received wrong payload over mesh: %q", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("mesh signal was not relayed from Server 1 to Server 2")
	}
}

func TestDijkstraRouting(t *testing.T) {
	s := NewServer()
	s.PublicURL = "ws://c1"

	// Definiujemy topologię 4 węzłów:
	// c1 --(150ms)--> c3 --(10ms)--> c4 (suma = 160ms)
	// c1 --(12ms)-->  c2 --(15ms)--> c3 --(10ms)--> c4 (suma = 37ms)
	s.mesh.updateTopologyEdge("ws://c1", "ws://c3", 150)
	s.mesh.updateTopologyEdge("ws://c3", "ws://c4", 10)

	s.mesh.updateTopologyEdge("ws://c1", "ws://c2", 12)
	s.mesh.updateTopologyEdge("ws://c2", "ws://c3", 15)

	nextHop := s.mesh.NextHopDijkstra("ws://c1", "ws://c4")
	if nextHop != "ws://c2" {
		t.Fatalf("Dijkstra expected next hop 'ws://c2' (37ms path), got %q", nextHop)
	}
}

func TestCebulionSecurityAndLimits(t *testing.T) {
	s := NewServer()
	s.PublicURL = "ws://local-cebulion"

	// Weryfikacja ochrony SSRF przed adresami metadanych chmurowych i nieprawidłowym IP w Sieci Cebulionowej
	s.mesh.addNeighbor("http://169.254.169.254/latest", 0, 10)
	s.mesh.addNeighbor("http://0.0.0.0:80", 0, 10)
	s.mesh.addNeighbor("ftp://bad-scheme.com", 0, 10)

	if len(s.mesh.neighbors) != 0 {
		t.Fatalf("Oczekiwanio 0 sąsiadów po próbie wstrzyknięcia złośliwych adresów, jest %d", len(s.mesh.neighbors))
	}

	s.mesh.addNeighbor("ws://127.0.0.1:8080", 5, 10)
	if len(s.mesh.neighbors) != 1 {
		t.Fatalf("Oczekiwano 1 sąsiada po dodaniu ws://127.0.0.1:8080, jest %d", len(s.mesh.neighbors))
	}

	// Weryfikacja ochrony pamięci przed rozrostem grafu (MaxTopologyNodes)
	for i := 0; i < 350; i++ {
		s.mesh.updateTopologyEdge("ws://node0", "ws://node"+string(rune(i+1000)), 10)
	}
	s.mesh.mu.Lock()
	nodesCount := len(s.mesh.topology)
	s.mesh.mu.Unlock()

	if nodesCount > MaxTopologyNodes {
		t.Fatalf("Topologia przekroczyła limit MaxTopologyNodes (%d): %d", MaxTopologyNodes, nodesCount)
	}
}

func TestCebulionMultiHopRelay(t *testing.T) {
	s1 := NewServer()
	srv1 := httptest.NewServer(s1)
	defer srv1.Close()
	url1 := "ws" + strings.TrimPrefix(srv1.URL, "http")
	s1.SetPublicURL(url1)

	s2 := NewServer()
	srv2 := httptest.NewServer(s2)
	defer srv2.Close()
	url2 := "ws" + strings.TrimPrefix(srv2.URL, "http")
	s2.SetPublicURL(url2)

	s3 := NewServer()
	srv3 := httptest.NewServer(s3)
	defer srv3.Close()
	url3 := "ws" + strings.TrimPrefix(srv3.URL, "http")
	s3.SetPublicURL(url3)

	// Łączymy w siatkę Sieci Cebulion: s1 <-> s2 <-> s3
	s1.mesh.addNeighbor(url2, 0, 10)
	s2.mesh.addNeighbor(url1, 0, 10)
	s2.mesh.addNeighbor(url3, 0, 10)
	s3.mesh.addNeighbor(url2, 0, 10)

	_, aPriv, _ := ed25519.GenerateKey(nil)
	_, bPriv, _ := ed25519.GenerateKey(nil)

	a := NewClient(url1, aPriv)
	b := NewClient(url3, bPriv)

	got := make(chan []byte, 1)
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
	for !(a.Up() && b.Up()) {
		if time.Now().After(deadline) {
			t.Fatal("clients did not come online in Sieć Cebulion")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Klient A na s1 wysyła sygnał do Klienta B na s3 przez węzeł pośredni s2
	if err := a.Signal(b.Pub, []byte("cebulion-multihop-test")); err != nil {
		t.Fatal(err)
	}

	select {
	case p := <-got:
		if string(p) != "cebulion-multihop-test" {
			t.Fatalf("received wrong payload in Sieć Cebulionowa: %q", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("mesh signal was not relayed across 3 hops (s1 -> s2 -> s3) in Sieć Cebulionowa")
	}
}
