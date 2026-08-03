package connector

import (
	"crypto/ed25519"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
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

	// loopback counts like any other address unless a proxy is explicitly trusted,
	// otherwise a connector behind a reverse proxy has no per-IP limit at all
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	r.Header.Set("X-Forwarded-For", "198.51.100.9, 10.0.0.1")
	if ip := s.clientIP(r); ip != "127.0.0.1" {
		t.Fatalf("bez --trust-proxy liczy się loopback, dostałem %q", ip)
	}
	s.SetTrustProxy(true)
	if ip := s.clientIP(r); ip != "198.51.100.9" {
		t.Fatalf("z --trust-proxy ma być pierwszy wpis XFF, dostałem %q", ip)
	}
	s.SetTrustProxy(false)
	r.RemoteAddr = "203.0.113.7:5555"
	if ip := s.clientIP(r); ip != "203.0.113.7" {
		t.Fatalf("clientIP = %q", ip)
	}
	for i := 0; i < MaxSessionsPerIP+5; i++ {
		if !s.takeSlot("") {
			t.Fatal("zwolnione IP nie ma mieć limitu")
		}
	}
}

// A browser page must not be able to open a socket to the connector (CSWSH):
// native clients send no Origin, a web page always does.
func TestCheckOriginRejectsBrowsers(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if !checkOrigin(r) {
		t.Fatal("brak Origin (klient natywny) ma przechodzić")
	}
	r.Header.Set("Origin", "https://zla-strona.example")
	if checkOrigin(r) {
		t.Fatal("Origin z przeglądarki ma być odrzucony")
	}
}

// A hostile connector must not be able to downgrade transport or grow
// the fallback list without bound.
func TestRedirectValidation(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	c := NewClient("wss://dobry.example:7777", priv)
	c.addFallbacks([]string{"ws://podsluch.example", "wss://zapas.example"})
	if len(c.FallbackURLs) != 1 || c.FallbackURLs[0] != "wss://zapas.example" {
		t.Fatalf("degradacja wss->ws przeszła: %v", c.FallbackURLs)
	}
	c.addFallbacks([]string{"wss://zapas.example"})
	if len(c.FallbackURLs) != 1 {
		t.Fatalf("duplikat wszedł: %v", c.FallbackURLs)
	}
	many := make([]string, 0, 32)
	for i := 0; i < 32; i++ {
		many = append(many, "wss://spam"+string(rune('a'+i%26))+".example")
	}
	c.addFallbacks(many)
	if len(c.FallbackURLs) > maxFallbacks {
		t.Fatalf("lista zapasowa urosła ponad %d: %d", maxFallbacks, len(c.FallbackURLs))
	}
	// plain ws:// connector may be upgraded to wss://, never the other way round
	c2 := NewClient("ws://zwykly.example", priv)
	c2.addFallbacks([]string{"wss://lepszy.example"})
	if len(c2.FallbackURLs) != 1 {
		t.Fatal("podniesienie ws->wss ma być dozwolone")
	}
}

// Single socket = single pubkey (otherwise each registration leaves entry
// that disconnect does not clean up), and eavesdropped frame cannot steal slot.
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
	// replaying same signed frame from another socket does not steal slot
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

// allowLoopbackMesh relaxes the SSRF guard for tests that necessarily run
// against httptest servers on 127.0.0.1.
func allowLoopbackMesh(t *testing.T) {
	t.Helper()
	allowPrivateMeshHosts.Store(true)
	t.Cleanup(func() { allowPrivateMeshHosts.Store(false) })
}

const testMeshSecret = "wspolny-sekret-testowy"

// meshNode spins up a connector that is part of an authenticated test mesh.
func meshNode(t *testing.T) (*Server, string) {
	t.Helper()
	s := NewServer()
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	u := "ws" + strings.TrimPrefix(srv.URL, "http")
	s.SetPublicURL(u)
	s.SetMeshSecret(testMeshSecret)
	return s, u
}

