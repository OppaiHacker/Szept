// Package core: the client brain shared by TUI and GUI.
// Owns identity, store, connector link and p2p manager; implements chat
// creation, DAG sync/gossip, invites, friends, key distribution and calls.
package core

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

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
	DataDir   string   `json:"-"`
	// MediaDir: directory browsed by built-in gallery (Android doesn't have
	// single obvious place for photos, and system file picker doesn't
	// show thumbnail grid).
	MediaDir string `json:"media_dir,omitempty"`

	// Connectors: zapisana lista connectorów do przełączania w ustawieniach.
	Connectors []string `json:"connectors,omitempty"`
	// ConnectorLists: adresy HTTP dynamicznych list (tekst, jeden ws:// na
	// linię) — „Odśwież” w ustawieniach dopisuje wpisy do Connectors.
	ConnectorLists []string `json:"connector_lists,omitempty"`

	Accent    string  `json:"accent,omitempty"`     // kolor akcentu, hex RRGGBB
	OutputVol float64 `json:"output_vol,omitempty"` // głośność rozmów 0..2, 0 = domyślna 1
	CamDevice string  `json:"cam_device,omitempty"` // np. /dev/video1
	NoNotify  bool    `json:"no_notify,omitempty"`  // wycisz powiadomienia systemowe

	Profile string `json:"profile,omitempty"` // aktywne konto (profil), "" = domyślne

	WebhookOn    bool   `json:"webhook_on,omitempty"`
	WebhookPort  int    `json:"webhook_port,omitempty"`
	WebhookToken string `json:"webhook_token,omitempty"`
}

// Ctrl is the JSON message format on the reliable "ctrl" data channel.
type Ctrl struct {
	T       string            `json:"t"` // hello|sync|want|events|keyreq|keys|redeem|call|blobreq|blobpart|typing|ack
	Chat    []byte            `json:"chat,omitempty"`
	Heads   [][]byte          `json:"heads,omitempty"`
	IDs     [][]byte          `json:"ids,omitempty"`
	Events  [][]byte          `json:"events,omitempty"`
	Epochs  map[uint32][]byte `json:"epochs,omitempty"`
	Secret  []byte            `json:"secret,omitempty"`
	Pin     string            `json:"pin,omitempty"` // redeem: one-time PIN
	Name    string            `json:"name,omitempty"`
	Action  string            `json:"action,omitempty"` // call: join|leave
	Reply   bool              `json:"reply,omitempty"`  // sync: suppress re-reply
	Typing  bool              `json:"typing,omitempty"`
	EventID []byte            `json:"event_id,omitempty"`
	Status  string            `json:"status,omitempty"`

	// blobreq/blobpart: attachment content, chunked (see blob.go)
	Blob  []byte `json:"blob,omitempty"` // sha256 of the content
	Part  []byte `json:"part,omitempty"`
	Idx   int    `json:"idx,omitempty"`
	Total int    `json:"total,omitempty"`
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

	mu         sync.Mutex
	orphans    map[string]map[string]*event.Event // chatKey -> eventID -> event
	states     map[string]*foldedState            // chatKey -> cached fold (see ChatState)
	redeem     *pendingRedeem
	calls      map[string]*Call
	listeners  []chan struct{}
	wants      map[string]*blobWant            // blob hash -> attachment we still need
	incoming   map[string]*blobAsm             // blob hash -> chunks arriving now
	serving    map[string]int                  // peer -> blob sends in flight
	assemblers map[string]map[uint32]*assembly // peer+label -> frame -> video parts
	typing     map[string]map[string]time.Time // chatKey -> pubKeyStr -> lastTyping
	acks       map[string]map[string]string    // eventIDStr -> pubKeyStr -> status ("delivered"|"read")

	// media hooks (set by calls.go / UIs)
	OnVideoFrame func(chat []byte, from []byte, kind string, jpeg []byte)
	voiceOut     func(src string, pcm []byte) // speaker mixer sink, set when call active
	player       *media.Player                // żyje razem z rozmową (JoinCall/LeaveCall)
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
		orphans:    map[string]map[string]*event.Event{},
		states:     map[string]*foldedState{},
		calls:      map[string]*Call{},
		wants:      map[string]*blobWant{},
		incoming:   map[string]*blobAsm{},
		serving:    map[string]int{},
		assemblers: map[string]map[uint32]*assembly{},
		typing:     map[string]map[string]time.Time{},
		acks:       map[string]map[string]string{},
	}
	c.P2P = p2p.NewManager(id.Priv, conn, cfg.STUN)
	c.P2P.Accept = c.acceptPeer
	c.P2P.OnPeerUp = c.onPeerUp
	c.P2P.OnPeerDown = func(pub []byte) { c.onPeerDown(pub) }
	c.P2P.OnCtrl = c.onCtrl
	c.P2P.OnMedia = c.onMedia
	conn.OnUp = func() { c.connectAll(); c.Notify() }
	conn.OnDown = c.Notify
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

