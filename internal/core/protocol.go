package core

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"szept/internal/crypt"
	"szept/internal/event"
	"szept/internal/identity"
	"szept/internal/p2p"
	"szept/internal/store"
)

// ---- peer lifecycle ----

func (c *Core) onPeerUp(p *p2p.Peer) {
	p.SendCtrl(Ctrl{T: "hello", Name: c.ID.Name})
	// offer sync for every chat this peer belongs to
	for idStr := range c.chatIDs() {
		chat := []byte(idStr)
		if st, err := c.ChatState(chat); err == nil && st.IsMember(p.Pub) {
			p.SendCtrl(Ctrl{T: "sync", Chat: chat, Heads: c.St.Heads(chat)})
		}
	}
	// pending invite redemption?
	c.mu.Lock()
	r := c.redeem
	var inCall [][]byte
	for chatStr, call := range c.calls {
		if call.Joined {
			inCall = append(inCall, []byte(chatStr))
		}
	}
	c.mu.Unlock()
	if r != nil && bytes.Equal(r.Inviter, p.Pub) {
		p.SendCtrl(Ctrl{T: "redeem", Chat: r.Chat, Secret: r.Secret, Pin: r.Pin, Name: c.ID.Name})
	}
	// announce active calls to the newcomer
	for _, chat := range inCall {
		if st, err := c.ChatState(chat); err == nil && st.IsMember(p.Pub) {
			p.SendCtrl(Ctrl{T: "call", Chat: chat, Action: "join"})
		}
	}
	go c.retryWants(true) // peer connected: may hold missing attachments
	c.Notify()
}

// ---- ctrl dispatch ----

// known: whether we share any relationship with this peer beyond a pending invite.
// Strangers admitted during open invite window are only allowed to redeem it —
// other handlers (keys, sync, blobs) are not for random guests.
func (c *Core) known(pub []byte) bool {
	if c.St.IsFriend(pub) {
		return true
	}
	c.mu.Lock()
	r := c.redeem
	c.mu.Unlock()
	if r != nil && bytes.Equal(r.Inviter, pub) {
		return true
	}
	for chatID := range c.chatIDs() {
		if st, err := c.ChatState([]byte(chatID)); err == nil && st.IsMember(pub) {
			return true
		}
	}
	return false
}

func (c *Core) onCtrl(p *p2p.Peer, data []byte) {
	var m Ctrl
	if err := json.Unmarshal(data, &m); err != nil {
		return
	}
	if m.T != "redeem" && m.T != "hello" && !c.known(p.Pub) {
		return
	}
	switch m.T {
	case "hello":
		if m.Name != "" && c.St.IsFriend(p.Pub) {
			fs, _ := c.St.Friends()
			name := safeName(m.Name)
			for _, f := range fs {
				if bytes.Equal(f.Pub, p.Pub) && f.Name != name {
					f.Name = name
					c.St.PutFriend(f)
				}
			}
		}
		c.Notify()
	case "sync":
		c.handleSync(p, m)
	case "want":
		c.handleWant(p, m)
	case "events":
		c.handleEvents(p, m)
	case "keyreq":
		if st, err := c.ChatState(m.Chat); err == nil && st.IsMember(p.Pub) {
			c.pushKeys(m.Chat, p.Pub)
		}
	case "keys":
		c.handleKeys(p, m)
	case "redeem":
		c.handleRedeem(p, m)
	case "call":
		c.handleCallCtrl(p, m)
	case "blobreq":
		c.serveBlob(p, m)
	case "blobpart":
		c.handleBlobPart(p, m)
	case "typing":
		c.handleTyping(p, m)
	case "ack":
		c.handleAck(p, m)
	}
}

func (c *Core) knownChat(chat []byte) bool {
	_, ok := c.chatIDs()[string(chat)]
	return ok
}

