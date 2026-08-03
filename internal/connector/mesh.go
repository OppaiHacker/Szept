package connector

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// Cebulion Network:
// Distributed mesh of connectors relaying signals for peers they do not host
// themselves (mesh_signal), discovered over SNDP (Szept Neighbor Discovery Protocol).
//
// The mesh is a *separate authenticated plane*: it only exists when the operator
// sets a shared secret (--mesh-secret). Without it, mesh_hello never succeeds,
// no socket ever becomes a mesh peer and both mesh operations are dead code.
// Client sockets must never reach these operations — see meshPeer in ServeHTTP.

const (
	// MaxNeighbors sets upper bound on stored neighbors in Cebulion Network (DoS / flood guard).
	MaxNeighbors = 64
	// maxJSONBytes limits response payload size from gossip endpoint /ndp/neighbors (OOM guard).
	maxJSONBytes = 128 << 10
	// maxSeenTraces caps the loop-detection map; past it we drop instead of growing.
	maxSeenTraces = 100_000
	// maxTraceIDLen: trace ids we mint are 16 hex chars, anything longer is hostile.
	maxTraceIDLen = 64
	// maxBroadcastFanout limits amplification when the target connector is unknown.
	maxBroadcastFanout = 4
	// meshContext domain-separates the mesh handshake MAC.
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
// are not enough: evil.example.com can resolve to 127.0.0.1, so the guard has to
// sit after DNS resolution, right before the socket is opened.
func safeDial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host)
	if ip == nil || !publicIP(ip) {
		return nil, errors.New("mesh: adres nie-publiczny: " + addr)
	}
	return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, addr)
}

var meshDialer = &websocket.Dialer{
	HandshakeTimeout: 3 * time.Second,
	NetDialContext:   safeDial,
}

var meshHTTP = &http.Client{
	Timeout: 5 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("mesh: przekierowania są zabronione")
	},
	Transport: &http.Transport{DialContext: safeDial},
}

// ponytail: unique TraceID to prevent routing loops in Cebulion Network
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
	// http/https are an internal detail of pingNode; a neighbour is always a socket
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

type NeighborInfo struct {
	URL        string `json:"url"`
	PeerCount  int    `json:"peer_count"`
	MaxClients int    `json:"max_clients"`
	PingMs     int64  `json:"ping_ms"`
}

type neighborNode struct {
	url       string
	peerCount int
	pingMs    int64
	lastSeen  time.Time
	ws        *websocket.Conn
	mu        sync.Mutex
}

type meshManager struct {
	mu         sync.Mutex
	server     *Server
	publicURL  string
	seeds      []string
	secret     []byte
	neighbors  map[string]*neighborNode // url -> node
	seenTraces map[string]time.Time     // trace_id -> timestamp
	started    bool
}

func newMeshManager(s *Server, publicURL string, seeds []string) *meshManager {
	m := &meshManager{
		server:     s,
		publicURL:  publicURL,
		seeds:      seeds,
		neighbors:  make(map[string]*neighborNode),
		seenTraces: make(map[string]time.Time),
	}
	return m
}

// enabled reports whether the mesh plane exists at all. No secret = no mesh.
func (m *meshManager) enabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.secret) > 0
}

// auth validates a mesh_hello frame: HMAC(secret, "szept-mesh-v1" || ts),
// timestamp within a minute of now.
func (m *meshManager) auth(ts int64, sig []byte) bool {
	m.mu.Lock()
	secret := m.secret
	m.mu.Unlock()
	if len(secret) == 0 || len(sig) != sha256.Size {
		return false
	}
	if d := time.Since(time.Unix(ts, 0)); d > time.Minute || d < -time.Minute {
		return false
	}
	return hmac.Equal(sig, meshMAC(secret, ts))
}

// helloMsg is the first frame we send on every outbound neighbour socket.
func (m *meshManager) helloMsg() Msg {
	m.mu.Lock()
	secret := m.secret
	m.mu.Unlock()
	ts := time.Now().Unix()
	return Msg{Op: "mesh_hello", TS: ts, Sig: meshMAC(secret, ts)}
}

// start launches the gossip loop once, and only for an enabled mesh.
func (m *meshManager) start() {
	m.mu.Lock()
	run := !m.started && len(m.secret) > 0 && (len(m.seeds) > 0 || m.publicURL != "")
	if run {
		m.started = true
	}
	m.mu.Unlock()
	if run {
		go m.loop()
	}
}

func (m *meshManager) activeNeighborURLs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	urls := make([]string, 0, len(m.neighbors))
	now := time.Now()
	for url, n := range m.neighbors {
		if now.Sub(n.lastSeen) < 2*time.Minute && url != m.publicURL {
			urls = append(urls, url)
		}
	}
	return urls
}

// neighborInfos snapshots neighbours for the /ndp/neighbors gossip endpoint.
func (m *meshManager) neighborInfos() []NeighborInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]NeighborInfo, 0, len(m.neighbors))
	now := time.Now()
	for url, n := range m.neighbors {
		if now.Sub(n.lastSeen) < 2*time.Minute && url != m.publicURL {
			out = append(out, NeighborInfo{URL: url, PeerCount: n.peerCount, PingMs: n.pingMs})
		}
	}
	return out
}

