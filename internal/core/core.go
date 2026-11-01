// Package core: the client brain shared by TUI and GUI.
// Owns identity, store, connector link and p2p manager; implements chat
// creation, DAG sync/gossip, invites, friends, key distribution and calls.
package core

import (
	"bytes"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"maps"
	"math/rand/v2"
	"net/http"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"szept/internal/attach"
	"szept/internal/connector"
	"szept/internal/crypt"
	"szept/internal/event"
	"szept/internal/i18n"
	"szept/internal/identity"
	"szept/internal/media"
	"szept/internal/p2p"
	"szept/internal/store"
)

type Config struct {
	Connector string   `json:"connector"`
	STUN      []string `json:"stun,omitempty"`
	// RelayOnly: talk to peers only through the connector's TURN relay, so
	// contacts never learn our IP. Needs a connector that runs TURN.
	RelayOnly bool `json:"relay_only,omitempty"`
	// QuietLookup: ask the connector only about members of chats the user
	// opened or wrote in recently, instead of every contact every 30 s. The
	// connector then no longer learns the whole contact list, at the cost of
	// presence for everyone else.
	QuietLookup bool   `json:"quiet_lookup,omitempty"`
	DataDir     string `json:"-"`
	// MediaDir: directory browsed by built-in gallery (Android doesn't have
	// single obvious place for photos, and system file picker doesn't
	// show thumbnail grid).
	MediaDir string `json:"media_dir,omitempty"`

	// Connectors: zapisana lista connectorów do przełączania w ustawieniach.
	Connectors []string `json:"connectors,omitempty"`
	// ConnectorLists: adresy HTTPS dynamicznych list (tekst, jeden ws:// na
	// linię) — „Odśwież” w ustawieniach dopisuje wpisy do Connectors.
	ConnectorLists []string `json:"connector_lists,omitempty"`

	Accent        string          `json:"accent,omitempty"`         // kolor akcentu, hex RRGGBB
	OutputVol     float64         `json:"output_vol,omitempty"`     // głośność rozmów 0..2, 0 = domyślna 1
	CamDevice     string          `json:"cam_device,omitempty"`     // np. /dev/video1
	CamVideo      media.VideoOpts `json:"cam_video,omitempty"`      // last picked, see DefCam
	ScreenVideo   media.VideoOpts `json:"screen_video,omitempty"`   // last picked, see DefScreen
	MicDevice     string          `json:"mic_device,omitempty"`     // malgo device ID, "" = domyślny
	SpeakerDevice string          `json:"speaker_device,omitempty"` // malgo device ID, "" = domyślny
	MicGain       float64         `json:"mic_gain,omitempty"`       // wzmocnienie mikrofonu 0..2, 0 = 1
	Voice         media.VoiceOpts `json:"voice"`                    // odszumianie, AGC, bramka, bitrate
	NoNotify      bool            `json:"no_notify,omitempty"`      // wycisz powiadomienia systemowe
	NoUpdate      bool            `json:"no_update,omitempty"`      // nie sprawdzaj i nie instaluj nowych wersji
	DevMode       bool            `json:"dev_mode,omitempty"`       // menu kontekstowe i inspektor webview

	Profile string `json:"profile,omitempty"` // aktywne konto (profil), "" = domyślne

	WebhookOn   bool `json:"webhook_on,omitempty"`
	WebhookPort int  `json:"webhook_port,omitempty"`
	// the webhook token lives in the encrypted database, see WebhookToken
}

// Ctrl is the JSON message format on the reliable "ctrl" data channel.
type Ctrl struct {
	T       string            `json:"t"` // hello|sync|want|events|keyreq|keys|redeem|call|blobreq|blobpart|typing|ack
	Chat    []byte            `json:"chat,omitempty"`
	Heads   [][]byte          `json:"heads,omitempty"`
	IDs     [][]byte          `json:"ids,omitempty"`
	Events  [][]byte          `json:"events,omitempty"`
	Epochs  map[uint32][]byte `json:"epochs,omitempty"`
	MEpochs map[uint32][]byte `json:"mepochs,omitempty"` // member keys for epochs before a join (no-history chats)
	Secret  []byte            `json:"secret,omitempty"`
	Pin     string            `json:"pin,omitempty"` // redeem: one-time PIN
	Name    string            `json:"name,omitempty"`
	Action  string            `json:"action,omitempty"` // call: join|leave|state|kick
	Reply   bool              `json:"reply,omitempty"`  // sync: suppress re-reply
	Typing  bool              `json:"typing,omitempty"`
	EventID []byte            `json:"event_id,omitempty"`
	Status  string            `json:"status,omitempty"`
	Ver     string            `json:"ver,omitempty"` // hello: sender's version

	// blobreq/blobpart: attachment content, chunked (see blob.go)
	Blob  []byte `json:"blob,omitempty"` // sha256 of the content
	Part  []byte `json:"part,omitempty"`
	Idx   int    `json:"idx,omitempty"`
	Total int    `json:"total,omitempty"`
	Need  []int  `json:"need,omitempty"` // blobreq: only these parts (resume)
}

type pendingRedeem struct {
	Chat    []byte
	Inviter []byte
	Secret  []byte
	Pin     string
}

type Core struct {
	ID   *identity.Identity
	St   *store.Store
	Conn *connector.Client
	P2P  *p2p.Manager
	Cfg  Config

	// VoiceIn and VoiceOut, when set before a call, stand in for the
	// microphone and the speaker: a headless client (a bot) talks and listens
	// through them. VoiceIn gets 20 ms 48 kHz s16le frames pushed by the
	// caller's cb; VoiceOut receives each heard peer's frames by pubkey.
	VoiceIn  func(cb func(pcm []byte)) (stop func(), err error)
	VoiceOut func(src string, pcm []byte)

	mu         sync.Mutex
	orphans    map[string]map[string]*orphan // chatKey -> eventID -> parked event
	refolds    map[string]*rate.Limiter      // peer -> budget of full history replays (ingest)
	genesis    map[string]*event.Event       // chatKey -> genesis we could not decrypt yet
	states     map[string]*foldedState       // chatKey -> cached fold (see ChatState)
	folders    map[string]*event.Folder      // chatKey -> fold behind states (see checkout)
	foldGen    map[string]uint64
	redeem     *pendingRedeem
	calls      map[string]*Call
	chanSel    map[string][]byte // chat -> channel picked with SetChannel
	stopTest   func()            // microphone test in the settings
	listeners  []chan struct{}
	wants      map[string]*blobWant            // blob hash -> attachment we still need
	incoming   map[string]*blobAsm             // blob hash -> chunks arriving now
	serving    map[string]int                  // peer -> blob sends in flight
	assemblers map[string]map[uint32]*assembly // peer+label -> frame -> video parts
	typing     map[string]map[string]time.Time // chatKey -> pubKeyStr -> lastTyping
	acks       map[string]map[string]string    // eventIDStr -> pubKeyStr -> status ("delivered"|"read")
	active     map[string]time.Time            // chatKey -> last opened or written (QuietLookup)

	// media hooks (set by calls.go / UIs)
	// OnVideoFrame gets each video frame; a nil jpeg means that stream ended.
	OnVideoFrame func(chat []byte, from []byte, kind string, jpeg []byte)
	voiceOut     func(src string, pcm []byte) // speaker mixer sink, set when call active
	voiceRx      *media.Receiver              // Opus decoders for voiceOut, per call
	player       *media.Player                // żyje razem z rozmową (JoinCall/LeaveCall)
	micPeak      float32                      // own mic level, for the speaking ring
	micAt        time.Time
	stopVoice    func()
	stopCam      func()
	stopScreen   func()
	webhook      *http.Server
}