func (c *Core) handleSync(p *p2p.Peer, m Ctrl) {
	if !c.knownChat(m.Chat) {
		// bootstrap: only friends or our inviter can introduce a new chat
		c.mu.Lock()
		r := c.redeem
		c.mu.Unlock()
		fromInviter := r != nil && bytes.Equal(r.Inviter, p.Pub) && bytes.Equal(r.Chat, m.Chat)
		if !c.St.IsFriend(p.Pub) && !fromInviter {
			return
		}
		c.St.PutChat(m.Chat, store.ChatMeta{})
		p.SendCtrl(Ctrl{T: "keyreq", Chat: m.Chat})
	}
	// request unknown heads
	var want [][]byte
	for _, h := range m.Heads {
		if !c.St.HasEvent(m.Chat, h) {
			want = append(want, h)
		}
	}
	if len(want) > 0 {
		p.SendCtrl(Ctrl{T: "want", Chat: m.Chat, IDs: want})
	}
	// reply with our heads once, so the other side can pull from us too —
	// but only to members: with an open invite we accept strangers, and a
	// stranger poking a chat ID must not get the DAG map back.
	// (chat with no folded state yet: Heads is empty anyway)
	if !m.Reply {
		if st, err := c.ChatState(m.Chat); err == nil && !st.IsMember(p.Pub) {
			return
		}
		p.SendCtrl(Ctrl{T: "sync", Chat: m.Chat, Heads: c.St.Heads(m.Chat), Reply: true})
	}
}

func (c *Core) handleWant(p *p2p.Peer, m Ctrl) {
	st, err := c.ChatState(m.Chat)
	authorized := err == nil && st.IsMember(p.Pub)
	if !authorized {
		// a joiner we just granted may not be in our fold yet if events raced;
		// invites are the only other path and were membership-checked there.
		return
	}
	var wires [][]byte
	bytesOut := 0
	sent := map[string]bool{}
	queue := append([][]byte(nil), m.IDs...)
	// send requested events plus their ancestors (bounded — items AND bytes:
	// a ctrl message above SCTP limit won't deliver and sync halts)
	for len(queue) > 0 && len(wires) < 2000 && bytesOut < 1<<20 {
		id := queue[0]
		queue = queue[1:]
		if sent[string(id)] {
			continue
		}
		e, err := c.St.GetEvent(m.Chat, id)
		if err != nil {
			continue
		}
		sent[string(id)] = true
		w := event.Marshal(e)
		bytesOut += len(w)
		wires = append(wires, w)
		queue = append(queue, e.Parents...)
	}
	if len(wires) > 0 {
		p.SendCtrl(Ctrl{T: "events", Chat: m.Chat, Events: wires})
	}
}

func (c *Core) handleEvents(p *p2p.Peer, m Ctrl) {
	if !c.knownChat(m.Chat) {
		return
	}
	// sender must be a member — or the chat has no folded state yet (bootstrap)
	if st, err := c.ChatState(m.Chat); err == nil && !st.IsMember(p.Pub) {
		return
	}
	fresh := 0
	for _, w := range m.Events {
		e, err := event.Unmarshal(w)
		if err != nil {
			continue
		}
		if c.ingest(m.Chat, e, p) {
			fresh++
		}
	}
	if fresh > 0 {
		c.drainOrphans(m.Chat, p)
		c.redeemDone(m.Chat)
		c.Notify()
	}
}

// redeemDone closes redeemed invite only when we are truly
// in the chat. Previously it was the only link connecting us with inviter —
// deleting too early cuts them off via known() gate halfway through joining.
func (c *Core) redeemDone(chat []byte) {
	st, err := c.ChatState(chat)
	if err != nil || !st.IsMember(c.ID.Pub) {
		return
	}
	c.mu.Lock()
	if c.redeem != nil && bytes.Equal(c.redeem.Chat, chat) {
		c.redeem = nil
	}
	c.mu.Unlock()
}