func (c *Core) acceptPeer(pub []byte) bool {
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
	// so invites also allow inbound: anyone may connect IF we have any unused,
	// unexpired invite outstanding; the redeem secret+PIN gate membership.
	// ponytail: leaks "some invite exists" to strangers; acceptable.
	return c.St.OpenInviteCount() > 0
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

// invalidate: call after writing an event or epoch key for this chat.
func (c *Core) invalidate(chat []byte) {
	c.mu.Lock()
	delete(c.states, string(chat))
	c.mu.Unlock()
}

// ChatState returns folded state of chat. Result is read-only — shared by
// all readers until next state mutation.
//
// ponytail: fold still walks entire history, just less frequently (and without
// verifying signatures, as store contains only validated events).
// For histories scaling to tens of thousands of events, next step is incremental
// fold or state snapshot every N events.
func (c *Core) ChatState(chat []byte) (*event.State, error) {
	c.mu.Lock()
	cached := c.states[string(chat)]
	c.mu.Unlock()
	if cached != nil {
		return cached.st, cached.err
	}
	st, err := c.foldChat(chat)
	c.mu.Lock()
	c.states[string(chat)] = &foldedState{st: st, err: err}
	c.mu.Unlock()
	return st, err
}

func (c *Core) foldChat(chat []byte) (*event.State, error) {
	evs, err := c.St.Events(chat)
	if err != nil {
		return nil, err
	}
	keys, err := c.St.ChatKeys(chat)
	if err != nil {
		return nil, err
	}
	return event.FoldTrusted(evs, chat, keys)
}

type ChatInfo struct {
	ID      []byte
	Name    string
	Members []event.Member
	Owner   []byte
	Epoch   uint32
	Online  int
	InCall  []string // member names currently in call
}

func (c *Core) Chats() []ChatInfo {
	var out []ChatInfo
	for idStr := range c.chatIDs() {
		id := []byte(idStr)
		st, err := c.ChatState(id)
		if err != nil {
			continue
		}
		ci := ChatInfo{ID: id, Name: c.displayName(st), Owner: st.Owner, Epoch: st.Epoch}
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
		sort.Slice(ci.Members, func(i, j int) bool { return ci.Members[i].Name < ci.Members[j].Name })
		c.mu.Lock()
		if call := c.calls[idStr]; call != nil {
			for pub := range call.Members {
				ci.InCall = append(ci.InCall, c.memberName(st, []byte(pub)))
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
const MaxName = 64

// safeText cleanses incoming network text before rendering in UI: control characters
// are stripped because TUI outputs text directly to terminal, and ANSI/OSC sequences
// could redraw screen, spoof messages or tamper with clipboard. Newlines and tabs remain.
func safeText(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return -1
		}
		return r
	}, s)
}

// safeName: single-line name (author, chat, friend) with capped length.
func safeName(s string) string {
	s = strings.ReplaceAll(safeText(s), "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	if len(s) > MaxName {
		s = strings.ToValidUTF8(s[:MaxName], "")
	}
	return s
}

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
				return safeName(f.Name)
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
		keys, _ := c.St.ChatKeys(chat)
		event.SortCanonical(evs) // events within one chat in DAG order
		for _, e := range evs {
			if e.Kind != event.KindNick && e.Kind != event.KindCreate {
				continue
			}
			if !bytes.Equal(e.Author, pub) {
				continue
			}
			key, ok := keys[e.Epoch]
			if !ok {
				continue
			}
			b, err := e.Decrypt(key)
			if err != nil {
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
	Kind      string // msg | info | file
	Info      string // for Kind == "info": event.Kind* — client selects icon and color
	Deleted   bool   // deleted for everyone in DAG
	Reactions map[string]string // pubKeyStr -> emoji
	ACK       string // "" | "delivered" | "read"

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

func (c *Core) Messages(chat []byte) []Msg {
	evs, err := c.St.Events(chat)
	if err != nil {
		return nil
	}
	keys, _ := c.St.ChatKeys(chat)
	st, errFold := c.ChatState(chat)
	if errFold != nil {
		st = &event.State{Members: map[string]*event.Member{}, Reactions: map[string]map[string]string{}, Deleted: map[string]bool{}}
	}
	event.SortCanonical(evs)
	hidden := c.St.Hidden(chat)
	var out []Msg
	for _, e := range evs {
		key, ok := keys[e.Epoch]
		if !ok {
			continue
		}
		id := e.ID()
		if hidden[string(id)] {
			continue // hidden locally (soft delete) — bytes remain in DAG
		}
		b, err := e.Decrypt(key)
		if err != nil {
			continue
		}
		name := c.memberName(st, e.Author)
		m := Msg{ID: id, Author: e.Author, Name: name, TS: time.Unix(b.TS, 0)}
		switch e.Kind {
		case event.KindMsg:
			m.Kind, m.Text = "msg", safeText(b.Text)
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
		case event.KindRekey:
			m.Kind, m.Info, m.Text = "info", e.Kind, "key rotation (new epoch)"
		default:
			continue // avatar, nick, rename, react, delete — not separate message items
		}
		if st.Deleted != nil && st.Deleted[string(id)] {
			m.Deleted = true
			m.Text = "[Wiadomość usunięta]"
		}
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
	c.mu.Lock()
	defer c.mu.Unlock()
	m := c.typing[string(chat)]
	if m == nil {
		return nil
	}
	st, _ := c.foldChat(chat)
	var names []string
	now := time.Now()
	for pubStr, t := range m {
		if now.Sub(t) < 4*time.Second {
			names = append(names, c.memberName(st, []byte(pubStr)))
		}
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
	evs, err := c.St.Events(chat)
	if err != nil {
		return
	}
	for _, e := range evs {
		if !bytes.Equal(e.Author, c.ID.Pub) {
			c.SendAck(chat, e.ID(), "read")
		}
	}
}

func (c *Core) ReactMsg(chat []byte, targetID []byte, emoji string) error {
	_, err := c.appendEvent(chat, event.KindReact, event.Body{Target: targetID, Text: emoji, TS: time.Now().Unix()})
	return err
}

func (c *Core) DeleteMsg(chat []byte, targetID []byte) error {
	_, err := c.appendEvent(chat, event.KindDelete, event.Body{Target: targetID, TS: time.Now().Unix()})
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
	av, haveAv := c.St.Avatar(c.ID.Pub)
	nick := string(c.St.Meta(nickKey))
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
		if haveAv && !bytes.Equal(me.Avatar, av.Blob) {
			c.appendEvent(chat, event.KindAvatar,
				event.Body{Blob: av.Blob, Mime: av.Mime, TS: time.Now().Unix()})
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
	keys, err := c.St.ChatKeys(chat)
	if err != nil {
		return nil, err
	}
	key, ok := keys[st.Epoch]
	if !ok {
		return nil, fmt.Errorf("brak klucza epoki %d", st.Epoch)
	}
	// heads and lamport from single read: Fold requires Lamport = max(parents)+1,
	// so these two values must come from the same snapshot
	heads := c.St.Heads(chat)
	e, err := event.New(c.ID.Priv, chat, kind, body, c.St.LamportAfter(chat, heads), heads, st.Epoch, key)
	if err != nil {
		return nil, err
	}
	// validate against the fold before persisting
	evs, _ := c.St.Events(chat)
	if _, err := event.FoldTrusted(append(evs, e), chat, keys); err != nil {
		return nil, err
	}
	if _, err := c.St.AddEvent(chat, e); err != nil {
		return nil, err
	}
	c.invalidate(chat)
	c.gossip(chat, e, nil)
	c.Notify()
	return e, nil
}

func (c *Core) CreateChat(name string) ([]byte, error) {
	key := crypt.NewKey()
	e, err := event.New(c.ID.Priv, make([]byte, 32), event.KindCreate,
		event.Body{Name: name, Text: c.ID.Name, TS: time.Now().Unix()}, 1, nil, 1, key)
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
	// reuse existing 1:1 with this friend if present
	for idStr := range c.chatIDs() {
		st, err := c.ChatState([]byte(idStr))
		if err != nil || st.Name != "" {
			continue
		}
		if len(st.Members) == 2 && st.IsMember(friend.Pub) {
			return []byte(idStr), nil
		}
	}
	chat, err := c.CreateChat("")
	if err != nil {
		return nil, err
	}
	if _, err := c.appendEvent(chat, event.KindAdd, event.Body{Pub: friend.Pub, Name: friend.Name, TS: time.Now().Unix()}); err != nil {
		return nil, err
	}
	go c.ensurePeer(friend.Pub)
	return chat, nil
}

func (c *Core) Send(chat []byte, text string) error {
	_, err := c.appendEvent(chat, event.KindMsg, event.Body{Text: text, TS: time.Now().Unix()})
	return err
}

func (c *Core) Rename(chat []byte, name string) error {
	_, err := c.appendEvent(chat, event.KindRename, event.Body{Name: name, TS: time.Now().Unix()})
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
		if !c.acceptPeer(p.Pub) {
			p.Close()
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
	if _, err := c.appendEvent(chat, event.KindRekey, event.Body{TS: time.Now().Unix()}); err != nil {
		return err
	}
	newKey := crypt.NewKey()
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
	keys, err := c.St.ChatKeys(chat)
	if err != nil {
		return
	}
	msg := Ctrl{T: "keys", Chat: chat, Epochs: keys}
	for _, m := range st.Members {
		if m.Removed || bytes.Equal(m.Pub, c.ID.Pub) {
			continue
		}
		if pub != nil && !bytes.Equal(m.Pub, pub) {
			continue
		}
		if p := c.P2P.Peer(m.Pub); p != nil {
			p.SendCtrl(msg)
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

func (c *Core) connectAll() {
	seen := map[string]bool{string(c.ID.Pub): true}
	if fs, err := c.St.Friends(); err == nil {
		for _, f := range fs {
			if !seen[string(f.Pub)] {
				seen[string(f.Pub)] = true
				go c.ensurePeer(f.Pub)
			}
		}
	}
	for idStr := range c.chatIDs() {
		st, err := c.ChatState([]byte(idStr))
		if err != nil {
			continue
		}
		for _, m := range st.Members {
			if !m.Removed && !seen[string(m.Pub)] {
				seen[string(m.Pub)] = true
				go c.ensurePeer(m.Pub)
			}
		}
	}
	c.mu.Lock()
	r := c.redeem
	c.mu.Unlock()
	if r != nil && !seen[string(r.Inviter)] {
		go c.ensurePeer(r.Inviter)
	}
}

func (c *Core) Online(pub []byte) bool { return c.P2P.Peer(pub) != nil }

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
