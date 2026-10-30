package connector

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"szept/internal/crypt"
)

// Cebulion Network:
// an open mesh of volunteer connectors. Anyone can run one, on anything, and
// join by pointing --seeds at a node they know. A node relays signals for peers
// it does not host (mesh_signal) and answers whether it hosts a key
// (mesh_lookup), so two clients on different connectors can still find and
// reach each other.
//
// Open means no shared password: in a network where anyone may run a node, the
// censor would have it too. A hostile node is contained instead: every frame is
// end-to-end signed by the clients, every mesh socket is rate limited, floods
// go to several neighbours so one node cannot blackhole a signal, and nobody
// gets the full node list (shareFor). --mesh-secret still exists for operators
// who want a private mesh among nodes they trust.
//
// The whole plane runs over WebSocket on the connector's secret path — no
// HTTP endpoint gives the mesh away to a probe.

const (
	// MaxNeighbors caps the neighbour table (DoS / flood guard).
	MaxNeighbors = 64
	// maxSeenTraces sizes the loop-detection ring. Past it the oldest trace is
	// forgotten — a flood costs us duplicates, it can no longer stop relaying.
	maxSeenTraces = 20_000
	// maxTraceIDLen: trace ids we mint are 16 hex chars, anything longer is hostile.
	maxTraceIDLen = 64
	// maxBroadcastFanout limits amplification when the target connector is unknown.
	maxBroadcastFanout = 4
	// meshTTL: hops a signal or lookup may travel.
	meshTTL = 5
	// gossipShare / redirectShare: how many node URLs one asker ever gets.
	gossipShare   = 8
	redirectShare = 3
	// maxGossipBlocks: network blocks (see netBlock) one node answers gossip
	// for within gossipBlockTTL. Neighbours re-ask every 15 s and keep their
	// slot; a censor walking the mesh from many blocks runs out of slots.
	maxGossipBlocks = 32
	gossipBlockTTL  = time.Hour
	// maxMeshFramesPerMinute throttles one mesh socket.
	maxMeshFramesPerMinute = 1200
	// neighborQueue: frames waiting for one neighbour; a full queue drops,
	// the flood's other branches still carry the frame.
	neighborQueue = 64
	// routeTTL: how long "key K lives behind neighbour N" is trusted.
	routeTTL = 5 * time.Minute
	// maxRoutes caps the route table; past it the table is reset.
	maxRoutes = 10_000
	// dialBackoff: a neighbour whose dial failed is not redialled by the
	// gossip loop for this long, instead of a new dial every 15 s.
	dialBackoff = 2 * time.Minute
	// lookupWait: how long a mesh lookup or signal waits for mesh_found.
	lookupWait = 2 * time.Second
	// meshContext domain-separates the private-mesh handshake MAC.
	meshContext = "szept-mesh-v1"
)

// allowPrivateMeshHosts loosens the SSRF guard so mesh tests can run against
// httptest servers on loopback. Never set outside tests; atomic because test
// cleanup flips it while connector goroutines are still serving.
var allowPrivateMeshHosts atomic.Bool

// publicIP reports whether an address is routable on the public internet.
// Everything else (loopback, RFC1918, link-local incl. cloud metadata,
// multicast, unspecified) is off limits to the mesh.
func publicIP(ip net.IP) bool {
	if allowPrivateMeshHosts.Load() {
		return true
	}
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast())
}

// safeDial refuses to connect to non-public addresses. String checks on the URL
// are not enough: evil.example.com can resolve to 127.0.0.1, so the guard sits
// after DNS resolution, and we dial the address we checked — a second lookup
// could rebind. Every answer must be public, not just the first.
func safeDial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return nil, errors.New("mesh: adres nie-publiczny: " + addr)
		}
	}
	return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
}

var meshDialer = &websocket.Dialer{
	HandshakeTimeout:  3 * time.Second,
	NetDialContext:    safeDial,
	NetDialTLSContext: chromeTLS(safeDial),
}

func newTraceID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func meshMAC(secret []byte, ts int64) []byte {
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(meshContext))
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(ts))
	h.Write(b[:])
	return h.Sum(nil)
}

func isValidNeighborURL(raw string, publicURL string) bool {
	if raw == "" || raw == publicURL || len(raw) > 128 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if scheme := strings.ToLower(u.Scheme); scheme != "ws" && scheme != "wss" {
		return false
	}
	host := u.Hostname()
	if host == "" {
		return false
	}
	// literal non-public address is rejected here; a hostname is re-checked
	// after DNS resolution in safeDial
	if ip := net.ParseIP(host); ip != nil && !publicIP(ip) {
		return false
	}
	return true
}