// ingest verifies and persists one event; returns true if newly stored.
// Missing parents or missing epoch keys park the event in the orphan buffer.
func (c *Core) ingest(chat []byte, e *event.Event, from *p2p.Peer) bool {
	id := e.ID()
	if c.St.HasEvent(chat, id) {
		return false
	}
	if !e.VerifySig() {
		return false
	}
	// parents present?
	var missing [][]byte
	for _, par := range e.Parents {
		if !c.St.HasEvent(chat, par) {
			missing = append(missing, par)
		}
	}
	if len(missing) > 0 {
		c.park(chat, e)
		if from != nil {
			from.SendCtrl(Ctrl{T: "want", Chat: chat, IDs: missing})
		}
		return false
	}
	// validate the DAG with this event included
	evs, err := c.St.Events(chat)
	if err != nil {
		return false
	}
	keys, _ := c.St.ChatKeys(chat)
	// FoldTrusted: signature of e was checked above, rest is already in database
	if _, err := event.FoldTrusted(append(evs, e), chat, keys); err != nil {
		if strings.Contains(err.Error(), "missing key") {
			c.park(chat, e)
			if from != nil {
				from.SendCtrl(Ctrl{T: "keyreq", Chat: chat})
			}
		}
		return false
	}
	if fresh, err := c.St.AddEvent(chat, e); err != nil || !fresh {
		return false
	}
	c.invalidate(chat)
	// keep chat name cache fresh for the UI list
	if e.Kind == event.KindCreate || e.Kind == event.KindRename {
		if st, err := c.ChatState(chat); err == nil {
			c.St.PutChat(chat, store.ChatMeta{Name: st.Name})
		}
	}
	// attachment or avatar: request missing content
	if e.Kind == event.KindFile || e.Kind == event.KindAvatar {
		c.noteAttachment(chat, e)
	}
	var skip []byte
	if from != nil {
		skip = from.Pub
		if !bytes.Equal(e.Author, c.ID.Pub) {
			from.SendCtrl(Ctrl{T: "ack", Chat: chat, EventID: id, Status: "delivered"})
		}
	}
	c.gossip(chat, e, skip)
	return true
}

func (c *Core) handleTyping(p *p2p.Peer, m Ctrl) {
	c.mu.Lock()
	if c.typing == nil {
		c.typing = make(map[string]map[string]time.Time)
	}
	chatStr := string(m.Chat)
	if c.typing[chatStr] == nil {
		c.typing[chatStr] = make(map[string]time.Time)
	}
	pubStr := string(p.Pub)
	if m.Typing {
		c.typing[chatStr][pubStr] = time.Now()
	} else {
		delete(c.typing[chatStr], pubStr)
	}
	c.mu.Unlock()
	c.Notify()
}

func (c *Core) handleAck(p *p2p.Peer, m Ctrl) {
	if len(m.EventID) == 0 || m.Status == "" {
		return
	}
	c.mu.Lock()
	if c.acks == nil {
		c.acks = make(map[string]map[string]string)
	}
	idStr := string(m.EventID)
	if c.acks[idStr] == nil {
		c.acks[idStr] = make(map[string]string)
	}
	pubStr := string(p.Pub)
	if current := c.acks[idStr][pubStr]; current != "read" {
		c.acks[idStr][pubStr] = m.Status
	}
	c.mu.Unlock()
	c.Notify()
}

// maxOrphans: ceiling on orphans IN TOTAL, not per chat — friend can create
// arbitrarily many chats (bootstrap in handleSync), so limit per chat is no limit.
const maxOrphans = 5000

func (c *Core) park(chat []byte, e *event.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	total := 0
	for _, m := range c.orphans {
		total += len(m)
	}
	if total >= maxOrphans {
		return
	}
	key := string(chat)
	if c.orphans[key] == nil {
		c.orphans[key] = map[string]*event.Event{}
	}
	c.orphans[key][string(e.ID())] = e
}

func (c *Core) drainOrphans(chat []byte, from *p2p.Peer) {
	for {
		c.mu.Lock()
		var batch []*event.Event
		for _, e := range c.orphans[string(chat)] {
			batch = append(batch, e)
		}
		c.mu.Unlock()
		if len(batch) == 0 {
			return
		}
		progress := false
		for _, e := range batch {
			if c.ingest(chat, e, from) {
				c.mu.Lock()
				delete(c.orphans[string(chat)], string(e.ID()))
				c.mu.Unlock()
				progress = true
			}
		}
		if !progress {
			return
		}
	}
}

