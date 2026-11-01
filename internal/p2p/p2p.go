// Package p2p: direct peer links over WebRTC data channels.
// NAT traversal via ICE/STUN (torrent-style hole punching); signaling blobs
// travel through the connector sealed to the addressee (seal.go) and
// Ed25519-signed end-to-end, verified against the expected peer key, so a
// malicious connector can neither read them nor MITM the DTLS handshake. Result: every data channel is an authenticated E2E pipe.
package p2p

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"

	"szept/internal/connector"
	"szept/internal/crypt"
)

var DefaultSTUN = []string{"stun:stun.l.google.com:19302", "stun:stun1.l.google.com:19302"}

type sigPayload struct {
	Kind   string `json:"kind"` // offer | answer
	SDP    string `json:"sdp"`
	From   []byte `json:"from"`
	To     []byte `json:"to"`
	TS     int64  `json:"ts"`
	ConnID []byte `json:"connid"`
	Obf    []byte `json:"obf,omitempty"` // 32B ChaCha20 key for wire obfuscation; offerer picks it
	Inv    []byte `json:"inv,omitempty"` // sha256 of the invite secret: lets a stranger redeeming our invite in
}

type sigEnvelope struct {
	Payload []byte `json:"payload"`
	Sig     []byte `json:"sig"`
}

type Peer struct {
	Pub  []byte
	pc   *webrtc.PeerConnection
	obf  *obfNet // nil without obfuscation
	ctrl *webrtc.DataChannel

	mu    sync.Mutex
	media map[string]*webrtc.DataChannel // label -> open channel
	open  map[string]bool
}

// SendCtrl sends a JSON control message over the reliable channel.
func (p *Peer) SendCtrl(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if p.ctrl == nil {
		return errors.New("ctrl channel not open")
	}
	return p.ctrl.Send(b)
}

// SendRaw sends bytes as they are on the reliable channel. Used for binary
// frames the receiver tells apart from JSON by their first byte.
func (p *Peer) SendRaw(b []byte) error {
	if p.ctrl == nil {
		return errors.New("ctrl channel not open")
	}
	return p.ctrl.Send(b)
}

// BufferedCtrl reports how many bytes are still queued on the reliable
// channel. Big transfers (file blobs) throttle on it instead of stuffing the
// whole file into pion's send buffer at once.
func (p *Peer) BufferedCtrl() uint64 {
	if p.ctrl == nil {
		return 0
	}
	return p.ctrl.BufferedAmount()
}

// SendMedia sends a frame over an unreliable, unordered channel with the
// given label, creating it on first use. Frames sent before the channel
// opens are dropped (fine for audio/video).
// MediaBacklog: bytes queued on a media channel and not yet sent. Video
// skips whole frames while it is high, so a slow link shows fewer pictures
// instead of ever older ones.
func (p *Peer) MediaBacklog(label string) uint64 {
	p.mu.Lock()
	dc := p.media[label]
	p.mu.Unlock()
	if dc == nil {
		return 0
	}
	return dc.BufferedAmount()
}

func (p *Peer) SendMedia(label string, frame []byte) {
	p.mu.Lock()
	dc, ok := p.media[label]
	if !ok {
		f := false
		zero := uint16(0)
		var err error
		dc, err = p.pc.CreateDataChannel(label, &webrtc.DataChannelInit{Ordered: &f, MaxRetransmits: &zero})
		if err != nil {
			p.mu.Unlock()
			return
		}
		lbl := label
		dc.OnOpen(func() {
			p.mu.Lock()
			p.open[lbl] = true
			p.mu.Unlock()
		})
		p.media[label] = dc
	}
	ready := p.open[label]
	p.mu.Unlock()
	if ready {
		dc.Send(frame)
	}
}

func (p *Peer) CloseMedia(prefix string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for lbl, dc := range p.media {
		if len(lbl) >= len(prefix) && lbl[:len(prefix)] == prefix {
			dc.Close()
			delete(p.media, lbl)
			delete(p.open, lbl)
		}
	}
}

func (p *Peer) Close() { p.pc.Close() }

// Relay returns the TURN relay address the link runs through (ours or the
// peer's), or "" for a direct link. Whoever runs that relay sees who talks to
// whom, when and how much; the content stays DTLS-encrypted end to end.
func (p *Peer) Relay() string {
	sctp := p.pc.SCTP()
	if sctp == nil {
		return ""
	}
	pair, err := sctp.Transport().ICETransport().GetSelectedCandidatePair()
	if err != nil || pair == nil {
		return ""
	}
	if pair.Local.Typ == webrtc.ICECandidateTypeRelay {
		return pair.Local.Address
	}
	if pair.Remote.Typ == webrtc.ICECandidateTypeRelay {
		return pair.Remote.Address
	}
	return ""
}

