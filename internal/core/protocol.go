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
	"net/url"
	"strings"
	"time"

	"golang.org/x/time/rate"

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
		if st, err := c.ChatState(chatOf(chat)); err == nil && st.IsMember(p.Pub) {
			p.SendCtrl(Ctrl{T: "call", Chat: chat, Action: "join", Status: c.ownFlags(chat)})
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
	if m, ok := parseBlobFrame(data); ok {
		if c.known(p.Pub) {
			c.handleBlobPart(p, m)
		}
		return
	}
	var m Ctrl
	if err := json.Unmarshal(data, &m); err != nil {
		return
	}
	if m.T != "redeem" && m.T != "hello" && !c.known(p.Pub) {
		return
	}
	switch m.T {
	case "hello":
		// a peer names itself, never you: "hello" only ever writes PeerName, so a
		// name the user gave this contact stays put (see store.Friend)
		if m.Name != "" && c.St.IsFriend(p.Pub) {
			fs, _ := c.St.Friends()
			name := safeName(m.Name)
			for _, f := range fs {
				if bytes.Equal(f.Pub, p.Pub) && f.PeerName != name {
					f.PeerName = name
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

// maxPendingChats: chats a peer has announced but which never folded into a
// usable state (no genesis, or we are not in them). They cost disk and CPU, so
// the number a peer can leave lying around is capped.
const maxPendingChats = 32

func (c *Core) pendingChats() int {
	n := 0
	for idStr := range c.chatIDs() {
		if _, err := c.ChatState([]byte(idStr)); err != nil {
			n++
		}
	}
	return n
}

func (c *Core) handleSync(p *p2p.Peer, m Ctrl) {
	if !c.knownChat(m.Chat) {
		// bootstrap: only friends or our inviter can introduce a new chat
		c.mu.Lock()
		r := c.redeem
		c.mu.Unlock()
		fromInviter := r != nil && bytes.Equal(r.Inviter, p.Pub) && bytes.Equal(r.Chat, m.Chat)
		// a chat we left comes back only through a new invite
		if (!c.St.IsFriend(p.Pub) || c.left(m.Chat)) && !fromInviter {
			return
		}
		if len(m.Chat) != sha256.Size || c.pendingChats() >= maxPendingChats {
			// a friend introducing chats in a loop would otherwise fill the
			// database with empty ones, and every one of them is then walked by
			// connectAll, announceProfile, rescanAttachments and known()
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
	if len(m.Events) > maxEventsPerFrame {
		m.Events = m.Events[:maxEventsPerFrame] // symmetric with handleWant
	}
	// canonical order: parents before children and as few back-dated events
	// (full replays) as the frame allows
	evs := make([]*event.Event, 0, len(m.Events))
	for _, w := range m.Events {
		if e, err := event.Unmarshal(w); err == nil {
			evs = append(evs, e)
		}
	}
	event.SortCanonical(evs)
	for _, e := range evs {
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

const (
	// maxEventBytes: the DAG is append-only and replicated, so anything accepted
	// here sits on every member's disk forever. Body.Text/Name/Bio are only capped
	// on the *sending* side, which a hostile client simply ignores.
	maxEventBytes = 256 << 10
	// maxEventsPerFrame mirrors the outbound cap in handleWant.
	maxEventsPerFrame = 2000
)

// ingest verifies and persists one event; returns true if newly stored.
// Missing parents or missing epoch keys park the event in the orphan buffer.
func (c *Core) ingest(chat []byte, e *event.Event, from *p2p.Peer) bool {
	if len(event.Marshal(e)) > maxEventBytes {
		return false
	}
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
		c.park(chat, e, from)
		if from != nil {
			from.SendCtrl(Ctrl{T: "want", Chat: chat, IDs: missing})
		}
		return false
	}
	// validate the DAG with this event included; signature of e was checked
	// above, the rest is already in the database
	keys, mkeys, _ := c.chatKeys(chat)
	f, gen := c.checkout(chat)
	// an event that does not sort after the cached fold replays the whole
	// history; back-dated events from one peer get a budget, or a member could
	// make every event cost a full replay. Dropped ones come back on next sync.
	if from != nil && f != nil && !f.After(e) && !c.refoldOK(from.Pub) {
		c.putBack(chat, gen, f)
		return false
	}
	f, err := c.foldWith(chat, f, gen, e, keys, mkeys)
	if err != nil {
		if bytes.Equal(e.ID(), chat) {
			// genesis we cannot decrypt: our epoch-1 key is missing or bad. Keep
			// it as the judge for offered keys (see keyVerdict) and ask again.
			c.mu.Lock()
			c.genesis[string(chat)] = e
			c.mu.Unlock()
			if from != nil {
				from.SendCtrl(Ctrl{T: "keyreq", Chat: chat})
			}
		}
		return false
	}
	// A membership event we cannot decrypt would be applied blind, so park it and
	// ask for the epoch key instead. Content events are stored either way — they
	// simply stay unreadable until the key shows up.
	// Judge e itself: an epoch flagged by someone else's unreadable event must
	// not hold back a readable owner kick.
	if _, ok := e.Open(keys, mkeys); !ok && event.MembershipKind(e.Kind) {
		c.invalidate(chat)
		c.park(chat, e, from)
		if from != nil {
			from.SendCtrl(Ctrl{T: "keyreq", Chat: chat})
		}
		return false
	}
	if fresh, err := c.St.AddEvent(chat, e); err != nil || !fresh {
		c.invalidate(chat)
		return false
	}
	c.publish(chat, gen, f, nil)
	if e.Kind == event.KindAdd {
		go c.autoRekey(chat)
	}
	if e.Kind == event.KindLeave {
		go c.rekeyAfterLeave(chat)
	}
	if bytes.Equal(e.ID(), chat) {
		c.mu.Lock()
		delete(c.genesis, string(chat))
		c.mu.Unlock()
	}
	// keep chat name cache fresh for the UI list
	if e.Kind == event.KindCreate || e.Kind == event.KindRename {
		if st, err := c.ChatState(chat); err == nil {
			c.St.PutChat(chat, store.ChatMeta{Name: st.Name})
		}
	}
	// attachment, avatar or banner: request missing content
	if blobKind(e.Kind) {
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

// typingTTL: an entry older than this is stale — the UI only shows 4 s of it
// anyway, so anything past this point is pure residue.
const typingTTL = 30 * time.Second

func (c *Core) handleTyping(p *p2p.Peer, m Ctrl) {
	// membership check: without it any friend could mint an entry for any chat id
	// they invent, and neither map was ever cleaned
	if st, err := c.ChatState(m.Chat); err != nil || !st.IsMember(p.Pub) {
		return
	}
	c.mu.Lock()
	if c.typing == nil {
		c.typing = make(map[string]map[string]time.Time)
	}
	cutoff := time.Now().Add(-typingTTL)
	for chat, peers := range c.typing {
		for pub, at := range peers {
			if at.Before(cutoff) {
				delete(peers, pub)
			}
		}
		if len(peers) == 0 {
			delete(c.typing, chat)
		}
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
	// an ack is only meaningful for an event we hold, in a chat the sender is in.
	// Unchecked, this map grew one entry per arbitrary EventID a friend made up.
	if len(m.EventID) != sha256.Size || (m.Status != "delivered" && m.Status != "read") {
		return
	}
	if st, err := c.ChatState(m.Chat); err != nil || !st.IsMember(p.Pub) {
		return
	}
	if !c.St.HasEvent(m.Chat, m.EventID) {
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

// Orphan buffer limits. The global ones hold because a friend can create
// arbitrarily many chats (bootstrap in handleSync), so a per-chat limit is no
// limit. The per-sender ones keep one peer from filling the global cap and
// starving out-of-order sync everywhere else; orphanTTL clears what never
// resolves (a parent nobody has).
const (
	maxOrphans       = 5000
	maxOrphanBytes   = 64 << 20
	maxSenderOrphans = 1000
	maxSenderBytes   = 16 << 20
	orphanTTL        = 10 * time.Minute
)

type orphan struct {
	e    *event.Event
	from string // peer that delivered it
	size int
	at   time.Time
}

// refoldOK spends one full replay from pub's budget.
func (c *Core) refoldOK(pub []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	l := c.refolds[string(pub)]
	if l == nil {
		if len(c.refolds) > 1024 {
			clear(c.refolds)
		}
		l = rate.NewLimiter(20, 200)
		c.refolds[string(pub)] = l
	}
	return l.Allow()
}

func (c *Core) park(chat []byte, e *event.Event, from *p2p.Peer) {
	var sender string
	if from != nil {
		sender = string(from.Pub)
	}
	size := len(event.Marshal(e))
	c.mu.Lock()
	defer c.mu.Unlock()
	key, id := string(chat), string(e.ID())
	if o := c.orphans[key][id]; o != nil {
		return // already parked (drainOrphans retries go through here)
	}
	total, held, mine, mineBytes := 0, 0, 0, 0
	for ck, m := range c.orphans {
		for oid, o := range m {
			if time.Since(o.at) > orphanTTL {
				delete(m, oid)
				continue
			}
			total++
			held += o.size
			if o.from == sender {
				mine++
				mineBytes += o.size
			}
		}
		if len(m) == 0 {
			delete(c.orphans, ck)
		}
	}
	if total >= maxOrphans || held+size > maxOrphanBytes ||
		mine >= maxSenderOrphans || mineBytes+size > maxSenderBytes {
		return
	}
	if c.orphans[key] == nil {
		c.orphans[key] = map[string]*orphan{}
	}
	c.orphans[key][id] = &orphan{e: e, from: sender, size: size, at: time.Now()}
}

func (c *Core) drainOrphans(chat []byte, from *p2p.Peer) {
	for {
		c.mu.Lock()
		var batch []*event.Event
		for _, o := range c.orphans[string(chat)] {
			batch = append(batch, o.e)
		}
		c.mu.Unlock()
		event.SortCanonical(batch)
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

// keyVerdict judges an epoch key against signed history. known=false means
// nothing signed vouches either way.
//
// Epoch keys are not signed, so a member (or, while joining, any friend) can
// offer a fake one. Chats publish sha256 of every epoch key inside the signed
// KindCreate / KindRekey event; that settles it once state folds. While
// joining there is no state yet, but the genesis is authenticated by its ID
// (= chat ID), so the epoch-1 key is right exactly when it decrypts it.
func keyVerdict(st *event.State, gen *event.Event, ep uint32, k []byte) (known, ok bool) {
	if st != nil {
		if want, has := st.KeyHash[ep]; has {
			sum := sha256.Sum256(k)
			return true, bytes.Equal(sum[:], want)
		}
	}
	if gen != nil && gen.Epoch == ep {
		_, err := gen.Decrypt(k)
		return true, err == nil
	}
	return false, false
}

// mkeyVerdict: keyVerdict for a member key held without its content key.
func mkeyVerdict(st *event.State, gen *event.Event, ep uint32, k []byte) (known, ok bool) {
	if st != nil {
		if want, has := st.MKeyHash[ep]; has {
			sum := sha256.Sum256(k)
			return true, bytes.Equal(sum[:], want)
		}
	}
	if gen != nil && gen.Epoch == ep {
		_, err := gen.DecryptMeta(k)
		return true, err == nil
	}
	return false, false
}

// acceptKey: whether to store offered key k for epoch ep, given what signed
// history says about it and about the key already held (old, if any).
func acceptKey(verdict func(*event.State, *event.Event, uint32, []byte) (bool, bool),
	st *event.State, gen *event.Event, ep uint32, k, old []byte) bool {
	if len(k) != 32 {
		return false
	}
	known, ok := verdict(st, gen, ep, k)
	if known && !ok {
		return false
	}
	if old != nil {
		// a stored key is replaced only when signed history proves it wrong
		// and k right; otherwise one fake key would break its epoch forever
		oldKnown, oldOK := verdict(st, gen, ep, old)
		return known && oldKnown && !oldOK
	}
	// old chat without KeyHash: refuse epochs not announced yet
	return known || st == nil || ep <= st.Epoch
}

// handleKeys accepts epoch keys. Only from a chat member (or during joining,
// when state does not exist yet — then only from the inviter or a friend), and
// only when signed history does not refute the key (keyVerdict). A stored key
// is replaced only when signed history proves it wrong and the new one right.
func (c *Core) handleKeys(p *p2p.Peer, m Ctrl) {
	if !c.knownChat(m.Chat) {
		return
	}
	st, stErr := c.ChatState(m.Chat)
	if stErr == nil {
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
	if stErr != nil {
		st = nil
	}
	c.mu.Lock()
	gen := c.genesis[string(m.Chat)]
	c.mu.Unlock()
	added := false
	for ep, k := range m.Epochs {
		if acceptKey(keyVerdict, st, gen, ep, k, existing[ep]) {
			if err := c.St.PutChatKey(m.Chat, ep, k); err == nil {
				added = true
			}
		}
	}
	// member keys only for epochs we hold no content key for
	mexisting, _ := c.St.MemberKeys(m.Chat)
	for ep, k := range m.MEpochs {
		if _, have := existing[ep]; have {
			continue
		}
		if acceptKey(mkeyVerdict, st, gen, ep, k, mexisting[ep]) {
			if err := c.St.PutMemberKey(m.Chat, ep, k); err == nil {
				added = true
			}
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
	c.autoRekey(m.Chat)
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
	return &ParsedInvite{Chat: v.C, Inviter: v.I, Secret: v.S, Connector: wsOrEmpty(v.U), Exp: v.E}, nil
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

// FriendCode: shareable identity card (pubkey + connector hint). No name —
// the code travels through screenshots, QR photos and clipboards, so it carries
// only what is needed to reach the person. The name comes over the encrypted
// link ("hello") once connected, and every later change is a signed event.
func (c *Core) FriendCode() string {
	blob, _ := json.Marshal(map[string]any{
		"p": []byte(c.ID.Pub), "u": c.connURL(),
	})
	return friendPrefix + base64.RawURLEncoding.EncodeToString(blob)
}

// Befriend adds a chat member as a friend, the same one-sided step a friend
// code is. The member reached us through our connector, so it goes on file.
func (c *Core) Befriend(pub []byte, name string) error {
	if len(pub) != ed25519.PublicKeySize || bytes.Equal(pub, c.ID.Pub) {
		return errors.New("nie można dodać tej osoby")
	}
	if err := c.St.PutFriend(store.Friend{Pub: pub, PeerName: safeName(name), Connector: c.connURL()}); err != nil {
		return err
	}
	go c.ensurePeer(pub)
	c.Notify()
	return nil
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
	// v.N: only old codes still carry a name; new ones don't. Until "hello"
	// brings the real one, the fingerprint stands in — an empty row in the
	// friend list would be unclickable. It goes to PeerName, not Name: the user
	// has not named this contact yet, so a later "hello" may still fill it in.
	name := safeName(v.N)
	if name == "" {
		name = identity.Fingerprint(v.P)[:9]
	}
	f = store.Friend{Pub: v.P, PeerName: name, Connector: wsOrEmpty(v.U)}
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
//
// Anything that already carries a scheme is left alone, including a typo like
// "wws://host". Prepending blindly turned that into "ws://wws://host" — an
// address the user could no longer recognise as their own mistake, and one they
// then had to fix on a phone keyboard inside a mangled string.
// wsOrEmpty keeps a connector address from a code only if it is ws:// or
// wss:// with a host; anything else in a code is dropped, not stored.
func wsOrEmpty(s string) string {
	u, err := url.Parse(NormalizeWS(s))
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Host == "" {
		return ""
	}
	return u.String()
}

func NormalizeWS(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.Contains(s, "://") {
		return s
	}
	return "ws://" + s
}
