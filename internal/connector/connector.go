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
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const registerContext = "szept-register-v1"

type Msg struct {
	Op      string `json:"op"` // register | lookup | signal | error | ok | ice | redirect | mesh_hello | ndp_gossip | mesh_signal | mesh_lookup | mesh_found
	Pub     []byte `json:"pub,omitempty"`
	To      []byte `json:"to,omitempty"`
	From    []byte `json:"from,omitempty"`
	TS      int64  `json:"ts,omitempty"`
	Sig     []byte `json:"sig,omitempty"`
	Payload []byte `json:"payload,omitempty"`
	Online  bool   `json:"online,omitempty"`
	Err     string `json:"err,omitempty"`
	ICE     *ICE   `json:"ice,omitempty"` // ok / ice reply: this connector's STUN+TURN relay

	// Cebulion Network: load balancing and the connector mesh
	Redirect []string `json:"redirect,omitempty"`
	TraceID  string   `json:"trace_id,omitempty"`
	TTL      int      `json:"ttl,omitempty"`
	Nodes    []string `json:"nodes,omitempty"`  // ndp_gossip reply: a few neighbour URLs
	Hidden   bool     `json:"hidden,omitempty"` // mesh_hello: do not pass my URL on (--private-gossip)
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
	ts int64 // registration timestamp holding slot — see "register"

	lookups, signals, meshFrames window // per-socket rate limits
}

// window counts events in the current minute.
type window struct {
	n     int
	start time.Time
}

// maxLookupsPerMinute throttles the presence oracle. `lookup` answers for any
// pubkey — the connector cannot tell who is entitled to ask about whom — so the
// best it can do is make enumerating the whole network expensive.
// maxSignalsPerMinute: a connection costs one offer + one answer; without a
// cap one socket could flood the mesh with signals to random keys.
const (
	maxLookupsPerMinute = 60
	maxSignalsPerMinute = 120
)

func (c *client) allow(w *window, max int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(w.start) > time.Minute {
		*w = window{start: time.Now()}
	}
	w.n++
	return w.n <= max
}

func (c *client) send(m Msg) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.ws.WriteJSON(m)
}

type Server struct {
	mu         sync.Mutex
	peers      map[string]*client
	ips        map[string]int // IP address -> open WS sessions
	up         websocket.Upgrader
	MaxPeers   int
	PublicURL  string
	TrustProxy bool // read the client IP from X-Forwarded-For (only behind our own proxy)
	mesh       *meshManager
	turn       *turnRelay // nil = no relay (see StartTURN)

	// Path: the only path that upgrades to the connector ("" = any). Everything
	// else — a probe, a crawler, a wrong guess — gets Decoy, so a censor that
	// scans the address sees an ordinary web server, not a Szept connector.
	Path  string
	Decoy http.Handler // nil = plain 404
}

// checkOrigin: the connector has no browser clients. Native clients send no
// Origin header at all, so a present Origin means a web page is dialing us —
// that is cross-site WebSocket hijacking, not a user.
func checkOrigin(r *http.Request) bool { return r.Header.Get("Origin") == "" }

func NewServer() *Server {
	s := &Server{
		peers: map[string]*client{},
		ips:   map[string]int{},
		up:    websocket.Upgrader{CheckOrigin: checkOrigin},
	}
	s.mesh = newMeshManager(s, "", nil)
	return s
}

func (s *Server) SetSeeds(seeds []string) {
	s.mesh.mu.Lock()
	s.mesh.seeds = seeds
	s.mesh.mu.Unlock()
	s.mesh.start()
}

func (s *Server) SetPublicURL(rawURL string) {
	s.mu.Lock()
	s.PublicURL = rawURL
	s.mu.Unlock()
	s.mesh.mu.Lock()
	s.mesh.publicURL = rawURL
	s.mesh.mu.Unlock()
	s.mesh.start()
}

// SetMeshSecret turns the open mesh into a private one: only nodes that know
// the secret are accepted as neighbours.
func (s *Server) SetMeshSecret(secret string) {
	s.mesh.mu.Lock()
	s.mesh.secret = []byte(secret)
	s.mesh.mu.Unlock()
	s.mesh.start()
}

// SetPrivateGossip hides this node from mesh walkers: it tells nobody about
// its neighbours (except mesh peers proven by --mesh-secret), sends no
// redirect lists, and asks its neighbours not to pass its own URL on. It
// still finds the mesh through --seeds and relays as usual.
func (s *Server) SetPrivateGossip() {
	s.mesh.mu.Lock()
	s.mesh.private = true
	s.mesh.mu.Unlock()
}