type Manager struct {
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey
	conn *connector.Client
	stun []string // configured STUN servers (nil = connector's, else DefaultSTUN)

	// Accept decides whether we talk to this pubkey at all (friend, chat
	// co-member or holder of an invite ticket). Trust boundary — default deny.
	Accept func(pub, ticket []byte) bool
	// Ticket returns the invite ticket to put in an offer to pub, or nil.
	Ticket     func(pub []byte) []byte
	OnPeerUp   func(p *Peer)
	OnPeerDown func(pub []byte)
	OnCtrl     func(p *Peer, data []byte)
	OnMedia    func(p *Peer, label string, data []byte)

	mu      sync.Mutex
	peers   map[string]*Peer
	pending map[string][]byte // pub -> connID of our outgoing offer
	dialing map[string]*Peer  // pub -> peer awaiting answer
	// answering: pub -> our PeerConnection answering their latest offer. A
	// newer offer closes the older one, so a flood costs one PC per key.
	answering map[string]*webrtc.PeerConnection
	seen      map[string]time.Time // replayed signaling blobs, see fresh()
	// relayOnly: gather only TURN relay candidates, so peers never learn our
	// IP — they see the connector's. Costs latency; no relay = no connection.
	relayOnly bool
}

// sigTTL: same as freshness window of signed blob.
const sigTTL = 2 * time.Minute

// fresh filters out replays: signed offer is valid for 2 minutes, and during
// that time connector (or anyone on ws://) can replay it repeatedly —
// each copy would spin up a new PeerConnection with ICE gathering.
func (m *Manager) fresh(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if t, ok := m.seen[key]; ok && now.Sub(t) < sigTTL {
		return false
	}
	for k, t := range m.seen {
		if now.Sub(t) >= sigTTL {
			delete(m.seen, k)
		}
	}
	m.seen[key] = now
	return true
}

func NewManager(priv ed25519.PrivateKey, conn *connector.Client, stun []string) *Manager {
	m := &Manager{
		priv: priv, pub: priv.Public().(ed25519.PublicKey), conn: conn, stun: stun,
		peers: map[string]*Peer{}, pending: map[string][]byte{}, dialing: map[string]*Peer{},
		answering: map[string]*webrtc.PeerConnection{}, seen: map[string]time.Time{},
	}
	conn.OnSignal, conn.OnUndelivered = m.onSignal, m.onUndelivered
	return m
}

// SetRelayOnly applies to connections made from now on.
func (m *Manager) SetRelayOnly(on bool) {
	m.mu.Lock()
	m.relayOnly = on
	m.mu.Unlock()
}

// SetConn swaps the connector client (live connector change in settings).
func (m *Manager) SetConn(c *connector.Client) {
	m.mu.Lock()
	m.conn = c
	m.mu.Unlock()
	c.OnSignal, c.OnUndelivered = m.onSignal, m.onUndelivered
}

// onUndelivered: the connector says our offer reached nobody, so drop the
// attempt now instead of waiting 30 s for an answer; the next connect round
// tries again.
func (m *Manager) onUndelivered(pub []byte) {
	m.mu.Lock()
	peer := m.dialing[string(pub)]
	m.mu.Unlock()
	if peer != nil {
		peer.pc.Close() // the state handler clears pending and dialing
	}
}

func (m *Manager) signal(to, blob []byte) error {
	m.mu.Lock()
	c := m.conn
	m.mu.Unlock()
	return c.Signal(to, blob)
}

func (m *Manager) Peer(pub []byte) *Peer {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.peers[string(pub)]
}

func (m *Manager) Peers() []*Peer {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Peer, 0, len(m.peers))
	for _, p := range m.peers {
		out = append(out, p)
	}
	return out
}

// sign signs the payload and seals it to p.To (see seal.go).
func (m *Manager) sign(p sigPayload) ([]byte, error) {
	b, _ := json.Marshal(p)
	env := sigEnvelope{Payload: b, Sig: ed25519.Sign(m.priv, b)}
	return sealTo(p.To, connector.JSON(env))
}