// handleKeys accepts epoch keys. Only from chat member (or during
// joining, when state does not exist yet — then only from inviter):
// spoofed key cannot be removed and breaks epoch forever.
func (c *Core) handleKeys(p *p2p.Peer, m Ctrl) {
	if !c.knownChat(m.Chat) {
		return
	}
	if st, err := c.ChatState(m.Chat); err == nil {
		if !st.IsMember(p.Pub) {
			return
		}
	} else {
		// chat without folded state = joining in progress; keys can be provided
		// by the same peer that introduced the chat (see handleSync)
		c.mu.Lock()
		r := c.redeem
		c.mu.Unlock()
		fromInviter := r != nil && bytes.Equal(r.Inviter, p.Pub) && bytes.Equal(r.Chat, m.Chat)
		if !c.St.IsFriend(p.Pub) && !fromInviter {
			return
		}
	}
	existing, err := c.St.ChatKeys(m.Chat)
	if err != nil {
		return
	}
	added := false
	for ep, k := range m.Epochs {
		if _, have := existing[ep]; have || len(k) != 32 {
			continue // never overwrite known keys
		}
		if err := c.St.PutChatKey(m.Chat, ep, k); err == nil {
			added = true
		}
	}
	if added {
		c.invalidate(m.Chat)
		c.drainOrphans(m.Chat, p)
		go c.announceProfile() // new chat = announce profile in it
		// with key in hand attachment metadata can finally be decrypted
		go c.rescanAttachments()
		// with keys in hand, pull the DAG
		p.SendCtrl(Ctrl{T: "sync", Chat: m.Chat, Heads: c.St.Heads(m.Chat)})
		c.redeemDone(m.Chat)
		c.Notify()
	}
}

// handleRedeem: inviter side — verify one-time secret + PIN + expiry,
// add member, hand over keys.
func (c *Core) handleRedeem(p *p2p.Peer, m Ctrl) {
	h := sha256.Sum256(m.Secret)
	inv, err := c.St.TakeInvite(h[:], []byte(m.Pin))
	if err != nil || !bytes.Equal(inv.Chat, m.Chat) {
		return
	}
	name := safeName(m.Name)
	if name == "" {
		name = identity.Fingerprint(p.Pub)[:9]
	}
	if st, err := c.ChatState(m.Chat); err == nil && !st.IsMember(p.Pub) {
		if _, err := c.appendEvent(m.Chat, event.KindAdd, event.Body{Pub: p.Pub, Name: name, TS: time.Now().Unix()}); err != nil {
			return
		}
	}
	c.pushKeys(m.Chat, p.Pub)
	p.SendCtrl(Ctrl{T: "sync", Chat: m.Chat, Heads: c.St.Heads(m.Chat)})
	c.Notify()
}

// ---- invites & friend codes ----

const (
	invPrefix    = "SZC-" // chat invite
	friendPrefix = "SZF-" // friend code
)

// InviteTTL: an invite dies after this much time or after one successful join.
const InviteTTL = 10 * time.Minute

// NewInvite issues a one-time invite for a chat. Returns the code and a
// 6-digit PIN to hand over out-of-band; joining requires both.
//
// Only chat creator: invite means letting a stranger into shared,
// encrypted history, so decision belongs to one person. Constraint sits here,
// not in UI — TUI and GUI call the same method.
func (c *Core) NewInvite(chat []byte) (code, pin string, err error) {
	if st, err := c.ChatState(chat); err != nil {
		return "", "", err
	} else if !st.IsMember(c.ID.Pub) {
		return "", "", errors.New("nie jesteś członkiem")
	} else if !bytes.Equal(st.Owner, c.ID.Pub) {
		return "", "", errors.New("zaproszenia wystawia tylko założyciel czatu")
	}
	secret := makeSecret()
	pin = makePin()
	h := sha256.Sum256(secret)
	ph := sha256.Sum256([]byte(pin))
	exp := time.Now().Add(InviteTTL).Unix()
	if err := c.St.PutInvite(h[:], store.Invite{Chat: chat, PinHash: ph[:], Exp: exp}); err != nil {
		return "", "", err
	}
	blob, _ := json.Marshal(map[string]any{
		"c": chat, "i": []byte(c.ID.Pub), "s": secret, "u": c.connURL(), "e": exp,
	})
	return invPrefix + base64.RawURLEncoding.EncodeToString(blob), pin, nil
}