const langKey = "lang"

func Open(cfg Config, id *identity.Identity) (*Core, error) {
	st, err := store.Open(filepath.Join(cfg.DataDir, "szept.db"), id.StorageKey())
	if err != nil {
		return nil, err
	}
	if nick := st.Meta(nickKey); len(nick) > 0 {
		id.Name = string(nick) // name change in profile overrides identity file
	}
	if lang := st.Meta(langKey); len(lang) > 0 {
		i18n.SetLang(string(lang))
	}
	conn := connector.NewClient(cfg.Connector, id.Priv)
	c := &Core{
		ID: id, St: st, Conn: conn, Cfg: cfg,
		orphans:    map[string]map[string]*orphan{},
		refolds:    map[string]*rate.Limiter{},
		genesis:    map[string]*event.Event{},
		states:     map[string]*foldedState{},
		folders:    map[string]*event.Folder{},
		foldGen:    map[string]uint64{},
		calls:      map[string]*Call{},
		chanSel:    map[string][]byte{},
		wants:      map[string]*blobWant{},
		incoming:   map[string]*blobAsm{},
		serving:    map[string]int{},
		assemblers: map[string]map[uint32]*assembly{},
		typing:     map[string]map[string]time.Time{},
		active:     map[string]time.Time{},
		acks:       map[string]map[string]string{},
	}
	c.P2P = p2p.NewManager(id.Priv, conn, cfg.STUN)
	c.P2P.SetRelayOnly(cfg.RelayOnly)
	c.P2P.Accept = c.acceptPeer
	c.P2P.Ticket = c.inviteTicket
	c.P2P.OnPeerUp = c.onPeerUp
	c.P2P.OnPeerDown = func(pub []byte) { c.onPeerDown(pub) }
	c.P2P.OnCtrl = c.onCtrl
	c.P2P.OnMedia = c.onMedia
	conn.OnUp = func() { c.connectAll(); c.Notify() }
	conn.OnDown = c.Notify
	c.cleanTempDir() // plaintext a crash left behind
	return c, nil
}

// conn returns the current connector client (swappable via SetConnector).
func (c *Core) conn() *connector.Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Conn
}

// ConnUp reports whether we are registered with the connector right now.
func (c *Core) ConnUp() bool { return c.conn().Up() }

// ConnErr returns the last connector error ("" when registered) for UIs.
func (c *Core) ConnErr() string { return c.conn().LastErr() }

// SetConnector switches to a new connector URL live: new client, rewired
// callbacks, old session closed. Persisting the URL is the caller's job.
// SetRelayOnly: route every new peer connection through the connector's TURN
// relay (contacts see the connector's IP, not ours).
func (c *Core) SetRelayOnly(on bool) {
	c.mu.Lock()
	c.Cfg.RelayOnly = on
	c.mu.Unlock()
	c.P2P.SetRelayOnly(on)
}

// ConnRelay reports whether the current connector offers a TURN relay.
func (c *Core) ConnRelay() bool { return c.conn().ICE() != nil }

// ConnVersion: the connector's version ("" = it doesn't say).
func (c *Core) ConnVersion() string { return c.conn().ServerVersion() }

// peerVer: pubkey -> version the peer last said in "hello". Memory only.
var peerVer sync.Map

// PeerVersion: what pub runs, as it told us when last connected ("" = unknown).
func (c *Core) PeerVersion(pub []byte) string {
	v, _ := peerVer.Load(string(pub))
	s, _ := v.(string)
	return s
}

func (c *Core) SetConnector(url string) {
	old := c.conn()
	if old.URL == url {
		return
	}
	nc := connector.NewClient(url, c.ID.Priv)
	nc.OnUp = func() { c.connectAll(); c.Notify() }
	nc.OnDown = c.Notify
	c.mu.Lock()
	c.Conn = nc
	c.Cfg.Connector = url
	c.mu.Unlock()
	c.P2P.SetConn(nc)
	old.Close()
	go nc.Run()
	c.Notify()
}

func (c *Core) Start() {
	go c.Conn.Run()
	go c.rescanAttachments() // startup rescan: missing blobs from previous session
	go func() {
		for {
			time.Sleep(30 * time.Second)
			c.connectAll()
			c.retryWants(false)
			c.announceProfile() // chats where profile hasn't arrived yet
		}
	}()
}

func (c *Core) Close() {
	c.cleanTempDir()
	c.StopWebhook()
	c.LeaveAllCalls()
	c.P2P.Close()
	c.conn().Close()
	c.St.Close()
}

// ---- UI notification fan-out ----

func (c *Core) Subscribe() chan struct{} {
	ch := make(chan struct{}, 1)
	c.mu.Lock()
	c.listeners = append(c.listeners, ch)
	c.mu.Unlock()
	return ch
}