// iceConfig: the current connector's STUN+TURN when it runs one (no third
// party learns we use Szept), plus the configured STUN list; Google's only
// when there is nothing else. The URLs feed the obfuscation bypass.
func (m *Manager) iceConfig() (webrtc.Configuration, []string) {
	m.mu.Lock()
	c, relayOnly := m.conn, m.relayOnly
	m.mu.Unlock()
	var cfg webrtc.Configuration
	var urls []string
	if r := c.ICE(); r != nil {
		cfg.ICEServers = append(cfg.ICEServers, webrtc.ICEServer{URLs: r.URLs, Username: r.User, Credential: r.Pass})
		urls = append(urls, r.URLs...)
	}
	stun := m.stun
	if len(stun) == 0 && len(urls) == 0 {
		stun = DefaultSTUN
	}
	if len(stun) > 0 {
		cfg.ICEServers = append(cfg.ICEServers, webrtc.ICEServer{URLs: stun})
		urls = append(urls, stun...)
	}
	if relayOnly {
		cfg.ICETransportPolicy = webrtc.ICETransportPolicyRelay
	}
	return cfg, urls
}

// newPC builds a PeerConnection whose UDP transport obfuscates every
// peer-bound datagram with the shared key. mDNS is disabled so no cleartext
// .local multicast leaks onto the wire alongside the obfuscated flow.
func (m *Manager) newPC(key []byte) (*webrtc.PeerConnection, *obfNet, error) {
	cfg, urls := m.iceConfig()
	se := webrtc.SettingEngine{}
	se.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	var n *obfNet
	if len(key) == crypt.KeySize {
		var err error
		if n, err = newObfNet(key, stunHostSet(urls)); err != nil {
			return nil, nil, err
		}
		se.SetNet(n)
	}
	pc, err := webrtc.NewAPI(webrtc.WithSettingEngine(se)).NewPeerConnection(cfg)
	return pc, n, err
}

// Connect dials a peer (idempotent: returns existing link if up).
func (m *Manager) Connect(pub []byte) error {
	if bytes.Equal(pub, m.pub) {
		return errors.New("cannot connect to self")
	}
	m.mu.Lock()
	if _, up := m.peers[string(pub)]; up {
		m.mu.Unlock()
		return nil
	}
	if _, dialing := m.pending[string(pub)]; dialing {
		m.mu.Unlock()
		return nil
	}
	connID := crypt.RandBytes(8)
	obfKey := crypt.NewKey()
	m.pending[string(pub)] = connID
	m.mu.Unlock()

	fail := func(err error) error {
		m.mu.Lock()
		delete(m.pending, string(pub))
		delete(m.dialing, string(pub))
		m.mu.Unlock()
		return err
	}

	pc, onet, err := m.newPC(obfKey)
	if err != nil {
		return fail(err)
	}
	peer := m.newPeer(pub, pc)
	peer.obf = onet
	ctrl, err := pc.CreateDataChannel("ctrl", nil)
	if err != nil {
		pc.Close()
		return fail(err)
	}
	m.wireCtrl(peer, ctrl)

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		pc.Close()
		return fail(err)
	}
	done := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		pc.Close()
		return fail(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		pc.Close()
		return fail(errors.New("ICE gathering timeout"))
	}
	payload := sigPayload{
		Kind: "offer", SDP: pc.LocalDescription().SDP,
		From: m.pub, To: pub, TS: connector.Now().Unix(), ConnID: connID, Obf: obfKey,
	}
	if m.Ticket != nil {
		payload.Inv = m.Ticket(pub)
	}
	// remember pc for the answer before signalling: an answer or an
	// "undelivered" can come back before signal returns
	m.mu.Lock()
	m.dialing[string(pub)] = peer
	m.mu.Unlock()
	blob, err := m.sign(payload)
	if err == nil {
		err = m.signal(pub, blob)
	}
	if err != nil {
		pc.Close()
		return fail(err)
	}
	// answer arrives via onSignal; timeout cleanup
	go func() {
		time.Sleep(30 * time.Second)
		m.mu.Lock()
		id, still := m.pending[string(pub)]
		up := m.peers[string(pub)] != nil
		m.mu.Unlock()
		if still && bytes.Equal(id, connID) && !up {
			m.mu.Lock()
			delete(m.pending, string(pub))
			m.mu.Unlock()
			pc.Close()
		}
	}()
	return nil
}

func (m *Manager) onSignal(from []byte, blob []byte) {
	plain, err := openFrom(m.priv, blob)
	if err != nil {
		return
	}
	var env sigEnvelope
	if err := json.Unmarshal(plain, &env); err != nil {
		return
	}
	var p sigPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return
	}
	// end-to-end verification: signature, identity match, freshness, addressee
	if !bytes.Equal(p.From, from) || !bytes.Equal(p.To, m.pub) ||
		len(p.From) != ed25519.PublicKeySize ||
		!ed25519.Verify(ed25519.PublicKey(p.From), env.Payload, env.Sig) {
		return
	}
	if d := connector.Now().Sub(time.Unix(p.TS, 0)); d > 2*time.Minute || d < -2*time.Minute {
		return
	}
	if m.Accept == nil || !m.Accept(p.From, p.Inv) {
		return
	}
	if !m.fresh(p.Kind + string(p.From) + string(p.ConnID)) {
		return
	}
	switch p.Kind {
	case "offer":
		m.handleOffer(p)
	case "answer":
		m.handleAnswer(p)
	}
}

