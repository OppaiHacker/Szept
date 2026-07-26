// Package connector: the rendezvous "tracker". Peers register their pubkey,
// look each other up and relay opaque, end-to-end-signed signaling blobs.
// The server stores nothing, sees no message content and cannot forge `from`
// (it stamps it from the authenticated registration).
package connector

import (
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const registerContext = "szept-register-v1"

type Msg struct {
	Op      string `json:"op"` // register | lookup | signal | error | ok
	Pub     []byte `json:"pub,omitempty"`
	To      []byte `json:"to,omitempty"`
	From    []byte `json:"from,omitempty"`
	TS      int64  `json:"ts,omitempty"`
	Sig     []byte `json:"sig,omitempty"`
	Payload []byte `json:"payload,omitempty"`
	Online  bool   `json:"online,omitempty"`
	Err     string `json:"err,omitempty"`
}

func registerDigest(pub []byte, ts int64) []byte {
	b := make([]byte, 0, len(registerContext)+len(pub)+8)
	b = append(b, registerContext...)
	b = append(b, pub...)
	b = binary.BigEndian.AppendUint64(b, uint64(ts))
	return b
}

// SignRegister is used by clients; exported for the server test.
func SignRegister(priv ed25519.PrivateKey, pub []byte, ts int64) []byte {
	return ed25519.Sign(priv, registerDigest(pub, ts))
}

// ---- server ----

type client struct {
	ws *websocket.Conn
	mu sync.Mutex
}

func (c *client) send(m Msg) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.ws.WriteJSON(m)
}

type Server struct {
	mu    sync.Mutex
	peers map[string]*client
	up    websocket.Upgrader
	start time.Time
}

func NewServer() *Server {
	return &Server{
		peers: map[string]*client{},
		up:    websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }},
		start: time.Now(),
	}
}

// Count returns the number of currently registered peers.
func (s *Server) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.peers)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// plain HTTP (browser/curl) gets a status page — proves the connector is up
	if !websocket.IsWebSocketUpgrade(r) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "szept-connector OK\npeers online: %d\nuptime: %s\n",
			s.Count(), time.Since(s.start).Round(time.Second))
		return
	}
	ws, err := s.up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &client{ws: ws}
	var pub []byte
	defer func() {
		ws.Close()
		if pub != nil {
			s.mu.Lock()
			if s.peers[string(pub)] == c {
				delete(s.peers, string(pub))
			}
			s.mu.Unlock()
		}
	}()
	ws.SetReadLimit(256 << 10)
	ws.SetReadDeadline(time.Now().Add(90 * time.Second))
	ws.SetPongHandler(func(string) error {
		ws.SetReadDeadline(time.Now().Add(90 * time.Second))
		return nil
	})
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for range t.C {
			c.mu.Lock()
			err := ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
			c.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	for {
		var m Msg
		if err := ws.ReadJSON(&m); err != nil {
			return
		}
		ws.SetReadDeadline(time.Now().Add(90 * time.Second))
		switch m.Op {
		case "register":
			if len(m.Pub) != ed25519.PublicKeySize ||
				time.Since(time.Unix(m.TS, 0)) > time.Minute || time.Until(time.Unix(m.TS, 0)) > time.Minute ||
				!ed25519.Verify(ed25519.PublicKey(m.Pub), registerDigest(m.Pub, m.TS), m.Sig) {
				c.send(Msg{Op: "error", Err: "bad register"})
				continue
			}
			pub = m.Pub
			s.mu.Lock()
			s.peers[string(pub)] = c
			s.mu.Unlock()
			c.send(Msg{Op: "ok"})
		case "lookup":
			s.mu.Lock()
			_, online := s.peers[string(m.Pub)]
			s.mu.Unlock()
			c.send(Msg{Op: "lookup", Pub: m.Pub, Online: online})
		case "signal":
			if pub == nil {
				c.send(Msg{Op: "error", Err: "register first"})
				continue
			}
			s.mu.Lock()
			dst := s.peers[string(m.To)]
			s.mu.Unlock()
			if dst == nil {
				c.send(Msg{Op: "error", To: m.To, Err: "offline"})
				continue
			}
			dst.send(Msg{Op: "signal", From: pub, To: m.To, Payload: m.Payload})
		}
	}
}

// ---- client ----

type Client struct {
	URL      string
	Priv     ed25519.PrivateKey
	Pub      ed25519.PublicKey
	OnSignal func(from []byte, payload []byte)
	OnUp     func() // fires after each successful (re-)registration
	OnDown   func() // fires when a registered session drops

	mu      sync.Mutex
	ws      *websocket.Conn
	lookups chan Msg
	closed  bool
	up      bool
	lastErr string
}