func TestMeshRelay(t *testing.T) {
	allowLoopbackMesh(t)
	s1, url1 := meshNode(t)
	s2, url2 := meshNode(t)

	// SNDP network connection with latency weights
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

	// Client A (Server 1) sends signal to Client B (Server 2)
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

// A neighbour URL is a public ws:// socket or nothing. Everything that
// could point the mesh back at the host's own network is refused.
func TestNeighborURLRejectsPrivate(t *testing.T) {
	const pub = "ws://local-cebulion"
	bad := []string{
		"http://169.254.169.254/latest", // cloud metadata
		"https://169.254.169.254/latest",
		"http://0.0.0.0:80",
		"ftp://bad-scheme.com",
		"http://example.com",  // http is an internal detail of pingNode, never a neighbour
		"https://example.com", //
		"ws://127.0.0.1:8080", // loopback
		"ws://[::1]:8080",
		"ws://10.1.2.3:7777",    // RFC1918
		"ws://192.168.0.5:7777", //
		"ws://172.16.0.5:7777",  //
		"ws://169.254.169.254/", // link-local by scheme too
		"ws://",                 // no host
		"",                      //
		pub,                     // ourselves
		"ws://" + strings.Repeat("a", 200) + ".example", // oversized
	}
	for _, u := range bad {
		if isValidNeighborURL(u, pub) {
			t.Errorf("przepuszczony zły adres sąsiada: %q", u)
		}
	}
	for _, u := range []string{"ws://c2.szept.io:7777", "wss://c3.szept.io"} {
		if !isValidNeighborURL(u, pub) {
			t.Errorf("odrzucony poprawny adres sąsiada: %q", u)
		}
	}
}

// Mesh operations belong to an authenticated plane. An ordinary
// client socket — registered or not — must not reach them.
func TestMeshRequiresAuth(t *testing.T) {
	allowLoopbackMesh(t)
	s, _ := meshNode(t)
	srvURL := "ws://" + s.PublicURL[len("ws://"):]

	victimPub, victimPriv, _ := ed25519.GenerateKey(nil)
	victim := NewClient(srvURL, victimPriv)
	got := make(chan []byte, 1)
	victim.OnSignal = func(from, payload []byte) { got <- payload }
	go victim.Run()
	defer victim.Close()

	deadline := time.Now().Add(5 * time.Second)
	for !victim.Up() {
		if time.Now().After(deadline) {
			t.Fatal("ofiara się nie zarejestrowała")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// unregistered attacker: no key, no registration, just a socket
	ws, _, err := websocket.DefaultDialer.Dial(srvURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	if err := ws.WriteJSON(Msg{
		Op:      "mesh_signal",
		From:    []byte("TOTALLY-NOT-A-REAL-PUBKEY-------"),
		To:      victimPub,
		TraceID: "atak-1",
		Payload: []byte(`{"attacker":"controlled"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteJSON(Msg{Op: "ndp_gossip", From: []byte("ws://zly-serwer.example:7777")}); err != nil {
		t.Fatal(err)
	}
	// a wrong MAC must not open the plane either
	if err := ws.WriteJSON(Msg{Op: "mesh_hello", TS: time.Now().Unix(), Sig: make([]byte, 32)}); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteJSON(Msg{Op: "ndp_gossip", From: []byte("ws://zly-serwer2.example:7777")}); err != nil {
		t.Fatal(err)
	}

	select {
	case p := <-got:
		t.Fatalf("nieuwierzytelniony klient przekazał ramkę do ofiary: %s", p)
	case <-time.After(500 * time.Millisecond):
	}
	for _, u := range s.mesh.activeNeighborURLs() {
		if strings.Contains(u, "zly-serwer") {
			t.Fatalf("nieuwierzytelniony klient wstrzyknął sąsiada: %s", u)
		}
	}

	// with the right secret the same socket does become a mesh peer
	ts := time.Now().Unix()
	if err := ws.WriteJSON(Msg{Op: "mesh_hello", TS: ts, Sig: meshMAC([]byte(testMeshSecret), ts)}); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteJSON(Msg{Op: "ndp_gossip", From: []byte("ws://sasiad.example:7777")}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for {
		if slices.Contains(s.mesh.activeNeighborURLs(), "ws://sasiad.example:7777") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("uwierzytelniony sąsiad nie został dodany")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// markTraceSeen is fed attacker-controlled ids, so it must refuse to grow.
func TestSeenTracesBounded(t *testing.T) {
	m := newMeshManager(nil, "ws://me", nil)
	if !m.markTraceSeen("") {
		t.Fatal("pusty TraceID ma być odrzucony")
	}
	if !m.markTraceSeen(strings.Repeat("x", maxTraceIDLen+1)) {
		t.Fatal("przerośnięty TraceID ma być odrzucony")
	}
	if m.markTraceSeen("abc") || !m.markTraceSeen("abc") {
		t.Fatal("pierwszy raz nowy, drugi raz widziany")
	}
	m.mu.Lock()
	clear(m.seenTraces)
	for i := 0; i < maxSeenTraces; i++ {
		m.seenTraces[strconv.Itoa(i)] = time.Now()
	}
	m.mu.Unlock()
	if !m.markTraceSeen("po-przepelnieniu") {
		t.Fatal("po przepełnieniu mapa ma upuszczać, nie rosnąć")
	}
	m.mu.Lock()
	n := len(m.seenTraces)
	m.mu.Unlock()
	if n > maxSeenTraces {
		t.Fatalf("seenTraces urosło ponad limit: %d", n)
	}
}

func TestCebulionMultiHopRelay(t *testing.T) {
	allowLoopbackMesh(t)
	s1, url1 := meshNode(t)
	s2, url2 := meshNode(t)
	s3, url3 := meshNode(t)

	// Connect into Cebulion Network mesh: s1 <-> s2 <-> s3
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

	// Client A on s1 sends signal to Client B on s3 via intermediate node s2
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