func (m *Manager) handleOffer(p sigPayload) {
	key := string(p.From)
	m.mu.Lock()
	if _, up := m.peers[key]; up {
		m.mu.Unlock()
		return
	}
	if _, dialing := m.pending[key]; dialing {
		// glare: lower pubkey's offer wins
		if bytes.Compare(p.From, m.pub) > 0 {
			m.mu.Unlock()
			return // our offer wins; ignore theirs
		}
		// their offer wins: abandon ours
		if old := m.dialing[key]; old != nil {
			old.pc.Close()
		}
		delete(m.pending, key)
		delete(m.dialing, key)
	}
	m.mu.Unlock()

	pc, onet, err := m.newPC(p.Obf)
	if err != nil {
		return
	}
	m.mu.Lock()
	if old := m.answering[key]; old != nil {
		old.Close()
	}
	m.answering[key] = pc
	m.mu.Unlock()
	m.newPeer(p.From, pc)
	onet.bypassRelays(p.SDP)
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: p.SDP}); err != nil {
		pc.Close()
		return
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		pc.Close()
		return
	}
	done := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		pc.Close()
		return
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		pc.Close()
		return
	}
	resp := sigPayload{
		Kind: "answer", SDP: pc.LocalDescription().SDP,
		From: m.pub, To: p.From, TS: connector.Now().Unix(), ConnID: p.ConnID,
	}
	blob, err := m.sign(resp)
	if err == nil {
		err = m.signal(p.From, blob)
	}
	if err != nil {
		pc.Close()
	}
}

func (m *Manager) handleAnswer(p sigPayload) {
	key := string(p.From)
	m.mu.Lock()
	connID := m.pending[key]
	peer := m.dialing[key]
	m.mu.Unlock()
	if peer == nil || !bytes.Equal(connID, p.ConnID) {
		return
	}
	peer.obf.bypassRelays(p.SDP)
	if err := peer.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: p.SDP}); err != nil {
		log.Printf("p2p: set answer: %v", err)
	}
}

func (m *Manager) newPeer(pub []byte, pc *webrtc.PeerConnection) *Peer {
	peer := &Peer{Pub: pub, pc: pc, media: map[string]*webrtc.DataChannel{}, open: map[string]bool{}}
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		if dc.Label() == "ctrl" {
			m.wireCtrl(peer, dc)
		} else {
			m.wireMedia(peer, dc)
		}
	})
	pc.OnConnectionStateChange(func(st webrtc.PeerConnectionState) {
		switch st {
		// Disconnected is transient (Wi-Fi to LTE): ICE may recover, and pion
		// moves to Failed on its own if it does not.
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			m.mu.Lock()
			if m.peers[string(pub)] == peer {
				delete(m.peers, string(pub))
			}
			if m.answering[string(pub)] == pc {
				delete(m.answering, string(pub))
			}
			delete(m.pending, string(pub))
			delete(m.dialing, string(pub))
			m.mu.Unlock()
			pc.Close()
			if m.OnPeerDown != nil {
				m.OnPeerDown(pub)
			}
		}
	})
	return peer
}

func (m *Manager) wireCtrl(peer *Peer, dc *webrtc.DataChannel) {
	peer.ctrl = dc
	dc.OnOpen(func() {
		m.mu.Lock()
		m.peers[string(peer.Pub)] = peer
		delete(m.pending, string(peer.Pub))
		delete(m.dialing, string(peer.Pub))
		delete(m.answering, string(peer.Pub)) // live now: a later offer must not close it
		m.mu.Unlock()
		if m.OnPeerUp != nil {
			m.OnPeerUp(peer)
		}
	})
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		if m.OnCtrl != nil {
			m.OnCtrl(peer, msg.Data)
		}
	})
}

func (m *Manager) wireMedia(peer *Peer, dc *webrtc.DataChannel) {
	lbl := dc.Label()
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		if m.OnMedia != nil {
			m.OnMedia(peer, lbl, msg.Data)
		}
	})
}

func (m *Manager) Close() {
	m.mu.Lock()
	peers := m.peers
	m.peers = map[string]*Peer{}
	m.mu.Unlock()
	for _, p := range peers {
		p.Close()
	}
}
