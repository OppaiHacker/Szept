// Package p2p: direct peer links over WebRTC data channels.
// NAT traversal via ICE/STUN (torrent-style hole punching); signaling blobs
// travel through the connector but are Ed25519-signed end-to-end and verified
// against the expected peer key, so a malicious connector cannot MITM the
// DTLS handshake. Result: every data channel is an authenticated E2E pipe.
package p2p

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"

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
}

type sigEnvelope struct {
	Payload []byte `json:"payload"`
	Sig     []byte `json:"sig"`
}

type Peer struct {
	Pub  []byte
	pc   *webrtc.PeerConnection
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

type Manager struct {
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey
	conn *connector.Client
	cfg  webrtc.Configuration

	// Accept decides whether we talk to this pubkey at all (friend, chat
	// co-member or invite target). Trust boundary — default deny.
	Accept     func(pub []byte) bool
	OnPeerUp   func(p *Peer)
	OnPeerDown func(pub []byte)
	OnCtrl     func(p *Peer, data []byte)
	OnMedia    func(p *Peer, label string, data []byte)

	mu      sync.Mutex
	peers   map[string]*Peer
	pending map[string][]byte // pub -> connID of our outgoing offer
	dialing map[string]*Peer  // pub -> peer awaiting answer
}

func NewManager(priv ed25519.PrivateKey, conn *connector.Client, stun []string) *Manager {
	if len(stun) == 0 {
		stun = DefaultSTUN
	}
	m := &Manager{
		priv: priv, pub: priv.Public().(ed25519.PublicKey), conn: conn,
		cfg:   webrtc.Configuration{ICEServers: []webrtc.ICEServer{{URLs: stun}}},
		peers: map[string]*Peer{}, pending: map[string][]byte{}, dialing: map[string]*Peer{},
	}
	conn.OnSignal = m.onSignal
	return m
}

// SetConn swaps the connector client (live connector change in settings).
func (m *Manager) SetConn(c *connector.Client) {
	m.mu.Lock()
	m.conn = c
	m.mu.Unlock()
	c.OnSignal = m.onSignal
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

func (m *Manager) sign(p sigPayload) []byte {
	b, _ := json.Marshal(p)
	env := sigEnvelope{Payload: b, Sig: ed25519.Sign(m.priv, b)}
	return connector.JSON(env)
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
	m.pending[string(pub)] = connID
	m.mu.Unlock()

	fail := func(err error) error {
		m.mu.Lock()
		delete(m.pending, string(pub))
		m.mu.Unlock()
		return err
	}

	pc, err := webrtc.NewPeerConnection(m.cfg)
	if err != nil {
		return fail(err)
	}
	peer := m.newPeer(pub, pc)
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
		From: m.pub, To: pub, TS: time.Now().Unix(), ConnID: connID,
	}
	if err := m.signal(pub, m.sign(payload)); err != nil {
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
	// remember pc for the answer
	m.mu.Lock()
	m.dialing[string(pub)] = peer
	m.mu.Unlock()
	return nil
}

func (m *Manager) onSignal(from []byte, blob []byte) {
	var env sigEnvelope
	if err := json.Unmarshal(blob, &env); err != nil {
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
	if d := time.Since(time.Unix(p.TS, 0)); d > 2*time.Minute || d < -2*time.Minute {
		return
	}
	if m.Accept == nil || !m.Accept(p.From) {
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

	pc, err := webrtc.NewPeerConnection(m.cfg)
	if err != nil {
		return
	}
	m.newPeer(p.From, pc)
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
		From: m.pub, To: p.From, TS: time.Now().Unix(), ConnID: p.ConnID,
	}
	if err := m.signal(p.From, m.sign(resp)); err != nil {
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
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed, webrtc.PeerConnectionStateDisconnected:
			m.mu.Lock()
			if m.peers[string(pub)] == peer {
				delete(m.peers, string(pub))
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