// SetMeshOff keeps this connector standalone: no neighbours, no relaying.
func (s *Server) SetMeshOff() {
	s.mesh.mu.Lock()
	s.mesh.off = true
	s.mesh.mu.Unlock()
}

// SetPath / SetDecoy: see Server.Path.
func (s *Server) SetPath(p string)        { s.Path = p }
func (s *Server) SetDecoy(h http.Handler) { s.Decoy = h }
func (s *Server) hosts(pub []byte) *client {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peers[string(pub)]
}

// SetTrustProxy makes clientIP honour X-Forwarded-For on loopback connections.
// Deployment decision, never a guess: without a proxy in front, any client can
// forge the header and dodge the per-IP limit.
func (s *Server) SetTrustProxy(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TrustProxy = v
}

func (s *Server) SetMaxPeers(max int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.MaxPeers = max
}

// MaxSessionsPerIP: Ed25519 keys are free, so pubkey limit achieves nothing —
// we count sessions per IP address.
const MaxSessionsPerIP = 64

// clientIP returns the address the per-IP limit applies to, "" when none can be
// established. Behind a reverse proxy every connection arrives from loopback;
// trusting X-Forwarded-For there is a deployment decision (--trust-proxy), not a
// default — exempting loopback unconditionally disabled the limit outright.
func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	s.mu.Lock()
	trust := s.TrustProxy
	s.mu.Unlock()
	if ip := net.ParseIP(host); trust && ip != nil && ip.IsLoopback() {
		// the last entry is the one our proxy appended; earlier ones come
		// from the client and can be anything
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			host = strings.TrimSpace(parts[len(parts)-1])
		}
	}
	if net.ParseIP(host) == nil {
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
	// no status page, no banner: anything but an upgrade on our path looks
	// like whatever the decoy is
	if (s.Path != "" && r.URL.Path != s.Path) || !websocket.IsWebSocketUpgrade(r) {
		if s.Decoy != nil {
			s.Decoy.ServeHTTP(w, r)
		} else {
			http.NotFound(w, r)
		}
		return
	}
	// per-IP slot first: the overload path below also upgrades the socket, so
	// taking the slot after it left MaxSessionsPerIP bypassable under load.
	ip := s.clientIP(r)
	if !s.takeSlot(ip) {
		http.Error(w, "too many connections", http.StatusTooManyRequests)
		return
	}
	defer s.freeSlot(ip)
	if s.MaxPeers > 0 && s.Count() >= s.MaxPeers {
		ws, err := s.up.Upgrade(w, r, nil)
		if err == nil {
			// a few neighbours, not all: a full list per connect is a free
			// census of the mesh
			ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
			_ = ws.WriteJSON(Msg{
				Op:       "redirect",
				Err:      "connector high load",
				Redirect: s.mesh.shareable(ip, redirectShare, false),
			})
			ws.Close()
		}
		return
	}
	ws, err := s.up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &client{ws: ws}
	var pub []byte
	// meshPeer stays false for every ordinary client socket, which is what keeps
	// mesh_signal / ndp_gossip off the client plane entirely.
	var meshPeer bool
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
		case "mesh_hello":
			if !s.mesh.auth(m.TS, m.Sig) {
				c.send(Msg{Op: "error", Err: "mesh auth"})
				continue
			}
			meshPeer = true
			// candidate only: it becomes a neighbour once it answers our dial.
			// Its hidden flag is taken from that answer, never from here: any
			// hello naming its URL could set or clear it.
			if len(m.From) > 0 {
				if n := s.mesh.addNeighbor(string(m.From)); n != nil {
					s.mesh.start()
				}
			}
			s.mesh.mu.Lock()
			private := s.mesh.private
			s.mesh.mu.Unlock()
			c.send(Msg{Op: "ok", Hidden: private})
		case "ndp_gossip", "mesh_signal", "mesh_lookup":
			// meshPeer stays false for every ordinary client socket, which
			// keeps these off the client plane
			if !meshPeer || !c.allow(&c.meshFrames, maxMeshFramesPerMinute) {
				continue
			}
			s.meshFrame(c, m, ip)
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
			old := s.peers[string(m.Pub)]
			pub = m.Pub
			c.ts = m.TS
			s.peers[string(pub)] = c
			s.mu.Unlock()
			if old != nil && old != c {
				old.ws.Close() // the key re-registered elsewhere; drop the stale socket
			}
			c.send(Msg{Op: "ok", ICE: s.iceFor(pub)})
		case "ice":
			// credentials expire after turnCredTTL; long sessions ask again
			if pub != nil {
				c.send(Msg{Op: "ice", ICE: s.iceFor(pub)})
			}
		case "lookup":
			if pub == nil {
				c.send(Msg{Op: "error", Err: "register first"})
				continue
			}
			if !c.allow(&c.lookups, maxLookupsPerMinute) {
				c.send(Msg{Op: "error", Err: "lookup rate limit", Pub: m.Pub})
				continue
			}
			if s.hosts(m.Pub) != nil || !s.mesh.enabled() {
				c.send(Msg{Op: "lookup", Pub: m.Pub, Online: s.hosts(m.Pub) != nil})
				continue
			}
			// not ours: ask the mesh, without stalling this socket's reads
			go func(pub []byte) {
				c.send(Msg{Op: "lookup", Pub: pub, Online: s.mesh.lookup(pub)})
			}(m.Pub)
		case "signal":
			if pub == nil {
				c.send(Msg{Op: "error", Err: "register first"})
				continue
			}
			if !c.allow(&c.signals, maxSignalsPerMinute) {
				c.send(Msg{Op: "error", Err: "signal rate limit"})
				continue
			}
			dst := s.hosts(m.To)
			if dst == nil {
				// not ours — hand it to the mesh, which reports back if
				// nobody took it
				go s.mesh.signal(c, pub, m.To, m.Payload)
				continue
			}
			dst.send(Msg{Op: "signal", From: pub, To: m.To, Payload: m.Payload})
		}
	}
}