// shareSalt keys shareFor, so nobody outside this process can work out which
// network block would be shown which nodes.
var shareSalt = string(crypt.RandBytes(16))

// netBlock: the asker's /16 (IPv4) or /48 (IPv6).
func netBlock(askerIP string) string {
	if ip := net.ParseIP(askerIP); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			return string(v4[:2])
		}
		return string(ip[:6])
	}
	return askerIP
}

// shareFor picks up to n URLs for one asker. The same network block always
// gets the same few, so walking the mesh from one block shows a handful of
// nodes, not the whole list a censor would block.
func shareFor(urls []string, askerIP string, n int) []string {
	bucket := netBlock(askerIP)
	rank := func(u string) []byte {
		h := sha256.Sum256([]byte(shareSalt + bucket + "\x00" + u))
		return h[:]
	}
	slices.SortFunc(urls, func(a, b string) int { return bytes.Compare(rank(a), rank(b)) })
	return urls[:min(n, len(urls))]
}

type neighborNode struct {
	url      string
	added    time.Time
	lastSeen time.Time   // last frame it sent us; zero = candidate, never reached
	hidden   bool        // asked us not to pass its URL on (--private-gossip)
	failed   time.Time   // last failed dial; the loop backs off (dialBackoff)
	out      chan []byte // send queue of the open socket, nil = none
}

type route struct {
	node *neighborNode
	at   time.Time
}

type meshManager struct {
	mu        sync.Mutex
	server    *Server
	publicURL string
	seeds     []string
	secret    []byte // optional: private mesh
	off       bool
	private   bool                     // --private-gossip
	neighbors map[string]*neighborNode // url -> node

	traces   map[string]*client // trace id -> socket it came in on (nil = ours)
	traceLog []string           // ring over traces, oldest evicted
	traceAt  int
	waits    map[string]chan struct{} // our own lookups waiting for mesh_found
	routes   map[string]route         // pubkey -> neighbour that answered for it
	gossiped map[string]time.Time     // network block -> last gossip answer (maxGossipBlocks)
	started  bool
}

func newMeshManager(s *Server, publicURL string, seeds []string) *meshManager {
	return &meshManager{
		server:    s,
		publicURL: publicURL,
		seeds:     seeds,
		neighbors: map[string]*neighborNode{},
		traces:    map[string]*client{},
		waits:     map[string]chan struct{}{},
		routes:    map[string]route{},
		gossiped:  map[string]time.Time{},
	}
}

func (m *meshManager) enabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.off
}

// auth validates a mesh_hello. Open mesh: always. Private mesh:
// HMAC(secret, "szept-mesh-v1" || ts), timestamp within a minute of now.
func (m *meshManager) auth(ts int64, sig []byte) bool {
	m.mu.Lock()
	secret, off := m.secret, m.off
	m.mu.Unlock()
	if off {
		return false
	}
	if len(secret) == 0 {
		return true
	}
	if len(sig) != sha256.Size {
		return false
	}
	if d := time.Since(time.Unix(ts, 0)); d > time.Minute || d < -time.Minute {
		return false
	}
	return hmac.Equal(sig, meshMAC(secret, ts))
}

// helloMsg opens every outbound neighbour socket; From tells the far side
// where to dial us back.
func (m *meshManager) helloMsg() Msg {
	m.mu.Lock()
	secret, me := m.secret, m.publicURL
	m.mu.Unlock()
	ts := time.Now().Unix()
	m.mu.Lock()
	hidden := m.private
	m.mu.Unlock()
	msg := Msg{Op: "mesh_hello", TS: ts, From: []byte(me), Hidden: hidden}
	if len(secret) > 0 {
		msg.Sig = meshMAC(secret, ts)
	}
	return msg
}

// start launches the gossip loop once.
func (m *meshManager) start() {
	m.mu.Lock()
	run := !m.started && !m.off
	m.started = m.started || run
	m.mu.Unlock()
	if run {
		go m.loop()
	}
}

func active(n *neighborNode) bool { return time.Since(n.lastSeen) < 2*time.Minute }

// shareable: up to n neighbour URLs to tell someone at ip about. None from
// a --private-gossip node, unless the asker is a mesh peer proven by
// --mesh-secret (meshPeer). Neighbours that asked to stay hidden are never
// passed on.
func (m *meshManager) shareable(ip string, n int, meshPeer bool) []string {
	m.mu.Lock()
	if m.private && !(meshPeer && len(m.secret) > 0) {
		m.mu.Unlock()
		return nil
	}
	var urls []string
	for u, nb := range m.neighbors {
		if active(nb) && !nb.hidden {
			urls = append(urls, u)
		}
	}
	m.mu.Unlock()
	return shareFor(urls, ip, n)
}