func (m *meshManager) addNeighbor(url string, peerCount int, pingMs int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !isValidNeighborURL(url, m.publicURL) {
		return
	}
	if len(m.neighbors) >= MaxNeighbors {
		if _, ok := m.neighbors[url]; !ok {
			return
		}
	}
	n, ok := m.neighbors[url]
	if !ok {
		n = &neighborNode{url: url}
		m.neighbors[url] = n
	}
	n.peerCount = peerCount
	if pingMs > 0 {
		n.pingMs = pingMs
	}
	n.lastSeen = time.Now()
}

// markTraceSeen reports whether this signal already passed through us.
// A missing, oversized or overflow trace is treated as "seen" — dropping a
// frame is always safer than growing an attacker-controlled map.
func (m *meshManager) markTraceSeen(traceID string) bool {
	if traceID == "" || len(traceID) > maxTraceIDLen {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, seen := m.seenTraces[traceID]; seen {
		return true
	}
	if len(m.seenTraces) >= maxSeenTraces {
		return true
	}
	m.seenTraces[traceID] = time.Now()
	return false
}

func (m *meshManager) cleanupTraces() {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := time.Now().Add(-5 * time.Minute)
	for id, t := range m.seenTraces {
		if t.Before(cutoff) {
			delete(m.seenTraces, id)
		}
	}
}

// loop runs background SNDP gossip and seed discovery.
func (m *meshManager) loop() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	m.gossipSeeds()

	for range ticker.C {
		m.cleanupTraces()
		m.gossipSeeds()
	}
}

func (m *meshManager) gossipSeeds() {
	allTargets := make(map[string]bool)
	m.mu.Lock()
	for _, s := range m.seeds {
		if s != m.publicURL {
			allTargets[s] = true
		}
	}
	for url := range m.neighbors {
		if url != m.publicURL {
			allTargets[url] = true
		}
	}
	m.mu.Unlock()

	for target := range allTargets {
		go m.pingNode(target)
	}
}

func (m *meshManager) pingNode(targetURL string) {
	var httpURL string
	switch {
	case strings.HasPrefix(targetURL, "wss://"):
		httpURL = "https://" + strings.TrimPrefix(targetURL, "wss://")
	case strings.HasPrefix(targetURL, "ws://"):
		httpURL = "http://" + strings.TrimPrefix(targetURL, "ws://")
	default:
		return // isValidNeighborURL only ever admits ws:// and wss://
	}

	req, err := http.NewRequest("GET", httpURL+"/ndp/neighbors", nil)
	if err != nil {
		return
	}
	startTime := time.Now()
	resp, err := meshHTTP.Do(req)
	rtt := time.Since(startTime).Milliseconds()
	if rtt <= 0 {
		rtt = 1
	}

	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return
	}

	var gossip struct {
		URL        string         `json:"url"`
		PeerCount  int            `json:"peer_count"`
		MaxClients int            `json:"max_clients"`
		Neighbors  []NeighborInfo `json:"neighbors"`
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJSONBytes)).Decode(&gossip); err != nil {
		return
	}

	m.addNeighbor(targetURL, gossip.PeerCount, rtt)
	for _, n := range gossip.Neighbors {
		m.addNeighbor(n.URL, n.PeerCount, n.PingMs)
	}
}

// relayMeshSignal forwards a signal towards peers we do not host ourselves.
// ponytail: no routing table — we do not know which connector holds the target,
// so we flood a bounded number of neighbours and let TTL + TraceID kill loops.
func (m *meshManager) relayMeshSignal(msg Msg) {
	if msg.TTL <= 0 || !m.enabled() {
		return
	}
	msg.TTL--
	if msg.TraceID == "" {
		msg.TraceID = newTraceID()
		if m.markTraceSeen(msg.TraceID) {
			return
		}
	}

	msg.Op = "mesh_signal"
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	hello := m.helloMsg()

	m.mu.Lock()
	me := m.publicURL
	targets := make([]*neighborNode, 0, maxBroadcastFanout)
	for _, n := range m.neighbors {
		if len(targets) >= maxBroadcastFanout {
			break
		}
		if time.Since(n.lastSeen) < 2*time.Minute {
			targets = append(targets, n)
		}
	}
	m.mu.Unlock()

	for _, n := range targets {
		go func(node *neighborNode) {
			node.mu.Lock()
			defer node.mu.Unlock()
			if node.ws == nil {
				ws, _, err := meshDialer.Dial(node.url, nil)
				if err != nil {
					return
				}
				node.ws = ws
				// authenticate the plane before saying anything else
				ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if err := ws.WriteJSON(hello); err != nil {
					ws.Close()
					node.ws = nil
					return
				}
				_ = ws.WriteJSON(Msg{Op: "ndp_gossip", From: []byte(me)})
			}
			node.ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := node.ws.WriteMessage(websocket.TextMessage, data); err != nil {
				node.ws.Close()
				node.ws = nil
			}
		}(n)
	}
}
