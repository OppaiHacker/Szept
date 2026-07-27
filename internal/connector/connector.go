// Package connector: the rendezvous "tracker". Peers register their pubkey,
// look each other up and relay opaque, end-to-end-signed signaling blobs.
// The server stores nothing, sees no message content and cannot forge `from`
// (it stamps it from the authenticated registration).
package connector

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const registerContext = "szept-register-v1"

type Msg struct {
	Op      string `json:"op"` // register | lookup | signal | error | ok | redirect | ndp_gossip | mesh_signal
	Pub     []byte `json:"pub,omitempty"`
	To      []byte `json:"to,omitempty"`
	From    []byte `json:"from,omitempty"`
	TS      int64  `json:"ts,omitempty"`
	Sig     []byte `json:"sig,omitempty"`
	Payload []byte `json:"payload,omitempty"`
	Online  bool   `json:"online,omitempty"`
	Err     string `json:"err,omitempty"`

	// --- Nowe pola dla SNDP i Sieci Cebulionowej (Load Balancing & Mesh w Sieci Cebulion) ---
	Redirect []string       `json:"redirect,omitempty"`
	TraceID  string         `json:"trace_id,omitempty"`
	TTL      int            `json:"ttl,omitempty"`
	Nodes    []NeighborInfo `json:"nodes,omitempty"`
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
	ts int64 // timestamp rejestracji trzymającej slot — patrz "register"
}

func (c *client) send(m Msg) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.ws.WriteJSON(m)
}

type Server struct {
	mu        sync.Mutex
	peers     map[string]*client
	ips       map[string]int // adres IP -> otwarte sesje WS
	up        websocket.Upgrader
	start     time.Time
	MaxPeers  int
	PublicURL string
	mesh      *meshManager
}

func NewServer() *Server {
	s := &Server{
		peers: map[string]*client{},
		ips:   map[string]int{},
		up:    websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }},
		start: time.Now(),
	}
	s.mesh = newMeshManager(s, "", nil)
	return s
}

func (s *Server) SetSeeds(seeds []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mesh.seeds = seeds
	if len(seeds) > 0 {
		go s.mesh.loop()
	}
}

func (s *Server) SetPublicURL(rawURL string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PublicURL = rawURL
	s.mesh.publicURL = rawURL
}

func (s *Server) SetMaxPeers(max int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.MaxPeers = max
}

// MaxSessionsPerIP: klucze Ed25519 są darmowe, więc limit na pubkey nic nie daje —
// liczymy sesje po adresie IP.
const MaxSessionsPerIP = 64

// clientIP zwraca "" gdy limit ma nie obowiązywać.
// Za reverse proxy wszyscy przychodzą z loopbacku, więc go zwalniamy;
// jeśli kiedyś proxy będzie zdalne, trzeba zaufanemu X-Forwarded-For.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip == nil || ip.IsLoopback() {
		return ""
	}
	return host
}

func (s *Server) takeSlot(ip string) bool {
	if ip == "" {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ips[ip] >= MaxSessionsPerIP {
		return false
	}
	s.ips[ip]++
	return true
}

func (s *Server) freeSlot(ip string) {
	if ip == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ips[ip]--; s.ips[ip] <= 0 {
		delete(s.ips, ip)
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
		if r.URL.Path == "/ndp/neighbors" {
			w.Header().Set("Content-Type", "application/json")
			neighbors := make([]NeighborInfo, 0)
			for _, url := range s.mesh.activeNeighborURLs() {
				neighbors = append(neighbors, NeighborInfo{URL: url})
			}
			s.mesh.mu.Lock()
			topCopy := make(map[string]map[string]int64)
			for u, edges := range s.mesh.topology {
				topCopy[u] = make(map[string]int64)
				for v, w := range edges {
					topCopy[u][v] = w
				}
			}
			s.mesh.mu.Unlock()

			_ = json.NewEncoder(w).Encode(map[string]any{
				"url":         s.PublicURL,
				"peer_count":  s.Count(),
				"max_clients": s.MaxPeers,
				"neighbors":   neighbors,
				"topology":    topCopy,
			})
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "szept-connector (Sieć Cebulion) OK\npeers online: %d\nuptime: %s\nmesh neighbors: %d\n",
			s.Count(), time.Since(s.start).Round(time.Second), len(s.mesh.activeNeighborURLs()))
		return
	}
	if s.MaxPeers > 0 && s.Count() >= s.MaxPeers {
		ws, err := s.up.Upgrade(w, r, nil)
		if err == nil {
			_ = ws.WriteJSON(Msg{
				Op:       "redirect",
				Err:      "connector high load",
				Redirect: s.mesh.activeNeighborURLs(),
			})
			ws.Close()
		}
		return
	}
	ip := clientIP(r)
	if !s.takeSlot(ip) {
		http.Error(w, "too many connections", http.StatusTooManyRequests)
		return
	}
	defer s.freeSlot(ip)
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
		case "ndp_gossip":
			if len(m.From) > 0 {
				s.mesh.addNeighbor(string(m.From), 0, 0)
			}
		case "mesh_signal":
			if s.mesh.markTraceSeen(m.TraceID) {
				continue
			}
			s.mu.Lock()
			dst := s.peers[string(m.To)]
			s.mu.Unlock()
			if dst != nil {
				dst.send(Msg{Op: "signal", From: m.From, To: m.To, Payload: m.Payload})
			} else if m.TTL > 0 {
				targetConnector := s.mesh.routingCache[string(m.To)]
				s.mesh.relayMeshSignal(m, targetConnector)
			}
		case "register":
			if len(m.Pub) != ed25519.PublicKeySize ||
				time.Since(time.Unix(m.TS, 0)) > time.Minute || time.Until(time.Unix(m.TS, 0)) > time.Minute ||
				!ed25519.Verify(ed25519.PublicKey(m.Pub), registerDigest(m.Pub, m.TS), m.Sig) {
				c.send(Msg{Op: "error", Err: "bad register"})
				continue
			}
			if pub != nil && !bytes.Equal(pub, m.Pub) {
				c.send(Msg{Op: "error", Err: "already registered"})
				continue
			}
			s.mu.Lock()
			if old := s.peers[string(m.Pub)]; old != nil && old != c && m.TS <= old.ts {
				s.mu.Unlock()
				c.send(Msg{Op: "error", Err: "stale register"})
				continue
			}
			pub = m.Pub
			c.ts = m.TS
			s.peers[string(pub)] = c
			s.mu.Unlock()
			c.send(Msg{Op: "ok"})
		case "lookup":
			if pub == nil {
				c.send(Msg{Op: "error", Err: "register first"})
				continue
			}
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
				// Brak lokalnie - wyznacz optymalną ścieżkę Dijkstrą w Sieci Cebulionowej (Cebulion Network)
				targetConnector := s.mesh.routingCache[string(m.To)]
				s.mesh.relayMeshSignal(Msg{
					Op:      "mesh_signal",
					From:    pub,
					To:      m.To,
					Payload: m.Payload,
					TTL:     5,
				}, targetConnector)
				continue
			}
			dst.send(Msg{Op: "signal", From: pub, To: m.To, Payload: m.Payload})
		}
	}
}

