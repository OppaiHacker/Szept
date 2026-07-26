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