func (c *Core) connURL() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Cfg.Connector
}

type ParsedInvite struct {
	Chat      []byte
	Inviter   []byte
	Secret    []byte
	Connector string
	Exp       int64
}

func ParseInvite(code string) (*ParsedInvite, error) {
	if !strings.HasPrefix(code, invPrefix) {
		return nil, errors.New("to nie jest kod zaproszenia")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(code[len(invPrefix):]))
	if err != nil {
		return nil, err
	}
	var v struct {
		C []byte `json:"c"`
		I []byte `json:"i"`
		S []byte `json:"s"`
		U string `json:"u"`
		E int64  `json:"e"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	if len(v.C) != 32 || len(v.I) != ed25519.PublicKeySize || len(v.S) == 0 {
		return nil, errors.New("uszkodzony kod")
	}
	return &ParsedInvite{Chat: v.C, Inviter: v.I, Secret: v.S, Connector: v.U, Exp: v.E}, nil
}

// Redeem joins a chat from an invite code + PIN. The inviter must be online.
func (c *Core) Redeem(code, pin string) error {
	inv, err := ParseInvite(code)
	if err != nil {
		return err
	}
	if inv.Exp > 0 && time.Now().Unix() > inv.Exp {
		return errors.New("zaproszenie wygasło (ważne 10 minut) — poproś o nowe")
	}
	if inv.Connector != "" && inv.Connector != c.connURL() {
		return fmt.Errorf("zaproszenie wskazuje inny connector (%s) — zmień go w ustawieniach", inv.Connector)
	}
	pin = strings.TrimSpace(pin)
	c.mu.Lock()
	c.redeem = &pendingRedeem{Chat: inv.Chat, Inviter: inv.Inviter, Secret: inv.Secret, Pin: pin}
	c.mu.Unlock()
	go c.ensurePeer(inv.Inviter)
	// if already connected, fire immediately
	if p := c.P2P.Peer(inv.Inviter); p != nil {
		p.SendCtrl(Ctrl{T: "redeem", Chat: inv.Chat, Secret: inv.Secret, Pin: pin, Name: c.ID.Name})
	}
	return nil
}

// FriendCode: shareable identity card (pubkey + name + connector hint).
func (c *Core) FriendCode() string {
	blob, _ := json.Marshal(map[string]any{
		"p": []byte(c.ID.Pub), "n": c.ID.Name, "u": c.connURL(),
	})
	return friendPrefix + base64.RawURLEncoding.EncodeToString(blob)
}

func (c *Core) AddFriend(code string) (store.Friend, error) {
	var f store.Friend
	if !strings.HasPrefix(code, friendPrefix) {
		return f, errors.New("to nie jest kod znajomego")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(code[len(friendPrefix):]))
	if err != nil {
		return f, err
	}
	var v struct {
		P []byte `json:"p"`
		N string `json:"n"`
		U string `json:"u"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return f, err
	}
	if len(v.P) != ed25519.PublicKeySize {
		return f, errors.New("uszkodzony kod")
	}
	if bytes.Equal(v.P, c.ID.Pub) {
		return f, errors.New("to twój własny kod")
	}
	f = store.Friend{Pub: v.P, Name: v.N, Connector: v.U}
	if err := c.St.PutFriend(f); err != nil {
		return f, err
	}
	go c.ensurePeer(f.Pub)
	c.Notify()
	return f, nil
}

func makeSecret() []byte {
	return crypt.RandBytes(16)
}

// makePin: 6 random digits from crypto/rand.
func makePin() string {
	b := crypt.RandBytes(4)
	n := binary.BigEndian.Uint32(b) % 1000000
	return fmt.Sprintf("%06d", n)
}

// NormalizeWS: users paste bare host:port — prepend ws:// for them.
func NormalizeWS(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "ws://") || strings.HasPrefix(s, "wss://") {
		return s
	}
	return "ws://" + s
}