func (c *Core) Notify() {
	c.mu.Lock()
	ls := append([]chan struct{}(nil), c.listeners...)
	c.mu.Unlock()
	for _, ch := range ls {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// ---- trust boundary ----

func (c *Core) acceptPeer(pub, ticket []byte) bool {
	if c.St.IsFriend(pub) {
		return true
	}
	c.mu.Lock()
	r := c.redeem
	c.mu.Unlock()
	if r != nil && bytes.Equal(r.Inviter, pub) {
		return true
	}
	// co-member of any chat
	for chatID := range c.chatIDs() {
		if st, err := c.ChatState([]byte(chatID)); err == nil && st.IsMember(pub) {
			return true
		}
	}
	// holder of a fresh invite we issued: we can't know the pub in advance,
	// so the offer carries the hash of the invite secret. Without it a
	// stranger gets nothing, not even our ICE candidates (IP). Secret+PIN
	// still gate membership in handleRedeem.
	return len(ticket) == sha256.Size && c.St.InviteOpen(ticket)
}

// inviteTicket: offers to the inviter of a pending redeem carry the secret hash.
func (c *Core) inviteTicket(pub []byte) []byte {
	c.mu.Lock()
	r := c.redeem
	c.mu.Unlock()
	if r == nil || !bytes.Equal(r.Inviter, pub) {
		return nil
	}
	h := sha256.Sum256(r.Secret)
	return h[:]
}

// ---- chats ----

func (c *Core) chatIDs() map[string]store.ChatMeta {
	m, err := c.St.Chats()
	if err != nil {
		return map[string]store.ChatMeta{}
	}
	return m
}

// foldedState: cached result of last chat fold. Fold verifies full history, and
// we query state per media packet (audio = 50 frames/s) and per received
// event — without cache a chat with 1000 events consumes full CPU.
type foldedState struct {
	st  *event.State
	err error
}

// invalidate: call after writing an epoch key for this chat (or anything else
// that a cached fold cannot see).
func (c *Core) invalidate(chat []byte) {
	c.mu.Lock()
	delete(c.states, string(chat))
	delete(c.folders, string(chat))
	c.foldGen[string(chat)]++
	c.mu.Unlock()
}

// Cached folds. folders holds the Folder behind states, advanced to the last
// stored event, so a new event is validated by applying just that event
// instead of decrypting and replaying the whole history (the old O(N) per
// event, O(N²) per sync). foldGen bumps on every publish and invalidation: a
// fold built from an older snapshot (a concurrent ingest got there first) is
// dropped instead of overwriting a newer one.

// checkout takes the cached Folder for exclusive use. Apply mutates it, so
// it leaves the cache until publish puts a successor back.
func (c *Core) checkout(chat []byte) (*event.Folder, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	f := c.folders[string(chat)]
	delete(c.folders, string(chat))
	return f, c.foldGen[string(chat)]
}

// publish returns a read-only copy of f's state for the caller.
func (c *Core) publish(chat []byte, gen uint64, f *event.Folder, err error) *event.State {
	if err != nil {
		f = nil
	}
	var st *event.State
	if f != nil {
		st = f.St.Clone() // readers keep it; Apply must not touch it
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	k := string(chat)
	if c.foldGen[k] != gen {
		delete(c.states, k)
		delete(c.folders, k)
		c.foldGen[k]++
		return st
	}
	c.foldGen[k]++
	if f == nil {
		c.states[k] = &foldedState{err: err}
		return nil
	}
	c.folders[k] = f
	c.states[k] = &foldedState{st: st}
	return st
}

// foldWith validates e against the chat's stored history and returns the
// Folder with e applied: one Apply when e sorts after the cached fold, a full
// replay otherwise. A rejected e leaves a cached Folder as it was, and
// foldWith hands it back to the cache.
func (c *Core) foldWith(chat []byte, f *event.Folder, gen uint64, e *event.Event, keys, mkeys map[uint32][]byte) (*event.Folder, error) {
	if f != nil && f.SameKeys(keys, mkeys) && f.After(e) {
		if err := f.Apply(e); err != nil {
			c.putBack(chat, gen, f)
			return nil, err
		}
		return f, nil
	}
	return c.fullFold(chat, e, keys, mkeys)
}

// putBack returns an unchanged Folder to the cache.
func (c *Core) putBack(chat []byte, gen uint64, f *event.Folder) {
	c.mu.Lock()
	if c.foldGen[string(chat)] == gen {
		c.folders[string(chat)] = f
	}
	c.mu.Unlock()
}

// fullFold replays the stored history, plus extra if given.
func (c *Core) fullFold(chat []byte, extra *event.Event, keys, mkeys map[uint32][]byte) (*event.Folder, error) {
	evs, err := c.St.Events(chat)
	if err != nil {
		return nil, err
	}
	if extra != nil {
		evs = append(evs, extra)
	}
	event.SortCanonical(evs)
	// FoldTrusted semantics: everything stored passed ingest's checks
	f := event.NewFolder(chat, keys, mkeys, false)
	for _, e := range evs {
		if err := f.Apply(e); err != nil {
			return nil, err
		}
	}
	if len(f.St.Owner) == 0 {
		return nil, errors.New("no genesis event")
	}
	return f, nil
}

// ChatState returns folded state of chat. Result is read-only — shared by
// all readers until next state mutation.
//
// Fold still walks entire history, just less frequently (and without
// verifying signatures, as store contains only validated events).
// For histories scaling to tens of thousands of events, next step is incremental
// fold or state snapshot every N events.
func (c *Core) ChatState(chat []byte) (*event.State, error) {
	c.mu.Lock()
	cached := c.states[string(chat)]
	gen := c.foldGen[string(chat)]
	c.mu.Unlock()
	if cached != nil {
		return cached.st, cached.err
	}
	keys, mkeys, err := c.chatKeys(chat)
	if err != nil {
		return nil, err
	}
	f, err := c.fullFold(chat, nil, keys, mkeys)
	return c.publish(chat, gen, f, err), err
}

// chatKeys: content keys, and member keys held for epochs without one.
func (c *Core) chatKeys(chat []byte) (keys, mkeys map[uint32][]byte, err error) {
	if keys, err = c.St.ChatKeys(chat); err != nil {
		return nil, nil, err
	}
	mkeys, err = c.St.MemberKeys(chat)
	return keys, mkeys, err
}

type ChatInfo struct {
	ID       []byte
	Name     string
	Members  []event.Member
	Owner    []byte
	Epoch    uint32
	Online   int
	InCall   []string            // member names currently in any of the chat's calls
	Voice    map[string][]string // channel ID ("" = default) -> member names in its call
	VFlags   map[string][]string // same keys, Voice's order: each one's Call.State
	Direct   bool                // 1:1 chat: unnamed with two members (see CreateDirect)
	Channels []event.Channel
	// group look (event.KindServer)
	Icon, Banner []byte
	Style        string
	NoHistory    bool // members added now do not see earlier messages
}

// directPeer: the other person of a 1:1 chat (unnamed, two members), else nil.
func (c *Core) directPeer(st *event.State) []byte {
	if st.Name != "" {
		return nil
	}
	var other []byte
	n := 0
	for _, m := range st.Members {
		if !m.Removed {
			n++
			if !bytes.Equal(m.Pub, c.ID.Pub) {
				other = m.Pub
			}
		}
	}
	if n != 2 {
		return nil
	}
	return other
}

// directTwins: every 1:1 chat with the same person as chat, lowest ID first,
// or just chat when it is not a 1:1. Two people who open a DM at the same
// moment each create one; both sides then treat the lowest ID as the DM and
// show the others' messages inside it.
// Scans every chat per call, an index by peer if chat counts grow.
func (c *Core) directTwins(chat []byte) [][]byte {
	st, err := c.ChatState(chat)
	if err != nil {
		return [][]byte{chat}
	}
	peer := c.directPeer(st)
	if peer == nil {
		return [][]byte{chat}
	}
	var out [][]byte
	for idStr := range c.chatIDs() {
		if s, err := c.ChatState([]byte(idStr)); err == nil && bytes.Equal(c.directPeer(s), peer) {
			out = append(out, []byte(idStr))
		}
	}
	slices.SortFunc(out, bytes.Compare)
	return out
}

// holding: the twin of chat whose DAG has event id (a message shown in a
// merged DM may live in another twin).
func (c *Core) holding(chat, id []byte) []byte {
	for _, t := range c.directTwins(chat) {
		if c.St.HasEvent(t, id) {
			return t
		}
	}
	return chat
}

func (c *Core) Chats() []ChatInfo {
	var out []ChatInfo
	for idStr := range c.chatIDs() {
		id := []byte(idStr)
		st, err := c.ChatState(id)
		if err != nil {
			continue
		}
		if c.directPeer(st) != nil && !bytes.Equal(c.directTwins(id)[0], id) {
			continue // a duplicate DM: its messages show in the first one
		}
		ci := ChatInfo{ID: id, Name: c.displayName(st), Owner: st.Owner, Epoch: st.Epoch, Channels: st.Channels,
			Icon: st.Icon, Banner: st.Banner, Style: st.Style, NoHistory: st.NoHistory}
		for _, m := range st.Members {
			if m.Removed {
				continue
			}
			mem := *m
			mem.Name = safeName(mem.Name)
			ci.Members = append(ci.Members, mem)
			if !bytes.Equal(m.Pub, c.ID.Pub) && c.P2P.Peer(m.Pub) != nil {
				ci.Online++
			}
		}
		ci.Direct = c.directPeer(st) != nil
		sort.Slice(ci.Members, func(i, j int) bool { return ci.Members[i].Name < ci.Members[j].Name })
		c.mu.Lock()
		for key, call := range c.calls {
			if !strings.HasPrefix(key, idStr) {
				continue
			}
			for pub := range call.Members {
				n := c.memberName(st, []byte(pub))
				ci.InCall = append(ci.InCall, n)
				if ci.Voice == nil {
					ci.Voice, ci.VFlags = map[string][]string{}, map[string][]string{}
				}
				ch := key[len(idStr):]
				ci.Voice[ch] = append(ci.Voice[ch], n)
				ci.VFlags[ch] = append(ci.VFlags[ch], call.Flags[pub])
			}
		}
		c.mu.Unlock()
		out = append(out, ci)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// MaxName: ceiling on network-provided name (SetNick enforces this locally,
// but peer sends arbitrary payload).
const MaxName = event.MaxName

// Sanitizing network text lives in the event package so the fold and the UI
// index agree on one implementation — event cannot import core.
func safeText(s string) string { return event.SafeText(s) }
func safeName(s string) string { return event.SafeName(s) }
func safeBio(s string) string  { return event.SafeBio(s) }

// displayName: direct 2-person unnamed chat shows the other side's name.
func (c *Core) displayName(st *event.State) string {
	if st.Name != "" {
		return safeName(st.Name)
	}
	var others []string
	for _, m := range st.Members {
		if !m.Removed && !bytes.Equal(m.Pub, c.ID.Pub) {
			others = append(others, c.memberName(st, m.Pub))
		}
	}
	if len(others) == 0 {
		return "(empty chat)"
	}
	return strings.Join(others, ", ")
}

// PeerName: friend name, or the key fingerprint for a stranger.
func (c *Core) PeerName(pub []byte) string { return c.memberName(nil, pub) }

func (c *Core) memberName(st *event.State, pub []byte) string {
	if bytes.Equal(pub, c.ID.Pub) {
		return c.ID.Name
	}
	if st != nil {
		if m, ok := st.Members[string(pub)]; ok && m.Name != "" {
			return safeName(m.Name)
		}
	}
	if fs, err := c.St.Friends(); err == nil {
		for _, f := range fs {
			if bytes.Equal(f.Pub, pub) {
				return safeName(f.Display())
			}
		}
	}
	return identity.Fingerprint(pub)[:9]
}

// NickEntry: one name a person announced, with the moment they did it.
type NickEntry struct {
	Name string
	TS   time.Time
}

// NickHistory: every name a person has announced across chats we share with
// them. Names live in the signed DAG (genesis carries the creator's, KindNick
// every later change), so this cannot be quietly rewritten — that is the whole
// point of showing it: a peer swapping their display name mid-conversation
// leaves a visible trail for everyone in the chat.
func (c *Core) NickHistory(pub []byte) []NickEntry {
	var out []NickEntry
	for idStr := range c.chatIDs() {
		chat := []byte(idStr)
		evs, err := c.St.Events(chat)
		if err != nil {
			continue
		}
		keys, mkeys, _ := c.chatKeys(chat)
		event.SortCanonical(evs) // events within one chat in DAG order
		for _, e := range evs {
			if e.Kind != event.KindNick && e.Kind != event.KindCreate {
				continue
			}
			if !bytes.Equal(e.Author, pub) {
				continue
			}
			b, ok := e.Open(keys, mkeys)
			if !ok {
				continue
			}
			name := b.Name
			if e.Kind == event.KindCreate {
				name = b.Text // genesis: Name is the chat's, Text the creator's
			}
			if name = safeName(name); name != "" {
				out = append(out, NickEntry{Name: name, TS: time.Unix(b.TS, 0)})
			}
		}
	}
	// one rename is announced in every chat at once, so the same name shows up
	// as many times as there are chats — keep the first occurrence of each
	uniq, seen := out[:0], map[string]bool{}
	for _, n := range out {
		if !seen[n.Name] {
			seen[n.Name] = true
			uniq = append(uniq, n)
		}
	}
	// stable: renames within one second are common (SetNick writes to every
	// chat in a loop), and canonical DAG order must survive the sort
	sort.SliceStable(uniq, func(i, j int) bool { return uniq[i].TS.Before(uniq[j].TS) })
	return uniq
}

type Msg struct {
	ID        []byte // event ID — key for "hide locally"
	Author    []byte
	Name      string
	Text      string
	TS        time.Time
	Kind      string            // msg | info | file
	Info      string            // for Kind == "info": event.Kind* — client selects icon and color
	Deleted   bool              // deleted for everyone in DAG
	Pinned    bool              // pinned for everyone in DAG
	Reply     []byte            // ID of the message this one answers
	Reactions map[string]string // pubKeyStr -> emoji
	ACK       string            // "" | "delivered" | "read"
	Ch        []byte            // channel, nil = the default one

	// for Kind == "file" only
	Blob []byte // sha256 content hash
	Mime string
	File string // filename (cleaned by attach.SafeName)
	Size int64
	W, H int
	Have bool // full content available locally; false = downloading needed or in progress

	// large file: thumbnail auto-replicates, full bytes on demand
	Prev     []byte
	PrevMime string
	HavePrev bool
	Getting  bool // full bytes currently in queue
}

// Messages of chat; a DM also gathers its twins' (see directTwins).
func (c *Core) Messages(chat []byte) []Msg {
	twins := c.directTwins(chat)
	if len(twins) < 2 {
		return c.messages(chat)
	}
	var out []Msg
	for _, t := range twins {
		out = append(out, c.messages(t)...)
	}
	slices.SortStableFunc(out, func(a, b Msg) int { return a.TS.Compare(b.TS) })
	return out
}

func (c *Core) messages(chat []byte) []Msg {
	evs, err := c.St.Events(chat)
	if err != nil {
		return nil
	}
	keys, mkeys, _ := c.chatKeys(chat)
	st, errFold := c.ChatState(chat)
	if errFold != nil {
		st = &event.State{Members: map[string]*event.Member{}, Reactions: map[string]map[string]string{}, Deleted: map[string]bool{}, Pinned: map[string]bool{}}
	}
	event.SortCanonical(evs)
	hidden := c.St.Hidden(chat)
	var out []Msg
	for _, e := range evs {
		id := e.ID()
		if hidden[string(id)] {
			continue // hidden locally (soft delete) — bytes remain in DAG
		}
		// content from before joining a chat without history stays unreadable
		b, ok := e.Open(keys, mkeys)
		if !ok {
			continue
		}
		name := c.memberName(st, e.Author)
		m := Msg{ID: id, Author: e.Author, Name: name, TS: time.Unix(b.TS, 0), Ch: b.Ch}
		switch e.Kind {
		case event.KindMsg:
			m.Kind, m.Text, m.Reply = "msg", safeText(b.Text), b.Target
		case event.KindFile:
			m.Kind, m.Text = "file", safeText(b.Text)
			m.Blob, m.Mime, m.File, m.Size, m.W, m.H = b.Blob, b.Mime, attach.SafeName(b.Name, b.Mime), b.Size, b.W, b.H
			m.Prev, m.PrevMime = b.Prev, b.PrevMime
			m.Have = c.St.HasBlob(b.Blob)
			m.HavePrev = len(b.Prev) > 0 && c.St.HasBlob(b.Prev)
			m.Getting = !m.Have && c.Fetching(b.Blob)
		case event.KindCreate:
			m.Kind, m.Info, m.Text = "info", e.Kind, name+" created the chat"
		case event.KindAdd:
			m.Kind, m.Info, m.Text = "info", e.Kind, name+" added "+safeName(b.Name)
		case event.KindRemove:
			m.Kind, m.Info, m.Text = "info", e.Kind, name+" removed a member"
		case event.KindLeave:
			m.Kind, m.Info, m.Text = "info", e.Kind, name+" left"
		case event.KindRekey:
			m.Kind, m.Info, m.Text = "info", e.Kind, "key rotation (new epoch)"
		case event.KindHistory:
			m.Kind, m.Info, m.Text = "info", e.Kind, name+" made earlier messages visible to new members"
			if b.NoHist {
				m.Text = name + " hid earlier messages from new members"
			}
		default:
			continue // avatar, nick, rename, react, delete — not separate message items
		}
		if st.Deleted != nil && st.Deleted[string(id)] {
			m.Deleted = true
			m.Text = "[Wiadomość usunięta]"
		}
		m.Pinned = st.Pinned[string(id)]
		if st.Reactions != nil && st.Reactions[string(id)] != nil {
			m.Reactions = st.Reactions[string(id)]
		}
		if bytes.Equal(e.Author, c.ID.Pub) {
			m.ACK = c.AckStatus(id)
		}
		out = append(out, m)
	}
	return out
}

// ---- Typing, ACKs, Reactions, Delete, Search ----

func (c *Core) SendTyping(chat []byte, active bool) {
	c.markActive(chat)
	st, err := c.ChatState(chat)
	if err != nil {
		return
	}
	for _, m := range st.Members {
		if !m.Removed && !bytes.Equal(m.Pub, c.ID.Pub) {
			if p := c.P2P.Peer(m.Pub); p != nil {
				p.SendCtrl(Ctrl{T: "typing", Chat: chat, Typing: active})
			}
		}
	}
}

func (c *Core) TypingPeers(chat []byte) []string {
	var pubs []string
	c.mu.Lock()
	now := time.Now()
	for pubStr, t := range c.typing[string(chat)] {
		if now.Sub(t) < 4*time.Second {
			pubs = append(pubs, pubStr)
		}
	}
	c.mu.Unlock()
	if len(pubs) == 0 {
		return nil
	}
	st, _ := c.ChatState(chat)
	var names []string
	for _, p := range pubs {
		names = append(names, c.memberName(st, []byte(p)))
	}
	return names
}

func (c *Core) SendAck(chat []byte, eventID []byte, status string) {
	st, err := c.ChatState(chat)
	if err != nil {
		return
	}
	for _, m := range st.Members {
		if !m.Removed && !bytes.Equal(m.Pub, c.ID.Pub) {
			if p := c.P2P.Peer(m.Pub); p != nil {
				p.SendCtrl(Ctrl{T: "ack", Chat: chat, EventID: eventID, Status: status})
			}
		}
	}
}

func (c *Core) AckStatus(eventID []byte) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := c.acks[string(eventID)]
	if m == nil {
		return ""
	}
	hasRead, hasDelivered := false, false
	for _, s := range m {
		if s == "read" {
			hasRead = true
		} else if s == "delivered" {
			hasDelivered = true
		}
	}
	if hasRead {
		return "read"
	}
	if hasDelivered {
		return "delivered"
	}
	return ""
}

func (c *Core) MarkChatRead(chat []byte) {
	c.markActive(chat)
	for _, t := range c.directTwins(chat) {
		evs, err := c.St.Events(t)
		if err != nil {
			continue
		}
		for _, e := range evs {
			if !bytes.Equal(e.Author, c.ID.Pub) {
				c.SendAck(t, e.ID(), "read")
			}
		}
	}
}

func (c *Core) ReactMsg(chat []byte, targetID []byte, emoji string) error {
	chat = c.holding(chat, targetID)
	_, err := c.appendEvent(chat, event.KindReact, event.Body{Target: targetID, Text: emoji, TS: time.Now().Unix()})
	return err
}

func (c *Core) DeleteMsg(chat []byte, targetID []byte) error {
	chat = c.holding(chat, targetID)
	st, err := c.ChatState(chat)
	if err != nil {
		return err
	}
	if t, err := c.St.GetEvent(chat, targetID); err != nil ||
		!bytes.Equal(t.Author, c.ID.Pub) && !bytes.Equal(st.Owner, c.ID.Pub) {
		return errors.New("możesz usunąć tylko swoją wiadomość")
	}
	_, err = c.appendEvent(chat, event.KindDelete, event.Body{Target: targetID, TS: time.Now().Unix()})
	return err
}

func (c *Core) SearchChat(chat []byte, query string) ([]Msg, error) {
	msgs := c.Messages(chat)
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return msgs, nil
	}
	var res []Msg
	for _, m := range msgs {
		if !m.Deleted && strings.Contains(strings.ToLower(m.Text), q) {
			res = append(res, m)
		}
	}
	return res, nil
}

// ---- soft delete: "hide locally" ----
//
// DAG is signed and replicated, so nothing disappears from history. HideMsg
// remembers locally what not to render — on this device only.

func (c *Core) HideMsg(chat, id []byte) error {
	chat = c.holding(chat, id)
	if err := c.St.HideEvent(chat, id); err != nil {
		return err
	}
	c.Notify()
	return nil
}

// UnhideAll restores all hidden items in the given chat.
func (c *Core) UnhideAll(chat []byte) error {
	if err := c.St.ShowAllHidden(chat); err != nil {
		return err
	}
	c.Notify()
	return nil
}

func (c *Core) HiddenCount(chat []byte) int { return len(c.St.Hidden(chat)) }

// ---- local profile ----

// nickKey: display name lives in database, not in identity file — identity
// is passphrase-encrypted and passphrase is not retained after unlock.
const nickKey = "nick"

// SetNick updates display name and announces it in every chat, so
// peers joining later or currently offline receive it too.
func (c *Core) SetNick(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("nazwa nie może być pusta")
	}
	if len(name) > 40 {
		return errors.New("nazwa może mieć najwyżej 40 znaków")
	}
	if err := c.St.PutMeta(nickKey, []byte(name)); err != nil {
		return err
	}
	c.ID.Name = name
	var lastErr error
	for idStr := range c.chatIDs() {
		chat := []byte(idStr)
		if st, err := c.ChatState(chat); err != nil || !st.IsMember(c.ID.Pub) {
			continue
		}
		if _, err := c.appendEvent(chat, event.KindNick,
			event.Body{Name: name, TS: time.Now().Unix()}); err != nil {
			lastErr = err
		}
	}
	c.Notify()
	return lastErr
}

// announceProfile appends avatar and name to chats where member entry
// does not have them yet. Without this, profile set BEFORE joining a
// chat would never reach it (announcements are sent only to existing chats
// at the moment of change). Comparison with folded state keeps this idempotent,
// so it can be called in loops — which we do, as a newly joined chat might
// not have epoch key yet and first attempt fails.
func (c *Core) announceProfile() {
	av, haveAv := c.St.Profile(c.ID.Pub)
	// c.ID.Name, not the nickKey meta: a name chosen at first run lives only
	// in the identity file, and meta stays empty until the first rename
	nick := c.ID.Name
	if !haveAv && nick == "" {
		return
	}
	for idStr := range c.chatIDs() {
		chat := []byte(idStr)
		st, err := c.ChatState(chat)
		if err != nil {
			continue
		}
		me := st.Members[string(c.ID.Pub)]
		if me == nil || me.Removed {
			continue
		}
		if haveAv && len(av.Blob) > 0 && !bytes.Equal(me.Avatar, av.Blob) {
			c.appendEvent(chat, event.KindAvatar,
				event.Body{Blob: av.Blob, Mime: av.Mime, TS: time.Now().Unix(), NS: av.AvatarTS})
		}
		if me.Status != av.Status || me.Bio != av.Bio || me.Style != av.Style || !bytes.Equal(me.Banner, av.Banner) {
			c.appendEvent(chat, event.KindProfile, cardBody(av))
		}
		if nick != "" && me.Name != nick {
			c.appendEvent(chat, event.KindNick,
				event.Body{Name: nick, TS: time.Now().Unix()})
		}
	}
}

// append a locally-authored event: build on current heads, validate by fold,
// persist and gossip to online members.
func (c *Core) appendEvent(chat []byte, kind string, body event.Body) (*event.Event, error) {
	st, err := c.ChatState(chat)
	if err != nil {
		return nil, err
	}
	keys, mkeys, err := c.chatKeys(chat)
	if err != nil {
		return nil, err
	}
	key, ok := keys[st.Epoch]
	if !ok {
		if st.NoHistory {
			return nil, errors.New("czekam na nowy klucz od właściciela czatu")
		}
		return nil, fmt.Errorf("brak klucza epoki %d", st.Epoch)
	}
	// heads and lamport from single read: Fold requires Lamport = max(parents)+1,
	// so these two values must come from the same snapshot
	if kind == event.KindMsg || kind == event.KindFile {
		c.mu.Lock()
		body.Ch = c.chanSel[string(chat)]
		c.mu.Unlock()
	}
	heads := c.St.Heads(chat)
	e, err := event.New(c.ID.Priv, chat, kind, body, c.St.LamportAfter(chat, heads), heads, st.Epoch, key)
	if err != nil {
		return nil, err
	}
	// validate against the fold before persisting
	f, gen := c.checkout(chat)
	f, err = c.foldWith(chat, f, gen, e, keys, mkeys)
	if err != nil {
		return nil, err
	}
	if _, err := c.St.AddEvent(chat, e); err != nil {
		c.invalidate(chat)
		return nil, err
	}
	c.publish(chat, gen, f, nil)
	// same bookkeeping as on the receiving side (ingest): an attachment we
	// authored ourselves must land in the blob-to-chat index too
	if blobKind(kind) {
		c.noteAttachment(chat, e)
	}
	c.gossip(chat, e, nil)
	c.Notify()
	return e, nil
}

// CreateChat starts a group. noHistory: people added later do not get the
// content keys from before they joined (see event.State.NoHistory).
func (c *Core) CreateChat(name string, noHistory bool) ([]byte, error) {
	key := crypt.NewKey()
	// Blob / MHash = sha256 of the epoch-1 keys: the genesis event vouches for
	// them, so a peer cannot hand us different ones later (see handleKeys).
	sum, msum := keySums(key)
	e, err := event.New(c.ID.Priv, make([]byte, 32), event.KindCreate,
		event.Body{Name: name, Text: c.ID.Name, Blob: sum, MHash: msum, NoHist: noHistory, TS: time.Now().Unix()}, 1, nil, 1, key)
	if err != nil {
		return nil, err
	}
	chat := e.ID()
	if _, err := c.St.AddEvent(chat, e); err != nil {
		return nil, err
	}
	if err := c.St.PutChatKey(chat, 1, key); err != nil {
		return nil, err
	}
	if err := c.St.PutChat(chat, store.ChatMeta{Name: name}); err != nil {
		return nil, err
	}
	c.invalidate(chat)
	c.announceProfile() // avatar and name immediately, without waiting for next loop
	c.Notify()
	return chat, nil
}

// CreateDirect: unnamed chat + immediate member add; friend gets it on next sync.
func (c *Core) CreateDirect(friend store.Friend) ([]byte, error) {
	// reuse the existing 1:1 with this friend, the lowest ID if there are twins
	var found []byte
	for idStr := range c.chatIDs() {
		st, err := c.ChatState([]byte(idStr))
		if err == nil && bytes.Equal(c.directPeer(st), friend.Pub) && (found == nil || idStr < string(found)) {
			found = []byte(idStr)
		}
	}
	if found != nil {
		return found, nil
	}
	chat, err := c.CreateChat("", false)
	if err != nil {
		return nil, err
	}
	// the fingerprint placeholder from a friend code is no name: signed into
	// the add it would outrank the real one "hello" brings later
	name := friend.Display()
	if name == identity.Fingerprint(friend.Pub)[:9] {
		name = ""
	}
	if _, err := c.appendEvent(chat, event.KindAdd, event.Body{Pub: friend.Pub, Name: name, TS: time.Now().Unix()}); err != nil {
		return nil, err
	}
	go c.ensurePeer(friend.Pub)
	return chat, nil
}

func (c *Core) Send(chat []byte, text string) error { return c.SendReply(chat, text, nil) }

// SendReply sends text as an answer to message reply (nil = a plain message).
func (c *Core) SendReply(chat []byte, text string, reply []byte) error {
	c.markActive(chat)
	_, err := c.appendEvent(chat, event.KindMsg, event.Body{Text: text, Target: reply, TS: time.Now().Unix()})
	return err
}

// PinMsg pins (or with unpin, unpins) a message for every member.
func (c *Core) PinMsg(chat, targetID []byte, unpin bool) error {
	chat = c.holding(chat, targetID)
	_, err := c.appendEvent(chat, event.KindPin, event.Body{Target: targetID, Arch: unpin, TS: time.Now().Unix()})
	return err
}

// SetChannel picks the channel new messages and files in chat go to.
func (c *Core) SetChannel(chat, ch []byte) {
	c.mu.Lock()
	c.chanSel[string(chat)] = ch
	c.mu.Unlock()
}

// AddChannel creates a channel or category and returns its ID. Owner only.
func (c *Core) AddChannel(chat []byte, kind, name string, cat []byte) ([]byte, error) {
	id := make([]byte, 8)
	crand.Read(id)
	return id, c.PutChannel(chat, id, kind, name, cat, false)
}

// PutChannel renames, moves, archives or restores a channel or category, or
// with an empty name deletes it for good. An empty id is the default channel
// of that kind. Owner only.
func (c *Core) PutChannel(chat, id []byte, kind, name string, cat []byte, arch bool) error {
	st, err := c.ChatState(chat)
	if err != nil {
		return err
	}
	if !bytes.Equal(st.Owner, c.ID.Pub) {
		return errors.New("tylko właściciel może zmieniać kanały")
	}
	_, err = c.appendEvent(chat, event.KindChannel, event.Body{Target: id, Name: name, Text: kind, Ch: cat, Arch: arch, TS: time.Now().Unix()})
	return err
}

func (c *Core) Rename(chat []byte, name string) error {
	st, err := c.ChatState(chat)
	if err != nil {
		return err
	}
	if !bytes.Equal(st.Owner, c.ID.Pub) {
		return errors.New("tylko właściciel może zmienić nazwę czatu")
	}
	_, err = c.appendEvent(chat, event.KindRename, event.Body{Name: name, TS: time.Now().Unix()})
	if err == nil {
		c.St.PutChat(chat, store.ChatMeta{Name: name})
	}
	return err
}

// Kick removes a member (owner only, enforced by Fold) and rotates the key.
// Removal cascades to invited members (event.Fold), and active connection is dropped
// immediately — otherwise kicked member continues communicating with handlers.
func (c *Core) Kick(chat []byte, pub []byte) error {
	if _, err := c.appendEvent(chat, event.KindRemove, event.Body{Pub: pub, TS: time.Now().Unix()}); err != nil {
		return err
	}
	err := c.Rekey(chat)
	c.dropUnauthorized()
	return err
}

// dropUnauthorized closes connections to peers we no longer accept
// (same rule as inbound — acceptPeer).
func (c *Core) dropUnauthorized() {
	for _, p := range c.P2P.Peers() {
		if !c.acceptPeer(p.Pub, nil) {
			p.Close()
		}
	}
}

// Leave takes us out of the chat: a signed "leave" goes to the members online
// now (they pass it on), then the chat is deleted here and a friend's later
// sync no longer brings it back. The owner cannot hand the chat over, so they
// may only leave once they are its last member. A member may only leave while
// another member is online to receive the leave.
func (c *Core) Leave(chat []byte) error {
	st, err := c.ChatState(chat)
	if err != nil {
		return err
	}
	if bytes.Equal(st.Owner, c.ID.Pub) {
		for _, m := range st.Members {
			if !m.Removed && !bytes.Equal(m.Pub, c.ID.Pub) {
				return errors.New("jesteś właścicielem: najpierw wyrzuć pozostałych członków")
			}
		}
	} else {
		// someone has to carry the leave: sent to nobody, it would never
		// arrive, and we would stay a member over there for good
		online := false
		for _, m := range st.Members {
			if !m.Removed && !bytes.Equal(m.Pub, c.ID.Pub) && c.Online(m.Pub) {
				online = true
				break
			}
		}
		if !online {
			return errors.New("nikt z serwera nie jest teraz online: spróbuj, gdy ktoś będzie")
		}
		if _, err := c.appendEvent(chat, event.KindLeave, event.Body{TS: time.Now().Unix()}); err != nil {
			return err
		}
	}
	if err := c.St.PutMeta(leftKey(chat), []byte{1}); err != nil {
		return err
	}
	if err := c.St.DeleteChat(chat); err != nil {
		return err
	}
	c.invalidate(chat)
	// links kept only for this chat go too, but not before the leave is out:
	// closing at once dropped it unsent
	time.AfterFunc(5*time.Second, c.dropUnauthorized)
	c.Notify()
	return nil
}

func leftKey(chat []byte) string { return "left:" + hex.EncodeToString(chat) }

func (c *Core) left(chat []byte) bool { return c.St.Meta(leftKey(chat)) != nil }

// rekeyAfterLeave: whoever left must not read what comes next, same as a kick.
func (c *Core) rekeyAfterLeave(chat []byte) {
	if st, err := c.ChatState(chat); err == nil && bytes.Equal(st.Owner, c.ID.Pub) {
		if err := c.Rekey(chat); err != nil {
			log.Printf("core: rekey after leave: %v", err)
		}
	}
}

// Rekey bumps the epoch: rekey event (encrypted with old key) + fresh key
// pushed over E2E ctrl channels to remaining online members; offline members
// fetch it via keyreq when they return.
func (c *Core) Rekey(chat []byte) error {
	st, err := c.ChatState(chat)
	if err != nil {
		return err
	}
	// the rekey event announces sha256 of the key the next epoch will use, so
	// members can tell the real key from one a peer made up
	newKey := crypt.NewKey()
	sum, msum := keySums(newKey)
	if _, err := c.appendEvent(chat, event.KindRekey,
		event.Body{Blob: sum, MHash: msum, TS: time.Now().Unix()}); err != nil {
		return err
	}
	if err := c.St.PutChatKey(chat, st.Epoch+1, newKey); err != nil {
		return err
	}
	c.invalidate(chat)
	c.pushKeys(chat, nil)
	c.Notify()
	return nil
}

// pushKeys sends all epochs to online active members (or one peer if pub set).
func (c *Core) pushKeys(chat []byte, pub []byte) {
	st, err := c.ChatState(chat)
	if err != nil {
		return
	}
	keys, mkeys, err := c.chatKeys(chat)
	if err != nil {
		return
	}
	for _, m := range st.Members {
		if m.Removed || bytes.Equal(m.Pub, c.ID.Pub) {
			continue
		}
		if pub != nil && !bytes.Equal(m.Pub, pub) {
			continue
		}
		if p := c.P2P.Peer(m.Pub); p != nil {
			p.SendCtrl(keysFor(chat, st, m, keys, mkeys))
		}
	}
}

// keysFor: the keys member m may hold. In a chat without history, content
// keys only from m.JoinEpoch on; member keys (enough to fold membership and
// profiles) for every epoch.
func keysFor(chat []byte, st *event.State, m *event.Member, keys, mkeys map[uint32][]byte) Ctrl {
	msg := Ctrl{T: "keys", Chat: chat, Epochs: keys}
	if !st.NoHistory {
		return msg
	}
	msg.Epochs, msg.MEpochs = map[uint32][]byte{}, maps.Clone(mkeys)
	for ep, k := range keys {
		if ep >= m.JoinEpoch {
			msg.Epochs[ep] = k
		} else {
			msg.MEpochs[ep] = event.MemberKey(k)
		}
	}
	return msg
}

func keySums(key []byte) (sum, msum []byte) {
	a, b := sha256.Sum256(key), sha256.Sum256(event.MemberKey(key))
	return a[:], b[:]
}

// SetHistory: owner only. on = members added from now on see earlier
// messages. Turning it on also hands the older keys to members already in,
// who joined while history was hidden; turning it off takes nothing back.
func (c *Core) SetHistory(chat []byte, on bool) error {
	st, err := c.ChatState(chat)
	if err != nil {
		return err
	}
	if !bytes.Equal(st.Owner, c.ID.Pub) {
		return errors.New("tylko właściciel może zmienić to ustawienie")
	}
	if st.NoHistory == !on {
		return nil
	}
	if _, err := c.appendEvent(chat, event.KindHistory, event.Body{NoHist: !on, TS: time.Now().Unix()}); err != nil {
		return err
	}
	if on {
		c.pushKeys(chat, nil)
	}
	return nil
}

// autoRekey: in a chat without history the owner opens a new epoch after
// every add, so the newcomer's first content key is one no earlier message
// used.
func (c *Core) autoRekey(chat []byte) {
	st, err := c.ChatState(chat)
	if err != nil || !st.NoHistory || !bytes.Equal(st.Owner, c.ID.Pub) {
		return
	}
	for _, m := range st.Members {
		if !m.Removed && m.JoinEpoch > st.Epoch {
			if err := c.Rekey(chat); err != nil {
				log.Printf("core: auto rekey: %v", err)
			}
			return
		}
	}
}

// gossip sends a fresh event to all online active members except `skip`.
func (c *Core) gossip(chat []byte, e *event.Event, skip []byte) {
	st, err := c.ChatState(chat)
	if err != nil {
		return
	}
	wire := event.Marshal(e)
	msg := Ctrl{T: "events", Chat: chat, Events: [][]byte{wire}}
	for _, m := range st.Members {
		if m.Removed || bytes.Equal(m.Pub, c.ID.Pub) || bytes.Equal(m.Pub, skip) {
			continue
		}
		if p := c.P2P.Peer(m.Pub); p != nil {
			p.SendCtrl(msg)
			// A member added while already connected does not know the chat
			// yet and drops its events (handleEvents): only a sync bootstraps
			// it, and onPeerUp has long passed. Old members just see known heads.
			if e.Kind == event.KindAdd {
				p.SendCtrl(Ctrl{T: "sync", Chat: chat, Heads: c.St.Heads(chat)})
			}
		}
	}
}

// ---- connectivity ----

func (c *Core) ensurePeer(pub []byte) {
	if c.P2P.Peer(pub) != nil || bytes.Equal(pub, c.ID.Pub) {
		return
	}
	if on, err := c.conn().Lookup(pub, 3*time.Second); err == nil && on {
		if err := c.P2P.Connect(pub); err != nil {
			log.Printf("core: connect %s: %v", identity.Fingerprint(pub)[:9], err)
		}
	}
}

// activeFor: with QuietLookup a chat keeps its members looked up for this
// long after the user last opened or wrote in it.
const activeFor = 15 * time.Minute

// SetQuietLookup switches QuietLookup (see Config).
func (c *Core) SetQuietLookup(on bool) {
	c.mu.Lock()
	c.Cfg.QuietLookup = on
	c.mu.Unlock()
}

// markActive notes that the user is in this chat. With QuietLookup its
// members are looked up right away instead of at the next round.
func (c *Core) markActive(chat []byte) {
	c.mu.Lock()
	quiet := c.Cfg.QuietLookup
	stale := time.Since(c.active[string(chat)]) > time.Minute
	c.active[string(chat)] = time.Now()
	c.mu.Unlock()
	if !quiet || !stale {
		return
	}
	go func() {
		if st, err := c.ChatState(chat); err == nil {
			for _, m := range st.Members {
				if !m.Removed {
					c.ensurePeer(m.Pub)
				}
			}
		}
	}()
}

// lookupsPerRound keeps connectAll (every 30 s) under the connector's
// 60 lookups/min. Past that, a random slice per round: 100 offline contacts
// are all checked within ~2 min instead of the same 40 failing forever.
const lookupsPerRound = 25

func (c *Core) connectAll() {
	for _, pub := range c.lookupTargets() {
		go c.ensurePeer(pub)
	}
}

// lookupTargets: who to ask the connector about this round.
func (c *Core) lookupTargets() [][]byte {
	seen := map[string]bool{string(c.ID.Pub): true}
	var want [][]byte
	add := func(pub []byte) {
		if !seen[string(pub)] {
			seen[string(pub)] = true
			if c.P2P.Peer(pub) == nil {
				want = append(want, pub)
			}
		}
	}
	c.mu.Lock()
	quiet := c.Cfg.QuietLookup
	active := maps.Clone(c.active)
	c.mu.Unlock()
	if fs, err := c.St.Friends(); err == nil && !quiet {
		for _, f := range fs {
			add(f.Pub)
		}
	}
	for idStr := range c.chatIDs() {
		if quiet && time.Since(active[idStr]) > activeFor {
			continue
		}
		st, err := c.ChatState([]byte(idStr))
		if err != nil {
			continue
		}
		for _, m := range st.Members {
			if !m.Removed {
				add(m.Pub)
			}
		}
	}
	rand.Shuffle(len(want), func(i, j int) { want[i], want[j] = want[j], want[i] })
	c.mu.Lock()
	r := c.redeem
	c.mu.Unlock()
	if r != nil && !seen[string(r.Inviter)] {
		want = append([][]byte{r.Inviter}, want...) // the invite must not wait
	}
	return want[:min(len(want), lookupsPerRound)]
}

func (c *Core) Online(pub []byte) bool { return c.P2P.Peer(pub) != nil }

// Relay: address of the TURN relay our link to pub goes through, "" if direct
// or offline.
func (c *Core) Relay(pub []byte) string {
	if p := c.P2P.Peer(pub); p != nil {
		return p.Relay()
	}
	return ""
}

func (c *Core) onPeerDown(pub []byte) {
	c.mu.Lock()
	for _, call := range c.calls {
		delete(call.Members, string(pub))
	}
	c.mu.Unlock()
	c.Notify()
}

func (c *Core) SetLang(lang string) error {
	l := i18n.SetLang(lang)
	if err := c.St.PutMeta(langKey, []byte(l)); err != nil {
		return err
	}
	c.Notify()
	return nil
}

func (c *Core) Lang() string {
	return i18n.Lang()
}

// FindMembers matches a member by name (case-insensitive) or by fingerprint.
// Names are not unique and anyone can pick any nick, so callers that act on
// the result (kick) must refuse unless exactly one member matches.
func FindMembers(members []event.Member, q string) []event.Member {
	q = strings.TrimSpace(q)
	var out []event.Member
	for _, m := range members {
		if strings.EqualFold(m.Name, q) || strings.EqualFold(identity.Fingerprint(m.Pub), q) {
			out = append(out, m)
		}
	}
	return out
}
