package connector

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Cebulion Network:
// Distributed mesh of connectors with remote signal routing (mesh_signal)
// based on Dijkstra algorithm with delay weights (Ping RTT) and SNDP (Szept Neighbor Discovery Protocol).

const (
	// MaxNeighbors sets upper bound on stored neighbors in Cebulion Network (DoS / flood guard).
	MaxNeighbors = 64
	// MaxTopologyNodes sets max nodes in BGP topology map (prevents O(V^2) DoS in Dijkstra).
	MaxTopologyNodes = 256
	// maxJSONBytes limits response payload size from gossip endpoint /ndp/neighbors (OOM guard).
	maxJSONBytes = 128 << 10
)

var meshDialer = &websocket.Dialer{
	HandshakeTimeout: 3 * time.Second,
}

// Unique TraceID to prevent routing loops in Cebulion Network
func newTraceID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func isValidNeighborURL(raw string, publicURL string) bool {
	if raw == "" || raw == publicURL || len(raw) > 128 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "ws" && scheme != "wss" && scheme != "http" && scheme != "https" {
		return false
	}
	host := u.Hostname()
	// Block cloud metadata addresses (169.254.169.254) and invalid IPs in Cebulion Network
	if host == "" || host == "169.254.169.254" || host == "0.0.0.0" {
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
	mu           sync.Mutex
	server       *Server
	publicURL    string
	seeds        []string
	neighbors    map[string]*neighborNode    // url -> node
	topology     map[string]map[string]int64 // nodeA -> nodeB -> RTT ms (BGP/Dijkstra graph in Cebulion Network)
	seenTraces   map[string]time.Time        // trace_id -> timestamp
	routingCache map[string]string           // pubkey -> target connector url
}

func newMeshManager(s *Server, publicURL string, seeds []string) *meshManager {
	m := &meshManager{
		server:       s,
		publicURL:    publicURL,
		seeds:        seeds,
		neighbors:    make(map[string]*neighborNode),
		topology:     make(map[string]map[string]int64),
		seenTraces:   make(map[string]time.Time),
		routingCache: make(map[string]string),
	}
	if len(seeds) > 0 || publicURL != "" {
		go m.loop()
	}
	return m
}

func (m *meshManager) updateTopologyEdge(from, to string, cost int64) {
	if from == "" || to == "" || from == to || len(from) > 128 || len(to) > 128 {
		return
	}
	if cost <= 0 {
		cost = 1
	} else if cost > 100000 {
		cost = 100000
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.topology) >= MaxTopologyNodes && m.topology[from] == nil {
		return
	}
	if _, ok := m.topology[from]; !ok {
		m.topology[from] = make(map[string]int64)
	}
	m.topology[from][to] = cost

	if len(m.topology) >= MaxTopologyNodes && m.topology[to] == nil {
		return
	}
	if _, ok := m.topology[to]; !ok {
		m.topology[to] = make(map[string]int64)
	}
	m.topology[to][from] = cost
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

func (m *meshManager) addNeighbor(url string, peerCount int, pingMs int64) {
	if !isValidNeighborURL(url, m.publicURL) {
		return
	}
	m.mu.Lock()
	if len(m.neighbors) >= MaxNeighbors {
		if _, ok := m.neighbors[url]; !ok {
			m.mu.Unlock()
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
	m.mu.Unlock()

	if pingMs > 0 {
		m.updateTopologyEdge(m.publicURL, url, pingMs)
	}
}

func (m *meshManager) markTraceSeen(traceID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if traceID == "" {
		return false
	}
	if _, seen := m.seenTraces[traceID]; seen {
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

// NextHopDijkstra calculates shortest path for latency weights (Ping RTT) from startURL to targetURL.
func (m *meshManager) NextHopDijkstra(start, target string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if start == target || target == "" || start == "" {
		return ""
	}

	dist := make(map[string]int64)
	prev := make(map[string]string)
	visited := make(map[string]bool)

	nodes := make(map[string]bool)
	nodes[start] = true
	nodes[target] = true
	for u, nbrs := range m.topology {
		nodes[u] = true
		for v := range nbrs {
			nodes[v] = true
		}
	}

	const inf int64 = 1e15
	for u := range nodes {
		dist[u] = inf
	}
	dist[start] = 0

	for i := 0; i < len(nodes); i++ {
		u := ""
		var minD int64 = inf
		for node := range nodes {
			if !visited[node] && dist[node] < minD {
				minD = dist[node]
				u = node
			}
		}

		if u == "" || u == target {
			break
		}
		visited[u] = true

		for v, weight := range m.topology[u] {
			if !visited[v] {
				alt := dist[u] + weight
				if alt < dist[v] {
					dist[v] = alt
					prev[v] = u
				}
			}
		}
	}

	curr := target
	var path []string
	for curr != "" {
		path = append([]string{curr}, path...)
		curr = prev[curr]
	}

	if len(path) >= 2 && path[0] == start {
		return path[1]
	}
	return ""
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
	httpURL := targetURL
	if len(httpURL) > 5 && httpURL[:5] == "wss://" {
		httpURL = "https://" + httpURL[6:]
	} else if len(httpURL) > 4 && httpURL[:4] == "ws://" {
		httpURL = "http://" + httpURL[5:]
	}
	if httpURL == "" {
		return
	}

	req, err := http.NewRequest("GET", httpURL+"/ndp/neighbors", nil)
	if err != nil {
		return
	}
	client := &http.Client{Timeout: 5 * time.Second}
	startTime := time.Now()
	resp, err := client.Do(req)
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
		URL        string                      `json:"url"`
		PeerCount  int                         `json:"peer_count"`
		MaxClients int                         `json:"max_clients"`
		Neighbors  []NeighborInfo              `json:"neighbors"`
		Topology   map[string]map[string]int64 `json:"topology"`
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJSONBytes)).Decode(&gossip); err != nil {
		return
	}

	m.addNeighbor(targetURL, gossip.PeerCount, rtt)
	for _, n := range gossip.Neighbors {
		m.addNeighbor(n.URL, n.PeerCount, n.PingMs)
	}

	// Merging BGP graphs with received link state in Cebulion Network
	for u, edges := range gossip.Topology {
		for v, w := range edges {
			m.updateTopologyEdge(u, v, w)
		}
	}
}

// relayMeshSignal forwards a signal to the optimal neighbor using Dijkstra or broadcasts if target unknown.
func (m *meshManager) relayMeshSignal(msg Msg, targetConnectorURL string) {
	if msg.TTL <= 0 {
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

	// Determining optimal Next-Hop using Dijkstra algorithm in Cebulion Network
	nextHopURL := ""
	if targetConnectorURL != "" {
		nextHopURL = m.NextHopDijkstra(m.publicURL, targetConnectorURL)
	}

	m.mu.Lock()
	targets := make([]*neighborNode, 0, len(m.neighbors))
	if nextHopURL != "" && m.neighbors[nextHopURL] != nil {
		targets = append(targets, m.neighbors[nextHopURL])
	} else {
		for _, n := range m.neighbors {
			if time.Since(n.lastSeen) < 2*time.Minute {
				targets = append(targets, n)
			}
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
				_ = ws.WriteJSON(Msg{Op: "ndp_gossip", From: []byte(m.publicURL)})
			}
			node.ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := node.ws.WriteMessage(websocket.TextMessage, data); err != nil {
				node.ws.Close()
				node.ws = nil
			}
		}(n)
	}
}
