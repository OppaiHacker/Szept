package connector

import (
	"crypto/ed25519"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
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
	r.Header.Set("X-Forwarded-For", "10.0.0.1, 198.51.100.9") // first = client-forged
	if ip := s.clientIP(r); ip != "127.0.0.1" {
		t.Fatalf("bez --trust-proxy liczy się loopback, dostałem %q", ip)
	}
	s.SetTrustProxy(true)
	if ip := s.clientIP(r); ip != "198.51.100.9" {
		t.Fatalf("z --trust-proxy ma być ostatni wpis XFF (dopisany przez proxy), dostałem %q", ip)
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

// meshNode spins up a connector of an open test mesh, on a secret path.
func meshNode(t *testing.T) (*Server, string) {
	t.Helper()
	s := NewServer()
	s.SetPath("/tajne")
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/tajne"
	s.SetPublicURL(u)
	return s, u
}

func TestMeshRelay(t *testing.T) {
	allowLoopbackMesh(t)
	s1, url1 := meshNode(t)
	s2, url2 := meshNode(t)

	// SNDP network connection with latency weights
	s1.mesh.addNeighbor(url2)
	s2.mesh.addNeighbor(url1)

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

// Mesh operations belong to the mesh plane: an ordinary client socket must not
// reach them, and a private mesh (--mesh-secret) admits only nodes with the MAC.
func TestPrivateMeshRequiresAuth(t *testing.T) {
	allowLoopbackMesh(t)
	s, srvURL := meshNode(t)
	s.SetMeshSecret(testMeshSecret)

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

	ws, _, err := websocket.DefaultDialer.Dial(srvURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	forged := Msg{
		Op:      "mesh_signal",
		From:    []byte("TOTALLY-NOT-A-REAL-PUBKEY-------"),
		To:      victimPub,
		TraceID: "atak-1",
		TTL:     3,
		Payload: []byte(`{"attacker":"controlled"}`),
	}
	if err := ws.WriteJSON(forged); err != nil {
		t.Fatal(err)
	}
	// a wrong MAC must not open the plane either
	bad := Msg{Op: "mesh_hello", TS: time.Now().Unix(), Sig: make([]byte, 32), From: []byte("ws://zly-serwer.example:7777")}
	if err := ws.WriteJSON(bad); err != nil {
		t.Fatal(err)
	}
	forged.TraceID = "atak-2"
	if err := ws.WriteJSON(forged); err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-got:
		t.Fatalf("nieuwierzytelniony klient przekazał ramkę do ofiary: %s", p)
	case <-time.After(500 * time.Millisecond):
	}
	if s.mesh.neighbors["ws://zly-serwer.example:7777"] != nil {
		t.Fatal("zły MAC wstrzyknął sąsiada")
	}

	// with the right secret the same socket does become a mesh peer
	ts := time.Now().Unix()
	if err := ws.WriteJSON(Msg{Op: "mesh_hello", TS: ts, Sig: meshMAC([]byte(testMeshSecret), ts), From: []byte("ws://sasiad.example:7777")}); err != nil {
		t.Fatal(err)
	}
	forged.TraceID = "atak-3"
	if err := ws.WriteJSON(forged); err != nil {
		t.Fatal(err)
	}
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("uwierzytelniony węzeł nie przekazał sygnału")
	}
	s.mesh.mu.Lock()
	n := s.mesh.neighbors["ws://sasiad.example:7777"]
	s.mesh.mu.Unlock()
	if n == nil {
		t.Fatal("uwierzytelniony sąsiad nie został dodany")
	}
}

// The trace ring is fed attacker-controlled ids: it must stay bounded and a
// flood must evict old ids, never stop accepting new ones (that froze the mesh).
func TestSeenTracesBounded(t *testing.T) {
	m := newMeshManager(nil, "ws://me", nil)
	if !m.markTraceSeen("", nil, nil) {
		t.Fatal("pusty TraceID ma być odrzucony")
	}
	if !m.markTraceSeen(strings.Repeat("x", maxTraceIDLen+1), nil, nil) {
		t.Fatal("przerośnięty TraceID ma być odrzucony")
	}
	if m.markTraceSeen("abc", nil, nil) || !m.markTraceSeen("abc", nil, nil) {
		t.Fatal("pierwszy raz nowy, drugi raz widziany")
	}
	for i := 0; i < maxSeenTraces; i++ {
		m.markTraceSeen(strconv.Itoa(i), nil, nil)
	}
	if m.markTraceSeen("po-przepelnieniu", nil, nil) {
		t.Fatal("po zalewie nowy ślad ma przejść")
	}
	if len(m.traces) > maxSeenTraces || len(m.traceLog) > maxSeenTraces {
		t.Fatalf("ślady urosły ponad limit: %d", len(m.traces))
	}
	if m.markTraceSeen("abc", nil, nil) {
		t.Fatal("najstarszy ślad miał wypaść")
	}
}

// mesh_found counts only for the key the trace asked about, and only once.
func TestMeshFoundBoundToKey(t *testing.T) {
	m := newMeshManager(nil, "ws://me", nil)
	n := &neighborNode{url: "ws://sasiad"}
	m.markTraceSeen("t1", nil, []byte("ofiara"))
	m.found(n, Msg{Op: "mesh_found", TraceID: "t1", Pub: []byte("inny")})
	if _, ok := m.routes["inny"]; ok {
		t.Fatal("odpowiedź o cudzy klucz ustawiła trasę")
	}
	m.found(n, Msg{Op: "mesh_found", TraceID: "t1", Pub: []byte("ofiara")})
	if _, ok := m.routes["ofiara"]; !ok {
		t.Fatal("prawdziwa odpowiedź nie ustawiła trasy")
	}
	delete(m.routes, "ofiara")
	m.found(n, Msg{Op: "mesh_found", TraceID: "t1", Pub: []byte("ofiara")})
	if _, ok := m.routes["ofiara"]; ok {
		t.Fatal("ślad dało się użyć drugi raz")
	}
}

// A probe that does not know the path sees a plain 404, never the connector.
func TestConnectorHidesFromProbes(t *testing.T) {
	_, u := meshNode(t)
	base := "http" + strings.TrimPrefix(strings.TrimSuffix(u, "/tajne"), "ws")
	for _, p := range []string{"/", "/ndp/neighbors", "/tajne"} {
		resp, err := http.Get(base + p)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound || strings.Contains(strings.ToLower(string(body)), "szept") {
			t.Fatalf("GET %s: %d %q", p, resp.StatusCode, body)
		}
	}
	if _, _, err := websocket.DefaultDialer.Dial(strings.TrimSuffix(u, "/tajne")+"/", nil); err == nil {
		t.Fatal("upgrade poza ścieżką przeszedł")
	}
	ws, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	ws.Close()
}

// Clients on different connectors find each other: lookup asks the mesh.
func TestMeshLookup(t *testing.T) {
	allowLoopbackMesh(t)
	s1, url1 := meshNode(t)
	_, url2 := meshNode(t)
	s1.mesh.addNeighbor(url2)

	_, aPriv, _ := ed25519.GenerateKey(nil)
	_, bPriv, _ := ed25519.GenerateKey(nil)
	_, cPriv, _ := ed25519.GenerateKey(nil)
	a, b := NewClient(url1, aPriv), NewClient(url2, bPriv)
	go a.Run()
	go b.Run()
	defer a.Close()
	defer b.Close()
	deadline := time.Now().Add(5 * time.Second)
	for !(a.Up() && b.Up()) {
		if time.Now().After(deadline) {
			t.Fatal("klienci nie wstali")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if on, err := a.Lookup(b.Pub, 3*time.Second); err != nil || !on {
		t.Fatalf("b na innym connectorze: online=%v err=%v", on, err)
	}
	if on, _ := a.Lookup(cPriv.Public().(ed25519.PublicKey), 3*time.Second); on {
		t.Fatal("nieistniejący klucz online")
	}
	s1.mesh.mu.Lock()
	_, routed := s1.mesh.routes[string(b.Pub)]
	s1.mesh.mu.Unlock()
	if !routed {
		t.Fatal("brak trasy po mesh_found")
	}
}

func TestCebulionMultiHopRelay(t *testing.T) {
	allowLoopbackMesh(t)
	s1, url1 := meshNode(t)
	s2, url2 := meshNode(t)
	s3, url3 := meshNode(t)

	// Connect into Cebulion Network mesh: s1 <-> s2 <-> s3
	s1.mesh.addNeighbor(url2)
	s2.mesh.addNeighbor(url1)
	s2.mesh.addNeighbor(url3)
	s3.mesh.addNeighbor(url2)

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

// A hostname is fine for the mesh now; one that resolves to a private address
// is still refused after DNS, not by string matching.
func TestSafeDialResolvesHostnames(t *testing.T) {
	_, err := safeDial(t.Context(), "tcp", "localhost:1")
	if err == nil || !strings.Contains(err.Error(), "nie-publiczny") {
		t.Fatalf("localhost przepuszczony: %v", err)
	}
}

// The hosting connector confirms a relayed signal; a signal nobody
// takes comes back to the sender as "undelivered" instead of vanishing.
func TestMeshSignalReceipt(t *testing.T) {
	allowLoopbackMesh(t)
	s1, url1 := meshNode(t)
	_, url2 := meshNode(t)
	s1.mesh.addNeighbor(url2)

	_, aPriv, _ := ed25519.GenerateKey(nil)
	_, bPriv, _ := ed25519.GenerateKey(nil)
	ghost, _, _ := ed25519.GenerateKey(nil)
	a, b := NewClient(url1, aPriv), NewClient(url2, bPriv)
	lost := make(chan []byte, 4)
	a.OnUndelivered = func(pub []byte) { lost <- pub }
	go a.Run()
	go b.Run()
	defer a.Close()
	defer b.Close()
	deadline := time.Now().Add(5 * time.Second)
	for !(a.Up() && b.Up()) {
		if time.Now().After(deadline) {
			t.Fatal("clients did not come online")
		}
		time.Sleep(20 * time.Millisecond)
	}
	a.Signal(b.Pub, []byte("hi"))
	a.Signal(ghost, []byte("hi"))
	select {
	case pub := <-lost:
		if string(pub) != string(ghost) {
			t.Fatal("delivered signal reported as undelivered")
		}
	case <-time.After(3 * lookupWait):
		t.Fatal("no undelivered notice for an unknown key")
	}
	select {
	case <-lost:
		t.Fatal("second undelivered notice")
	case <-time.After(lookupWait):
	}
	s1.mesh.mu.Lock()
	_, routed := s1.mesh.routes[string(b.Pub)]
	s1.mesh.mu.Unlock()
	if !routed {
		t.Fatal("receipt did not teach the route")
	}
}

// One node answers gossip for a bounded number of network blocks, so
// walking the mesh from many address ranges stops paying off.
func TestGossipBlockCap(t *testing.T) {
	m := newMeshManager(NewServer(), "ws://198.51.100.1:7777", nil)
	m.addNeighbor("ws://203.0.113.9:7777").lastSeen = time.Now()
	for i := range maxGossipBlocks {
		if len(m.gossipFor(fmt.Sprintf("10.%d.0.1", i))) == 0 {
			t.Fatalf("block %d refused under the cap", i)
		}
	}
	if len(m.gossipFor("11.0.0.1")) != 0 {
		t.Fatal("block past the cap got gossip")
	}
	if len(m.gossipFor("10.0.9.9")) == 0 {
		t.Fatal("known block lost its slot")
	}
}

// --private-gossip: the node shares no neighbours outside a
// secret mesh, and a neighbour that asked to stay hidden is never passed on.
func TestPrivateGossip(t *testing.T) {
	s := NewServer()
	m := s.mesh
	m.addNeighbor("ws://203.0.113.9:7777").lastSeen = time.Now()
	hid := m.addNeighbor("ws://203.0.113.10:7777")
	hid.lastSeen = time.Now()
	// the flag comes from the neighbour's own answer to our dial
	m.fromNeighbor(hid, Msg{Op: "ok", Hidden: true})
	if got := m.gossipFor("10.0.0.1"); len(got) != 1 || got[0] != "ws://203.0.113.9:7777" {
		t.Fatalf("hidden neighbour leaked or visible one missing: %v", got)
	}
	s.SetPrivateGossip()
	if !m.helloMsg().Hidden {
		t.Fatal("hello does not ask to stay hidden")
	}
	if len(m.gossipFor("10.0.0.1")) != 0 || len(m.shareable("10.0.0.1", redirectShare, false)) != 0 {
		t.Fatal("private node shared its neighbours")
	}
	m.secret = []byte("s")
	if len(m.gossipFor("10.0.0.1")) == 0 {
		t.Fatal("secret mesh peer got nothing")
	}
	if len(m.shareable("10.0.0.1", redirectShare, false)) != 0 {
		t.Fatal("client redirect from a private node")
	}
}

func TestPublicIPSpecialRanges(t *testing.T) {
	for _, s := range []string{"100.64.0.1", "198.18.0.1", "240.0.0.1", "0.1.2.3", "64:ff9b::a00:1", "10.0.0.1", "::1"} {
		if publicIP(net.ParseIP(s)) {
			t.Errorf("%s uznany za publiczny", s)
		}
	}
	for _, s := range []string{"1.1.1.1", "2606:4700::1111"} {
		if !publicIP(net.ParseIP(s)) {
			t.Errorf("%s uznany za niepubliczny", s)
		}
	}
}

// Walking the mesh from every allowed block still reveals only maxGossipURLs.
func TestGossipURLBudget(t *testing.T) {
	m := NewServer().mesh
	for i := range MaxNeighbors {
		m.addNeighbor(fmt.Sprintf("ws://203.0.113.%d:7777", i+1)).lastSeen = time.Now()
	}
	learned := map[string]bool{}
	for b := range maxGossipBlocks {
		for _, u := range m.gossipFor(fmt.Sprintf("198.51.%d.1", b)) {
			learned[u] = true
		}
	}
	if len(learned) > maxGossipURLs {
		t.Fatalf("spis mesha: %d adresów, limit %d", len(learned), maxGossipURLs)
	}
}

// A client whose clock is 5 minutes behind is first rejected, then adopts the
// connector's time from the "bad register" reply and registers.
func TestClockSkewRecovers(t *testing.T) {
	srv := httptest.NewServer(NewServer())
	defer srv.Close()
	defer clockSkew.Store(0)
	clockSkew.Store(-300)

	_, priv, _ := ed25519.GenerateKey(nil)
	c := NewClient("ws"+strings.TrimPrefix(srv.URL, "http"), priv)
	go c.Run()
	defer c.Close()
	deadline := time.Now().Add(5 * time.Second)
	for !c.Up() {
		if time.Now().After(deadline) {
			t.Fatalf("never registered, skew=%d err=%q", clockSkew.Load(), c.LastErr())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if s := clockSkew.Load(); s < -2 || s > 2 {
		t.Fatalf("skew not corrected: %d", s)
	}
}