// meshFrame handles one frame from an authenticated mesh socket.
func (s *Server) meshFrame(c *client, m Msg, ip string) {
	switch m.Op {
	case "ndp_gossip":
		c.send(Msg{Op: "ndp_gossip", Nodes: s.mesh.gossipFor(ip)})
	case "mesh_signal":
		if s.mesh.markTraceSeen(m.TraceID, c, m.To) {
			return
		}
		if dst := s.hosts(m.To); dst != nil {
			// `From` on a relayed frame is a routing hint, never an identity:
			// only the Ed25519 signature inside Payload (p2p.onSignal) proves
			// who sent it.
			dst.send(Msg{Op: "signal", From: m.From, To: m.To, Payload: m.Payload})
			// receipt: walks back like a lookup answer and teaches the route
			c.send(Msg{Op: "mesh_found", TraceID: m.TraceID, Pub: m.To})
			return
		}
		s.mesh.relay(m, m.To)
	case "mesh_lookup":
		if s.mesh.markTraceSeen(m.TraceID, c, m.Pub) {
			return
		}
		if s.hosts(m.Pub) != nil {
			c.send(Msg{Op: "mesh_found", TraceID: m.TraceID, Pub: m.Pub})
			return
		}
		s.mesh.relay(m, m.Pub)
	}
}

// ---- client ----

type Client struct {
	URL          string
	FallbackURLs []string
	Priv         ed25519.PrivateKey
	Pub          ed25519.PublicKey
	OnSignal     func(from []byte, payload []byte)
	// OnUndelivered: a signal to pub reached no connector hosting it.
	OnUndelivered func(pub []byte)
	OnUp          func() // fires after each successful (re-)registration
	OnDown        func() // fires when a registered session drops

	mu      sync.Mutex
	ws      *websocket.Conn
	waiters map[string][]chan Msg // pubkey -> Lookup calls waiting for its answer
	closed  bool
	up      bool
	lastErr string
	ice     *ICE // relay offered by the current connector
}

// ICE returns the STUN/TURN relay of the current connector, or nil.
func (c *Client) ICE() *ICE {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ice
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
	return &Client{URL: url, Priv: priv, Pub: priv.Public().(ed25519.PublicKey), waiters: map[string][]chan Msg{}}
}

// maxFallbacks caps the redirect list; a hostile connector must not be able to
// grow it without bound, since every session walks the whole list.
const maxFallbacks = 8

// sameScheme rejects a wss:// -> ws:// downgrade. Signalling stays end-to-end
// signed either way, but the metadata (who talks to whom, from which IP, when)
// would go out in the clear.
func sameScheme(current, next string) bool {
	cur, err1 := url.Parse(current)
	nxt, err2 := url.Parse(next)
	if err1 != nil || err2 != nil {
		return false
	}
	ns := strings.ToLower(nxt.Scheme)
	if strings.EqualFold(cur.Scheme, "wss") {
		return ns == "wss"
	}
	return ns == "ws" || ns == "wss"
}