// addNeighbor records a candidate. It only becomes active once it answers our
// own socket, so gossip cannot fill the table with live-looking junk; when the
// table is full a never-reached or long-silent entry makes room.
func (m *meshManager) addNeighbor(u string) *neighborNode {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !isValidNeighborURL(u, m.publicURL) {
		return nil
	}
	if n, ok := m.neighbors[u]; ok {
		return n
	}
	if len(m.neighbors) >= MaxNeighbors {
		for old, n := range m.neighbors {
			if !active(n) {
				m.drop(old)
				break
			}
		}
		if len(m.neighbors) >= MaxNeighbors {
			return nil
		}
	}
	n := &neighborNode{url: u, added: time.Now()}
	m.neighbors[u] = n
	return n
}

// drop forgets a neighbour; caller holds m.mu. Closing the queue ends its socket.
func (m *meshManager) drop(u string) {
	if n := m.neighbors[u]; n != nil && n.out != nil {
		close(n.out)
		n.out = nil
	}
	delete(m.neighbors, u)
}

// markTraceSeen reports whether a frame already passed through us, and
// remembers which socket it came from so mesh_found can walk back.
func (m *meshManager) markTraceSeen(id string, from *client) bool {
	if id == "" || len(id) > maxTraceIDLen {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, seen := m.traces[id]; seen {
		return true
	}
	if len(m.traceLog) < maxSeenTraces {
		m.traceLog = append(m.traceLog, id)
	} else {
		delete(m.traces, m.traceLog[m.traceAt])
		m.traceLog[m.traceAt] = id
		m.traceAt = (m.traceAt + 1) % maxSeenTraces
	}
	m.traces[id] = from
	return false
}

// loop: every 15 s ask each neighbour (and seed) for gossip, which also keeps
// the socket alive and proves the node is up; forget nodes silent for 10 min.
func (m *meshManager) loop() {
	req, _ := json.Marshal(Msg{Op: "ndp_gossip"})
	for {
		m.mu.Lock()
		seeds := m.seeds
		m.mu.Unlock()
		for _, s := range seeds {
			m.addNeighbor(s)
		}
		m.mu.Lock()
		for u, n := range m.neighbors {
			if time.Since(n.lastSeen) > 10*time.Minute && time.Since(n.added) > 10*time.Minute {
				m.drop(u)
				continue
			}
			if n.out == nil && time.Since(n.failed) < dialBackoff {
				continue
			}
			m.sendLocked(n, req)
		}
		for k, r := range m.routes {
			if time.Since(r.at) > routeTTL {
				delete(m.routes, k)
			}
		}
		m.mu.Unlock()
		time.Sleep(15 * time.Second)
	}
}

// sendLocked queues a frame for n, opening the socket if needed; caller holds
// m.mu. A full queue drops the frame.
func (m *meshManager) sendLocked(n *neighborNode, data []byte) {
	if n.out == nil {
		n.out = make(chan []byte, neighborQueue)
		go m.run(n, n.out)
	}
	select {
	case n.out <- data:
	default:
	}
}

// run owns one outbound neighbour socket: a writer draining out and a reader
// for replies (ok, gossip, mesh_found). The old code never read this socket,
// so the far side's read deadline killed it every 90 s.
func (m *meshManager) run(n *neighborNode, out chan []byte) {
	defer func() {
		m.mu.Lock()
		if n.out == out {
			n.out = nil
		}
		m.mu.Unlock()
	}()
	ws, _, err := meshDialer.Dial(n.url, nil)
	if err != nil {
		m.mu.Lock()
		n.failed = time.Now()
		m.mu.Unlock()
		return
	}
	defer ws.Close()
	ws.SetReadLimit(256 << 10)
	ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if ws.WriteJSON(m.helloMsg()) != nil {
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			ws.SetReadDeadline(time.Now().Add(90 * time.Second))
			var msg Msg
			if ws.ReadJSON(&msg) != nil || msg.Op == "error" {
				return
			}
			m.fromNeighbor(n, msg)
		}
	}()
	for {
		select {
		case data, ok := <-out:
			if !ok {
				return
			}
			ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if ws.WriteMessage(websocket.TextMessage, data) != nil {
				return
			}
		case <-done:
			return
		}
	}
}