// ---- client ----

type Client struct {
	URL          string
	FallbackURLs []string
	Priv         ed25519.PrivateKey
	Pub          ed25519.PublicKey
	OnSignal     func(from []byte, payload []byte)
	OnUp         func() // fires after each successful (re-)registration
	OnDown       func() // fires when a registered session drops

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
	urlsToTry := append([]string{c.URL}, c.FallbackURLs...)
	var ws *websocket.Conn
	var dialErr error

	ts := time.Now().Unix()
	regMsg := Msg{Op: "register", Pub: c.Pub, TS: ts, Sig: SignRegister(c.Priv, c.Pub, ts)}

	for _, u := range urlsToTry {
		var conn *websocket.Conn
		conn, _, dialErr = websocket.DefaultDialer.Dial(u, nil)
		if dialErr != nil && strings.Contains(dialErr.Error(), "no such host") {
			conn, dialErr = dialFallbackDNS(u)
		}
		if dialErr == nil {
			// Wysyłamy rejestrację jako pierwszą ramkę (zgodnie z protokołem szept)
			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteJSON(regMsg); err != nil {
				conn.Close()
				continue
			}
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			var ack Msg
			if err := conn.ReadJSON(&ack); err == nil {
				if ack.Op == "redirect" {
					conn.Close()
					if len(ack.Redirect) > 0 {
						c.FallbackURLs = append(c.FallbackURLs, ack.Redirect...)
					}
					continue
				} else if ack.Op == "ok" {
					conn.SetReadDeadline(time.Time{})
					ws = conn
					break
				} else {
					conn.Close()
					return errors.New("register rejected: " + ack.Err)
				}
			} else {
				conn.Close()
			}
		}
	}

	if ws == nil {
		if dialErr != nil {
			return dialErr
		}
		return errors.New("no reachable connector URL")
	}

	c.mu.Lock()
	c.ws = ws
	c.up = true
	c.lastErr = ""
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
// Tylko awaryjna ścieżka po "no such host"; 1.1.1.1 na sztywno wystarczy.
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
		ip := ""
		if addrs, err := r.LookupHost(ctx, host); err == nil && len(addrs) > 0 {
			ip = addrs[0]
		} else if ip, err = dohLookup(ctx, host); err != nil {
			// port 53 ucięty (firma/operator) i DoH też nie przeszło
			return nil, fmt.Errorf("dns fallback: %w", err)
		}
		var dl net.Dialer
		return dl.DialContext(ctx, network, net.JoinHostPort(ip, port))
	}
	ws, _, err := d.Dial(rawURL, nil)
	return ws, err
}

// dohLookup: DNS over HTTPS, gdy port 53 jest zablokowany. Pytamy 1.1.1.1 po IP
// (cert Cloudflare ma IP SAN), więc nie potrzeba DNS-u żeby zapytać o DNS.
func dohLookup(ctx context.Context, host string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET",
		"https://1.1.1.1/dns-query?type=A&name="+url.QueryEscape(host), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/dns-json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var v struct {
		Answer []struct {
			Type int    `json:"type"`
			Data string `json:"data"`
		} `json:"Answer"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&v); err != nil {
		return "", err
	}
	for _, a := range v.Answer {
		if a.Type == 1 && net.ParseIP(a.Data) != nil { // 1 = A
			return a.Data, nil
		}
	}
	return "", errors.New("doh: brak rekordu A dla " + host)
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