// Up reports whether the client is currently registered with the connector.
func (c *Client) Up() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.up
}

// LastErr returns the most recent session error ("" once registered) so UIs
// can show *why* the connector is unreachable instead of spinning forever.
func (c *Client) LastErr() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastErr
}

func NewClient(url string, priv ed25519.PrivateKey) *Client {
	return &Client{URL: url, Priv: priv, Pub: priv.Public().(ed25519.PublicKey), lookups: make(chan Msg, 16)}
}

// Run connects, registers and reads until closed; reconnects with backoff.
func (c *Client) Run() {
	backoff := time.Second
	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()
		start := time.Now()
		if err := c.session(); err != nil {
			log.Printf("connector: %v (retry in %s)", err, backoff)
			c.mu.Lock()
			c.lastErr = err.Error()
			c.mu.Unlock()
		}
		if time.Since(start) > 15*time.Second {
			backoff = time.Second // session lived a while — reset backoff
		}
		time.Sleep(backoff)
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (c *Client) session() error {
	ws, _, err := websocket.DefaultDialer.Dial(c.URL, nil)
	if err != nil && strings.Contains(err.Error(), "no such host") {
		// Android potrafi odciąć Go od systemowego DNS — pytamy 1.1.1.1 i łączymy po IP.
		ws, err = dialFallbackDNS(c.URL)
	}
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.ws = ws
	c.mu.Unlock()
	defer func() {
		ws.Close()
		c.mu.Lock()
		wasUp := c.up
		c.up = false
		c.mu.Unlock()
		if wasUp && c.OnDown != nil {
			go c.OnDown()
		}
	}()

	ts := time.Now().Unix()
	err = c.write(Msg{Op: "register", Pub: c.Pub, TS: ts, Sig: SignRegister(c.Priv, c.Pub, ts)})
	if err != nil {
		return err
	}
	var ack Msg
	if err := ws.ReadJSON(&ack); err != nil {
		return err
	}
	if ack.Op != "ok" {
		return errors.New("register rejected: " + ack.Err)
	}
	c.mu.Lock()
	c.up = true
	c.lastErr = ""
	c.mu.Unlock()
	if c.OnUp != nil {
		go c.OnUp()
	}
	for {
		var m Msg
		if err := ws.ReadJSON(&m); err != nil {
			return err
		}
		switch m.Op {
		case "signal":
			if c.OnSignal != nil {
				go c.OnSignal(m.From, m.Payload)
			}
		case "lookup", "error":
			select {
			case c.lookups <- m:
			default:
			}
		}
	}
}

// dialFallbackDNS resolves the host via a public resolver and dials the IP,
// but keeps the original URL so TLS SNI and the Host header stay the hostname —
// podmiana hosta na IP w URL-u wywracała wss:// (cert bez IP SAN, zły routing).
// ponytail: tylko awaryjna ścieżka po "no such host"; 1.1.1.1 na sztywno wystarczy.
func dialFallbackDNS(rawURL string) (*websocket.Conn, error) {
	r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, "1.1.1.1:53")
	}}
	d := *websocket.DefaultDialer
	d.NetDialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		addrs, err := r.LookupHost(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("dns fallback: %w", err)
		}
		var dl net.Dialer
		return dl.DialContext(ctx, network, net.JoinHostPort(addrs[0], port))
	}
	ws, _, err := d.Dial(rawURL, nil)
	return ws, err
}

func (c *Client) write(m Msg) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ws == nil {
		return errors.New("not connected")
	}
	c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.ws.WriteJSON(m)
}

func (c *Client) Signal(to, payload []byte) error {
	return c.write(Msg{Op: "signal", To: to, Payload: payload})
}

// Lookup asks whether a peer is currently registered.
func (c *Client) Lookup(pub []byte, timeout time.Duration) (bool, error) {
	if err := c.write(Msg{Op: "lookup", Pub: pub}); err != nil {
		return false, err
	}
	deadline := time.After(timeout)
	for {
		select {
		case m := <-c.lookups:
			if m.Op == "lookup" && string(m.Pub) == string(pub) {
				return m.Online, nil
			}
		case <-deadline:
			return false, errors.New("lookup timeout")
		}
	}
}

func (c *Client) Close() {
	c.mu.Lock()
	c.closed = true
	if c.ws != nil {
		c.ws.Close()
	}
	c.mu.Unlock()
}

func JSON(v any) []byte { b, _ := json.Marshal(v); return b }