func (m *meshManager) fromNeighbor(n *neighborNode, msg Msg) {
	m.mu.Lock()
	n.lastSeen = time.Now()
	m.mu.Unlock()
	switch msg.Op {
	case "ndp_gossip":
		for _, u := range msg.Nodes[:min(len(msg.Nodes), gossipShare)] {
			m.addNeighbor(u)
		}
	case "mesh_found":
		m.found(n, msg)
	}
}

// found walks a mesh_found back towards whoever asked. Only for a lookup that
// went through us — an unsolicited answer could steer routes to a blackhole.
func (m *meshManager) found(n *neighborNode, msg Msg) {
	m.mu.Lock()
	origin, ok := m.traces[msg.TraceID]
	w := m.waits[msg.TraceID]
	if ok && len(msg.Pub) > 0 {
		if len(m.routes) >= maxRoutes {
			clear(m.routes)
		}
		m.routes[string(msg.Pub)] = route{n, time.Now()}
	}
	m.mu.Unlock()
	switch {
	case !ok:
	case w != nil:
		select {
		case w <- struct{}{}:
		default:
		}
	case origin != nil:
		origin.send(Msg{Op: "mesh_found", TraceID: msg.TraceID, Pub: msg.Pub})
	}
}

// gossipFor: the nodes to tell a mesh peer at ip about, or none once
// maxGossipBlocks other network blocks were answered within gossipBlockTTL.
func (m *meshManager) gossipFor(ip string) []string {
	b := netBlock(ip)
	m.mu.Lock()
	now := time.Now()
	for k, t := range m.gossiped {
		if now.Sub(t) > gossipBlockTTL {
			delete(m.gossiped, k)
		}
	}
	_, known := m.gossiped[b]
	if !known && len(m.gossiped) >= maxGossipBlocks {
		m.mu.Unlock()
		return nil
	}
	m.gossiped[b] = now
	m.mu.Unlock()
	return m.shareable(ip, gossipShare, true)
}

// lookup asks the mesh whether some other connector hosts pub.
func (m *meshManager) lookup(pub []byte) bool {
	return m.ask(Msg{Op: "mesh_lookup", Pub: pub}, pub, maxBroadcastFanout)
}

// signal carries a client's signal to whichever connector hosts to. That
// connector answers with mesh_found as a receipt. Without one the signal goes
// out once more to every neighbour, and if that fails too the client hears
// "undelivered", so it can retry later instead of waiting for an answer that
// will never come.
func (m *meshManager) signal(c *client, from, to, payload []byte) {
	msg := Msg{Op: "mesh_signal", From: from, To: to, Payload: payload}
	if m.enabled() && (m.ask(msg, to, maxBroadcastFanout) || m.ask(msg, to, MaxNeighbors)) {
		return
	}
	c.send(Msg{Op: "undelivered", Pub: to})
}

// ask sends msg under a fresh trace and waits for its mesh_found.
func (m *meshManager) ask(msg Msg, key []byte, fanout int) bool {
	id := newTraceID()
	w := make(chan struct{}, 1)
	m.mu.Lock()
	if len(m.neighbors) == 0 { // standalone: nobody to ask, answer now
		m.mu.Unlock()
		return false
	}
	m.waits[id] = w
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.waits, id)
		m.mu.Unlock()
	}()
	if m.markTraceSeen(id, nil) {
		return false
	}
	msg.TraceID, msg.TTL = id, meshTTL
	m.relayN(msg, key, fanout)
	select {
	case <-w:
		return true
	case <-time.After(lookupWait):
		return false
	}
}

// relay forwards a mesh frame about key: to the neighbour that last answered
// for that key, plus a bounded flood so a lying neighbour cannot swallow it.
// TTL and the trace ring kill loops.
func (m *meshManager) relay(msg Msg, key []byte) { m.relayN(msg, key, maxBroadcastFanout) }

func (m *meshManager) relayN(msg Msg, key []byte, fanout int) {
	if msg.TTL <= 0 || !m.enabled() {
		return
	}
	msg.TTL--
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var targets []*neighborNode
	if r, ok := m.routes[string(key)]; ok && time.Since(r.at) < routeTTL && m.neighbors[r.node.url] == r.node {
		targets = append(targets, r.node)
	}
	// active neighbours first, candidates only to fill up
	limit := len(targets) + fanout
	for _, wantActive := range []bool{true, false} {
		for _, n := range m.neighbors {
			if len(targets) >= limit {
				break
			}
			if active(n) == wantActive && !slices.Contains(targets, n) {
				targets = append(targets, n)
			}
		}
	}
	for _, n := range targets {
		m.sendLocked(n, data)
	}
}