// addFallbacks accepts a server-supplied redirect list under three rules:
// same-or-better scheme, no duplicates, hard cap.
func (c *Client) addFallbacks(urls []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range urls {
		if len(c.FallbackURLs) >= maxFallbacks || r == c.URL || !sameScheme(c.URL, r) {
			continue
		}
		if !slices.Contains(c.FallbackURLs, r) {
			c.FallbackURLs = append(c.FallbackURLs, r)
		}
	}
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
	c.mu.Lock()
	urlsToTry := append([]string{c.URL}, c.FallbackURLs...)
	c.mu.Unlock()
	var ws *websocket.Conn
	var dialErr error
	var ice *ICE

	ts := time.Now().Unix()
	regMsg := Msg{Op: "register", Pub: c.Pub, TS: ts, Sig: SignRegister(c.Priv, c.Pub, ts)}

	for _, u := range urlsToTry {
		var conn *websocket.Conn
		conn, _, dialErr = clientDialer.Dial(u, nil)
		if dialErr != nil && strings.Contains(dialErr.Error(), "no such host") {
			conn, dialErr = dialFallbackDNS(u)
		}
		if dialErr == nil {
			// same cap as the server side: a hostile or redirect-supplied
			// connector must not push unbounded frames into the client
			conn.SetReadLimit(256 << 10)
			// Send registration as first frame (per szept protocol specification)
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
					c.addFallbacks(ack.Redirect)
					continue
				} else if ack.Op == "ok" {
					conn.SetReadDeadline(time.Time{})
					ws = conn
					ice = ack.ICE
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
	c.ice = ice
	c.mu.Unlock()

	if ice != nil {
		stop := make(chan struct{})
		defer close(stop)
		go func() {
			t := time.NewTicker(turnCredTTL / 4)
			defer t.Stop()
			for {
				select {
				case <-stop:
					return
				case <-t.C:
					c.write(Msg{Op: "ice"})
				}
			}
		}()
	}

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
		case "undelivered":
			if c.OnUndelivered != nil {
				go c.OnUndelivered(m.Pub)
			}
		case "ice":
			c.mu.Lock()
			c.ice = m.ICE
			c.mu.Unlock()
		case "lookup", "error":
			// an answer goes to the Lookups that asked about this key; one
			// shared channel let concurrent lookups eat each other's replies
			c.mu.Lock()
			ws := c.waiters[string(m.Pub)]
			delete(c.waiters, string(m.Pub))
			c.mu.Unlock()
			for _, w := range ws {
				w <- m
			}
		}
	}
}

// dialFallbackDNS resolves the host via a public resolver and dials the IP,
// but keeps the original URL so TLS SNI and the Host header stay the hostname —
// replacing host with IP in URL broke wss:// (cert without IP SAN, bad routing).
// Fallback path only after "no such host"; hardcoded 1.1.1.1 is sufficient.
func dialFallbackDNS(rawURL string) (*websocket.Conn, error) {
	r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, "1.1.1.1:53")
	}}
	d := *clientDialer
	d.NetDialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ip := ""
		if addrs, err := r.LookupHost(ctx, host); err == nil && len(addrs) > 0 {
			ip = addrs[0]
		} else if ip, err = dohLookup(ctx, host); err != nil {
			// port 53 blocked (corporate/ISP) and DoH also failed
			return nil, fmt.Errorf("dns fallback: %w", err)
		}
		var dl net.Dialer
		return dl.DialContext(ctx, network, net.JoinHostPort(ip, port))
	}
	d.NetDialTLSContext = chromeTLS(d.NetDialContext) // SNI stays the hostname
	ws, _, err := d.Dial(rawURL, nil)
	return ws, err
}

// dohLookup: DNS over HTTPS when port 53 is blocked. Queries 1.1.1.1 by IP
// (Cloudflare cert has IP SAN), so DNS is not required to query DNS.
func dohLookup(ctx context.Context, host string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
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
	w := make(chan Msg, 1)
	c.mu.Lock()
	c.waiters[string(pub)] = append(c.waiters[string(pub)], w)
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.waiters[string(pub)] = slices.DeleteFunc(c.waiters[string(pub)], func(x chan Msg) bool { return x == w })
		if len(c.waiters[string(pub)]) == 0 {
			delete(c.waiters, string(pub))
		}
		c.mu.Unlock()
	}()
	if err := c.write(Msg{Op: "lookup", Pub: pub}); err != nil {
		return false, err
	}
	select {
	case m := <-w:
		if m.Op == "error" {
			return false, errors.New(m.Err)
		}
		return m.Online, nil
	case <-time.After(timeout):
		return false, errors.New("lookup timeout")
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
